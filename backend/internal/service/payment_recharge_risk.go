package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	clientip "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/google/uuid"
)

var ErrPaymentRiskIPBlocked = infraerrors.Forbidden("PAYMENT_RISK_IP_BLOCKED", "access from this IP is restricted; contact the administrator")

type PaymentRiskIPRecord struct {
	IP          string `json:"ip"`
	Reason      string `json:"reason"`
	Evidence    string `json:"evidence"`
	Actor       string `json:"actor"`
	CreatedAt   int64  `json:"created_at"`
	Active      bool   `json:"active"`
	LinkedUsers int    `json:"linked_users"`
}

type PaymentRiskService struct {
	client        *dbent.Client
	invalidator   APIKeyAuthCacheInvalidator
	rechargeLimit int
	now           func() time.Time
}

func NewPaymentRiskService(client *dbent.Client, invalidator APIKeyAuthCacheInvalidator) *PaymentRiskService {
	limit := 10
	if configured, err := strconv.Atoi(os.Getenv("PAYMENT_RECHARGE_IP_LIMIT_10M")); err == nil && configured > 0 && configured <= 1000 {
		limit = configured
	}
	return &PaymentRiskService{client: client, invalidator: invalidator, rechargeLimit: limit, now: time.Now}
}

func (s *PaymentRiskService) CheckIP(ctx context.Context, rawIP string) error {
	address := clientip.PublicClientIP(rawIP)
	if address == "" {
		return nil
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("payment risk storage unavailable")
	}
	blocked, err := paymentRiskIPBlocked(ctx, s.client, address)
	if err != nil {
		return err
	}
	if blocked {
		return ErrPaymentRiskIPBlocked
	}
	return nil
}

func paymentRiskIPBlocked(ctx context.Context, client *dbent.Client, address string) (bool, error) {
	rows, err := client.QueryContext(ctx, "SELECT active FROM payment_risk_ips WHERE ip = $1", address)
	if err != nil {
		return false, fmt.Errorf("read payment IP policy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var active bool
	if err := rows.Scan(&active); err != nil {
		return false, err
	}
	return active, rows.Err()
}

func paymentRiskLock(ctx context.Context, client *dbent.Client, address string, now int64) error {
	_, err := client.ExecContext(ctx, `INSERT INTO payment_risk_ip_locks(ip, updated_at) VALUES($1, $2)
		ON CONFLICT(ip) DO UPDATE SET updated_at = excluded.updated_at`, address, now)
	return err
}

func paymentRiskAssociate(ctx context.Context, client *dbent.Client, userID int64, address, source string, now int64) error {
	_, err := client.ExecContext(ctx, `INSERT INTO payment_risk_user_ips(ip,user_id,source,first_seen,last_seen)
		VALUES($1,$2,$3,$4,$4) ON CONFLICT(ip,user_id) DO UPDATE SET last_seen=excluded.last_seen,source=excluded.source`,
		address, userID, source, now)
	return err
}

func paymentRiskEvent(ctx context.Context, client *dbent.Client, address string, userID int64, kind, reason, evidence, actor string, now int64) error {
	var accountID any
	if userID > 0 {
		accountID = userID
	}
	_, err := client.ExecContext(ctx, `INSERT INTO payment_risk_events(id,ip,user_id,kind,reason,evidence,actor,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, uuid.NewString(), address, accountID, kind, reason, evidence, actor, now)
	return err
}

func (s *PaymentRiskService) invalidate(ctx context.Context, userIDs []int64) {
	if s.invalidator == nil {
		return
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for _, userID := range userIDs {
		s.invalidator.InvalidateAuthCacheByUserID(cacheCtx, userID)
	}
}

func paymentRiskDisableUsers(ctx context.Context, client *dbent.Client, address, reason, actor string, now int64, onlyUserID int64) ([]int64, error) {
	var accountIDs []int64
	if onlyUserID > 0 {
		accountIDs = []int64{onlyUserID}
	} else {
		rows, err := client.QueryContext(ctx, `SELECT user_id FROM payment_risk_user_ips WHERE ip=$1
			UNION SELECT user_id FROM payment_orders WHERE client_ip=$1`, address)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var accountID int64
			if err := rows.Scan(&accountID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			accountIDs = append(accountIDs, accountID)
		}
		rowErr := rows.Err()
		_ = rows.Close()
		if rowErr != nil {
			return nil, rowErr
		}
	}
	sort.Slice(accountIDs, func(left, right int) bool { return accountIDs[left] < accountIDs[right] })
	disabled := make([]int64, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		changed, err := client.User.Update().
			Where(user.IDEQ(accountID), user.RoleNEQ(RoleAdmin), user.StatusNEQ(StatusDisabled), user.DeletedAtIsNil()).
			SetStatus(StatusDisabled).Save(ctx)
		if err != nil {
			return nil, err
		}
		if changed == 0 {
			continue
		}
		if err := paymentRiskEvent(ctx, client, address, accountID, "account_disabled", reason, "{}", actor, now); err != nil {
			return nil, err
		}
		disabled = append(disabled, accountID)
	}
	return disabled, nil
}

func (s *PaymentRiskService) RecordUserIP(ctx context.Context, userID int64, rawIP, source string) error {
	address := clientip.PublicClientIP(rawIP)
	if address == "" {
		return nil
	}
	if userID <= 0 || (source != "registration" && source != "login" && source != "payment") {
		return fmt.Errorf("invalid payment IP association")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	now := s.now().Unix()
	if err := paymentRiskLock(ctx, client, address, now); err != nil {
		return err
	}
	if err := paymentRiskAssociate(ctx, client, userID, address, source, now); err != nil {
		return err
	}
	blocked, err := paymentRiskIPBlocked(ctx, client, address)
	if err != nil {
		return err
	}
	var disabled []int64
	if blocked {
		disabled, err = paymentRiskDisableUsers(ctx, client, address, "linked_to_restricted_ip", "ip-policy", now, userID)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidate(ctx, disabled)
	if blocked {
		return ErrPaymentRiskIPBlocked
	}
	return nil
}

func (s *PaymentRiskService) CheckRecharge(ctx context.Context, userID int64, rawIP string) error {
	reservation, err := s.ReserveRecharge(ctx, userID, rawIP)
	if err != nil {
		return err
	}
	return s.FinishRecharge(ctx, reservation, true)
}

func (s *PaymentRiskService) ReserveRecharge(ctx context.Context, userID int64, rawIP string) (string, error) {
	address := clientip.PublicClientIP(rawIP)
	if address == "" {
		return "", nil
	}
	if err := s.RecordUserIP(ctx, userID, address, "payment"); err != nil {
		return "", err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	now := s.now().Unix()
	if err := paymentRiskLock(ctx, client, address, now); err != nil {
		return "", err
	}
	blocked, err := paymentRiskIPBlocked(ctx, client, address)
	if err != nil {
		return "", err
	}
	if blocked {
		return "", ErrPaymentRiskIPBlocked
	}
	rows, err := client.QueryContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN kind='recharge_created' THEN 1 ELSE 0 END),0)
		FROM payment_risk_events WHERE ip=$1 AND kind IN ('recharge_attempt','recharge_created') AND created_at>$2`,
		address, now-600)
	if err != nil {
		return "", err
	}
	var count, completed int
	if !rows.Next() {
		_ = rows.Close()
		return "", fmt.Errorf("payment risk counter unavailable")
	}
	err = rows.Scan(&count, &completed)
	rowErr := rows.Err()
	_ = rows.Close()
	if err != nil {
		return "", err
	}
	if rowErr != nil {
		return "", rowErr
	}
	if count >= s.rechargeLimit {
		if completed < s.rechargeLimit {
			return "", infraerrors.TooManyRequests("PAYMENT_RECHARGE_IN_FLIGHT", "too many checkout requests in progress; retry later")
		}
		disabled, err := paymentRiskDisableUsers(ctx, client, address, "recharge_limit_10m", "ip-policy", now, userID)
		if err != nil {
			return "", err
		}
		if err := paymentRiskEvent(ctx, client, address, userID, "recharge_limit", "recharge_limit_10m",
			fmt.Sprintf(`{"limit":%d,"window_seconds":600}`, s.rechargeLimit), "ip-policy", now); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		s.invalidate(ctx, disabled)
		return "", infraerrors.Forbidden("PAYMENT_RECHARGE_RATE_BLOCKED", "too many recharge attempts in ten minutes; account requires administrator review")
	}
	reservation := uuid.NewString()
	_, err = client.ExecContext(ctx, `INSERT INTO payment_risk_events(id,ip,user_id,kind,reason,evidence,actor,created_at)
		VALUES($1,$2,$3,'recharge_attempt','','{}','authenticated-user',$4)`, reservation, address, userID, now)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return reservation, nil
}

func (s *PaymentRiskService) FinishRecharge(ctx context.Context, reservation string, succeeded bool) error {
	if reservation == "" {
		return nil
	}
	kind := "recharge_failed"
	if succeeded {
		kind = "recharge_created"
	}
	_, err := s.client.ExecContext(ctx, `UPDATE payment_risk_events SET kind=$2 WHERE id=$1 AND kind='recharge_attempt'`, reservation, kind)
	return err
}

func (s *PaymentRiskService) BlockOrderIP(ctx context.Context, orderID int64, reason, actor string) error {
	reason, actor = strings.TrimSpace(reason), strings.TrimSpace(actor)
	if reason == "" || len(reason) > 512 || actor == "" || len(actor) > 128 {
		return infraerrors.BadRequest("INVALID_RISK_REASON", "a bounded reason and actor are required")
	}
	order, err := s.client.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return err
	}
	address := clientip.PublicClientIP(order.ClientIP)
	if address == "" {
		return infraerrors.BadRequest("NON_PUBLIC_PAYMENT_IP", "order has no attributable public client IP")
	}
	if actor == "gateway-confirmation" {
		rows, err := s.client.QueryContext(ctx, `SELECT first_seen FROM payment_risk_user_ips WHERE ip=$1 AND user_id=$2`,
			address, order.UserID)
		if err != nil {
			return err
		}
		var firstSeen int64
		matched := rows.Next()
		if matched {
			err = rows.Scan(&firstSeen)
		}
		rowErr := rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		if rowErr != nil {
			return rowErr
		}
		if !matched || firstSeen > order.CreatedAt.Unix() {
			return infraerrors.Conflict("UNVERIFIED_HISTORICAL_PAYMENT_IP", "historical IP attribution requires administrator review")
		}
	}
	evidence, err := json.Marshal(map[string]any{
		"order_id": order.ID, "out_trade_no": order.OutTradeNo, "user_id": order.UserID,
		"pay_amount": order.PayAmount, "source": "persisted_payment_order",
	})
	if err != nil {
		return err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	now := s.now().Unix()
	if err := paymentRiskLock(ctx, client, address, now); err != nil {
		return err
	}
	if err := paymentRiskAssociate(ctx, client, order.UserID, address, "payment", now); err != nil {
		return err
	}
	_, err = client.ExecContext(ctx, `INSERT INTO payment_risk_ips(ip,reason,evidence,actor,created_at,active)
		VALUES($1,$2,$3,$4,$5,TRUE) ON CONFLICT(ip) DO UPDATE SET
		reason=excluded.reason,evidence=excluded.evidence,actor=excluded.actor,active=TRUE,released_at=NULL`,
		address, reason, string(evidence), actor, now)
	if err != nil {
		return err
	}
	if err := paymentRiskEvent(ctx, client, address, order.UserID, "ip_blocked", reason, string(evidence), actor, now); err != nil {
		return err
	}
	disabled, err := paymentRiskDisableUsers(ctx, client, address, reason, actor, now, 0)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidate(ctx, disabled)
	return nil
}

func (s *PaymentRiskService) UnblockIP(ctx context.Context, rawIP, actor, reason string) error {
	address := clientip.PublicClientIP(rawIP)
	reason, actor = strings.TrimSpace(reason), strings.TrimSpace(actor)
	if address == "" || reason == "" || len(reason) > 512 || actor == "" || len(actor) > 128 {
		return infraerrors.BadRequest("INVALID_RISK_RELEASE", "public IP, actor and release reason are required")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	now := s.now().Unix()
	if err := paymentRiskLock(ctx, client, address, now); err != nil {
		return err
	}
	result, err := client.ExecContext(ctx, "UPDATE payment_risk_ips SET active=FALSE,released_at=$2 WHERE ip=$1", address, now)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		return infraerrors.NotFound("PAYMENT_RISK_IP_NOT_FOUND", "IP policy was not found")
	}
	if err := paymentRiskEvent(ctx, client, address, 0, "ip_released", reason, "{}", actor, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PaymentRiskService) ListBlockedIPs(ctx context.Context, page, size int) ([]PaymentRiskIPRecord, int, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	totalRows, err := s.client.QueryContext(ctx, "SELECT COUNT(*) FROM payment_risk_ips")
	if err != nil {
		return nil, 0, err
	}
	var total int
	if !totalRows.Next() {
		_ = totalRows.Close()
		return nil, 0, fmt.Errorf("payment risk count unavailable")
	}
	err = totalRows.Scan(&total)
	_ = totalRows.Close()
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.client.QueryContext(ctx, `SELECT ip,reason,evidence,actor,created_at,active FROM payment_risk_ips
		ORDER BY active DESC,created_at DESC,ip ASC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	records := make([]PaymentRiskIPRecord, 0)
	for rows.Next() {
		var record PaymentRiskIPRecord
		if err := rows.Scan(&record.IP, &record.Reason, &record.Evidence, &record.Actor, &record.CreatedAt, &record.Active); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		records = append(records, record)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for index := range records {
		linked, err := s.client.QueryContext(ctx, `SELECT COUNT(*) FROM (
			SELECT user_id FROM payment_risk_user_ips WHERE ip=$1
			UNION SELECT user_id FROM payment_orders WHERE client_ip=$1
		) AS linked_users`, records[index].IP)
		if err != nil {
			return nil, 0, err
		}
		if !linked.Next() {
			_ = linked.Close()
			return nil, 0, sql.ErrNoRows
		}
		err = linked.Scan(&records[index].LinkedUsers)
		_ = linked.Close()
		if err != nil {
			return nil, 0, err
		}
	}
	return records, total, nil
}

func (s *PaymentRiskService) ScanBlockedAccounts(ctx context.Context) (int, error) {
	rows, err := s.client.QueryContext(ctx, "SELECT ip FROM payment_risk_ips WHERE active=TRUE ORDER BY ip")
	if err != nil {
		return 0, err
	}
	var addresses []string
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if clientip.PublicClientIP(address) != "" {
			addresses = append(addresses, address)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, address := range addresses {
		disabled, err := s.scanAddress(ctx, address)
		if err != nil {
			return total, err
		}
		total += len(disabled)
		s.invalidate(ctx, disabled)
	}
	return total, nil
}

func (s *PaymentRiskService) scanAddress(ctx context.Context, address string) ([]int64, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	now := s.now().Unix()
	if err := paymentRiskLock(ctx, client, address, now); err != nil {
		return nil, err
	}
	blocked, err := paymentRiskIPBlocked(ctx, client, address)
	if err != nil {
		return nil, err
	}
	if !blocked {
		return nil, tx.Commit()
	}
	disabled, err := paymentRiskDisableUsers(ctx, client, address, "periodic_ip_policy_enforcement", "ip-policy-scan", now, 0)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return disabled, nil
}
