package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type autoRoutingRepository struct {
	db *sql.DB
}

func NewAutoRoutingRepository(_ *dbent.Client, sqlDB *sql.DB) service.AutoRoutingRepository {
	return &autoRoutingRepository{db: sqlDB}
}

func (r *autoRoutingRepository) CreateAutoRoutingProfile(ctx context.Context, profile *service.AutoRoutingProfile) error {
	if profile == nil {
		return errors.New("auto routing profile is nil")
	}
	modelCatalogFilter, err := marshalAutoRoutingProfileModelCatalogFilter(profile.ModelCatalogFilter)
	if err != nil {
		return fmt.Errorf("marshal auto routing profile model catalog filter: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
		INSERT INTO auto_group_profiles (
			name, status, max_candidates, allow_custom_order, cross_group_retry_allowed,
			model_catalog_filter
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, revision, created_at, updated_at
	`, profile.Name, profile.Status, profile.MaxCandidates, profile.AllowCustomOrder,
		profile.CrossGroupRetryAllowed, modelCatalogFilter).
		Scan(&profile.ID, &profile.Revision, &profile.CreatedAt, &profile.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create auto routing profile: %w", err)
	}
	if err := replaceProfileMembers(ctx, tx, profile.ID, profile.Members); err != nil {
		return fmt.Errorf("create auto routing profile members: %w", err)
	}
	return tx.Commit()
}

func (r *autoRoutingRepository) GetAutoRoutingProfile(ctx context.Context, id int64) (*service.AutoRoutingProfile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	profile := &service.AutoRoutingProfile{}
	var modelCatalogFilter []byte
	err = tx.QueryRowContext(ctx, `
		SELECT id, name, status, revision, max_candidates, allow_custom_order,
		       cross_group_retry_allowed, model_catalog_filter, created_at, updated_at
		FROM auto_group_profiles
		WHERE id = $1
		FOR SHARE
	`, id).Scan(
		&profile.ID,
		&profile.Name,
		&profile.Status,
		&profile.Revision,
		&profile.MaxCandidates,
		&profile.AllowCustomOrder,
		&profile.CrossGroupRetryAllowed,
		&modelCatalogFilter,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit missing Auto routing profile lookup: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get auto routing profile: %w", err)
	}
	if err := unmarshalAutoRoutingProfileModelCatalogFilter(modelCatalogFilter, profile); err != nil {
		return nil, fmt.Errorf("decode auto routing profile model catalog filter: %w", err)
	}
	profile.Members, err = listProfileMembers(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Auto routing profile lookup: %w", err)
	}
	return profile, nil
}

func (r *autoRoutingRepository) ListAutoRoutingProfiles(ctx context.Context) ([]service.AutoRoutingProfile, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, status, revision, max_candidates, allow_custom_order,
		       cross_group_retry_allowed, model_catalog_filter, created_at, updated_at
		FROM auto_group_profiles
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list auto routing profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	profiles := make([]service.AutoRoutingProfile, 0)
	profileIDs := make([]int64, 0)
	for rows.Next() {
		var profile service.AutoRoutingProfile
		var modelCatalogFilter []byte
		if err := rows.Scan(
			&profile.ID,
			&profile.Name,
			&profile.Status,
			&profile.Revision,
			&profile.MaxCandidates,
			&profile.AllowCustomOrder,
			&profile.CrossGroupRetryAllowed,
			&modelCatalogFilter,
			&profile.CreatedAt,
			&profile.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan auto routing profile: %w", err)
		}
		if err := unmarshalAutoRoutingProfileModelCatalogFilter(modelCatalogFilter, &profile); err != nil {
			return nil, fmt.Errorf("decode auto routing profile model catalog filter: %w", err)
		}
		profiles = append(profiles, profile)
		profileIDs = append(profileIDs, profile.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auto routing profiles: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close auto routing profiles: %w", err)
	}
	if len(profileIDs) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty Auto routing profile list: %w", err)
		}
		return profiles, nil
	}

	memberRows, err := tx.QueryContext(ctx, `
		SELECT profile_id, group_id, sort_order
		FROM auto_group_profile_members
		WHERE profile_id = ANY($1)
		ORDER BY profile_id, sort_order
	`, pq.Array(profileIDs))
	if err != nil {
		return nil, fmt.Errorf("list auto routing profile members: %w", err)
	}
	defer func() { _ = memberRows.Close() }()
	profileIndex := make(map[int64]int, len(profiles))
	for i := range profiles {
		profileIndex[profiles[i].ID] = i
		profiles[i].Members = make([]service.AutoRoutingProfileMember, 0)
	}
	for memberRows.Next() {
		var profileID int64
		var member service.AutoRoutingProfileMember
		if err := memberRows.Scan(&profileID, &member.GroupID, &member.SortOrder); err != nil {
			return nil, fmt.Errorf("scan auto routing profile member: %w", err)
		}
		if index, ok := profileIndex[profileID]; ok {
			profiles[index].Members = append(profiles[index].Members, member)
		}
	}
	if err := memberRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auto routing profile members: %w", err)
	}
	if err := memberRows.Close(); err != nil {
		return nil, fmt.Errorf("close auto routing profile members: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Auto routing profile list: %w", err)
	}
	return profiles, nil
}

func (r *autoRoutingRepository) UpdateAutoRoutingProfile(ctx context.Context, profile *service.AutoRoutingProfile, expectedRevision int64) error {
	if profile == nil {
		return errors.New("auto routing profile is nil")
	}
	var modelCatalogFilter any
	if profile.ModelCatalogFilter != nil {
		filterJSON, err := marshalAutoRoutingProfileModelCatalogFilter(profile.ModelCatalogFilter)
		if err != nil {
			return fmt.Errorf("marshal auto routing profile model catalog filter: %w", err)
		}
		modelCatalogFilter = filterJSON
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
		UPDATE auto_group_profiles
		SET name = $2,
		    status = $3,
		    max_candidates = $4,
		    allow_custom_order = $5,
		    cross_group_retry_allowed = $6,
		    model_catalog_filter = COALESCE($7::jsonb, model_catalog_filter),
		    revision = revision + 1,
		    updated_at = NOW()
		WHERE id = $1 AND revision = $8
		RETURNING revision, updated_at
	`, profile.ID, profile.Name, profile.Status, profile.MaxCandidates, profile.AllowCustomOrder,
		profile.CrossGroupRetryAllowed, modelCatalogFilter, expectedRevision).
		Scan(&profile.Revision, &profile.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAutoRoutingRevisionConflict
	}
	if err != nil {
		return fmt.Errorf("update auto routing profile: %w", err)
	}
	if err := replaceProfileMembers(ctx, tx, profile.ID, profile.Members); err != nil {
		return fmt.Errorf("replace auto routing profile members: %w", err)
	}
	return tx.Commit()
}

func marshalAutoRoutingProfileModelCatalogFilter(filter *service.GroupModelAllowlist) ([]byte, error) {
	if filter == nil {
		filter = &service.GroupModelAllowlist{Models: []string{}}
	}
	models := append([]string{}, filter.Models...)
	return json.Marshal(struct {
		Enabled bool     `json:"enabled"`
		Models  []string `json:"models"`
	}{
		Enabled: filter.Enabled,
		Models:  models,
	})
}

func unmarshalAutoRoutingProfileModelCatalogFilter(raw []byte, profile *service.AutoRoutingProfile) error {
	filter := &service.GroupModelAllowlist{Models: []string{}}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, filter); err != nil {
			return err
		}
	}
	if filter.Models == nil {
		filter.Models = []string{}
	}
	profile.ModelCatalogFilter = filter
	return nil
}

func replaceProfileMembers(ctx context.Context, tx *sql.Tx, profileID int64, members []service.AutoRoutingProfileMember) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM auto_group_profile_members WHERE profile_id = $1`, profileID); err != nil {
		return err
	}
	for _, member := range members {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auto_group_profile_members (profile_id, group_id, sort_order)
			VALUES ($1, $2, $3)
		`, profileID, member.GroupID, member.SortOrder); err != nil {
			return err
		}
	}
	return nil
}

func listProfileMembers(ctx context.Context, q sqlQueryer, profileID int64) ([]service.AutoRoutingProfileMember, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT group_id, sort_order
		FROM auto_group_profile_members
		WHERE profile_id = $1
		ORDER BY sort_order
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list auto routing profile members: %w", err)
	}
	defer func() { _ = rows.Close() }()
	members := make([]service.AutoRoutingProfileMember, 0)
	for rows.Next() {
		var member service.AutoRoutingProfileMember
		if err := rows.Scan(&member.GroupID, &member.SortOrder); err != nil {
			return nil, fmt.Errorf("scan auto routing profile member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auto routing profile members: %w", err)
	}
	return members, nil
}

func (r *autoRoutingRepository) GetAPIKeyAutoRouting(ctx context.Context, apiKeyID int64) (*service.APIKeyAutoRoutingConfig, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var fixedGroupID sql.NullInt64
	var deletedAt sql.NullTime
	var routingMode string
	var routingRevision int64
	err = tx.QueryRowContext(ctx, `
		SELECT group_id, routing_mode, routing_revision, deleted_at
		FROM api_keys
		WHERE id = $1
		FOR SHARE
	`, apiKeyID).Scan(&fixedGroupID, &routingMode, &routingRevision, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && deletedAt.Valid) {
		return nil, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read API key before Auto routing lookup: %w", err)
	}

	var config service.APIKeyAutoRoutingConfig
	err = tx.QueryRowContext(ctx, `
		SELECT api_key_id, routing_mode, profile_id, selection_mode,
		       cross_group_retry, revision, created_at, updated_at
		FROM auto_key_routing
		WHERE api_key_id = $1
	`, apiKeyID).Scan(
		&config.APIKeyID,
		&config.Mode,
		&config.ProfileID,
		&config.SelectionMode,
		&config.CrossGroupRetry,
		&config.Revision,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if routingMode != "fixed" {
			return nil, errors.New("invalid API key Auto routing marker")
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit fixed API key routing lookup: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get API key auto routing: %w", err)
	}
	if config.Mode != service.AutoRoutingModeAuto || routingMode != service.AutoRoutingModeAuto ||
		fixedGroupID.Valid || config.Revision != routingRevision {
		return nil, errors.New("invalid Auto API key routing invariant")
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT group_id
		FROM auto_key_routing_groups
		WHERE api_key_id = $1
		ORDER BY sort_order
	`, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("list API key Auto routing groups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	config.GroupIDs = make([]int64, 0)
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("scan API key Auto routing group: %w", err)
		}
		config.GroupIDs = append(config.GroupIDs, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API key Auto routing groups: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close API key Auto routing groups: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit API key Auto routing lookup: %w", err)
	}
	return &config, nil
}

func (r *autoRoutingRepository) ReplaceAPIKeyAutoRouting(
	ctx context.Context,
	apiKeyID int64,
	config *service.APIKeyAutoRoutingConfig,
	fixedGroupID *int64,
	expectedRevision int64,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var routingWriteFlag string
	if err := tx.QueryRowContext(ctx, `
		SELECT set_config('app.auto_routing_write', 'on', true)
	`).Scan(&routingWriteFlag); err != nil {
		return fmt.Errorf("set Auto routing transaction marker: %w", err)
	}

	var currentGroupID sql.NullInt64
	var currentRoutingMode string
	var currentRoutingRevision int64
	err = tx.QueryRowContext(ctx, `
		SELECT group_id, routing_mode, routing_revision
		FROM api_keys
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, apiKeyID).Scan(&currentGroupID, &currentRoutingMode, &currentRoutingRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAPIKeyNotFound
	}
	if err != nil {
		return fmt.Errorf("lock API key for routing update: %w", err)
	}

	var autoConfigRevision int64
	err = tx.QueryRowContext(ctx, `
		SELECT revision FROM auto_key_routing WHERE api_key_id = $1 FOR UPDATE
	`, apiKeyID).Scan(&autoConfigRevision)
	rowExists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read API key routing revision: %w", err)
	}
	if currentRoutingMode != "fixed" && currentRoutingMode != service.AutoRoutingModeAuto {
		return errors.New("invalid API key routing mode")
	}
	if (rowExists && (currentRoutingMode != service.AutoRoutingModeAuto || currentGroupID.Valid ||
		autoConfigRevision != currentRoutingRevision)) ||
		(!rowExists && currentRoutingMode != "fixed") {
		return errors.New("invalid API key Auto routing invariant")
	}
	if currentRoutingRevision != expectedRevision {
		return service.ErrAutoRoutingRevisionConflict
	}

	if config == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM auto_key_routing WHERE api_key_id = $1`, apiKeyID); err != nil {
			return fmt.Errorf("remove API key Auto routing: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE api_keys
			SET routing_mode = 'fixed', group_id = $2, routing_revision = $3, updated_at = NOW()
			WHERE id = $1 AND deleted_at IS NULL
		`, apiKeyID, fixedGroupID, currentRoutingRevision+1); err != nil {
			return fmt.Errorf("restore fixed API key group: %w", err)
		}
		return tx.Commit()
	}
	if config.APIKeyID != 0 && config.APIKeyID != apiKeyID {
		return errors.New("auto API key routing identity mismatch")
	}
	if config.Mode != "" && config.Mode != service.AutoRoutingModeAuto {
		return errors.New("unsupported API key routing mode")
	}
	if config.ProfileID <= 0 {
		return errors.New("auto API key routing requires a profile")
	}
	if config.SelectionMode != service.AutoRoutingSelectionInherit && config.SelectionMode != service.AutoRoutingSelectionCustom {
		return errors.New("unsupported Auto API key selection mode")
	}
	if fixedGroupID != nil {
		return errors.New("auto API key routing cannot have a fixed group")
	}
	if config.SelectionMode == service.AutoRoutingSelectionInherit && len(config.GroupIDs) != 0 {
		return errors.New("inherited Auto API key routing cannot persist custom groups")
	}
	var profileActive bool
	if err := tx.QueryRowContext(ctx, `
		SELECT status = 'active' FROM auto_group_profiles WHERE id = $1 FOR SHARE
	`, config.ProfileID).Scan(&profileActive); err != nil {
		return fmt.Errorf("validate Auto routing profile: %w", err)
	}
	if !profileActive {
		return errors.New("auto routing profile is disabled")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET group_id = NULL, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, apiKeyID); err != nil {
		return fmt.Errorf("clear fixed API key group: %w", err)
	}
	nextRevision := expectedRevision + 1
	if rowExists {
		result, err := tx.ExecContext(ctx, `
			UPDATE auto_key_routing
			SET routing_mode = 'auto',
			    profile_id = $2,
			    selection_mode = $3,
			    cross_group_retry = $4,
			    revision = $5,
			    updated_at = NOW()
			WHERE api_key_id = $1 AND revision = $6
		`, apiKeyID, config.ProfileID, config.SelectionMode, config.CrossGroupRetry, nextRevision, expectedRevision)
		if err != nil {
			return fmt.Errorf("update API key Auto routing: %w", err)
		}
		if err := requireOneRow(result); err != nil {
			return service.ErrAutoRoutingRevisionConflict
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auto_key_routing (
				api_key_id, routing_mode, profile_id, selection_mode, cross_group_retry, revision
			) VALUES ($1, 'auto', $2, $3, $4, $5)
		`, apiKeyID, config.ProfileID, config.SelectionMode, config.CrossGroupRetry, nextRevision); err != nil {
			return fmt.Errorf("create API key Auto routing: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auto_key_routing_groups WHERE api_key_id = $1`, apiKeyID); err != nil {
		return fmt.Errorf("replace API key Auto routing groups: %w", err)
	}
	for sortOrder, groupID := range config.GroupIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auto_key_routing_groups (api_key_id, group_id, sort_order)
			VALUES ($1, $2, $3)
		`, apiKeyID, groupID, sortOrder); err != nil {
			return fmt.Errorf("insert API key Auto routing group: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_keys
		SET routing_mode = 'auto', group_id = NULL, routing_revision = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, apiKeyID, currentRoutingRevision+1); err != nil {
		return fmt.Errorf("set API key Auto routing marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit API key Auto routing update: %w", err)
	}
	config.APIKeyID = apiKeyID
	config.Mode = service.AutoRoutingModeAuto
	config.Revision = nextRevision
	return nil
}

func requireOneRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("expected one updated row")
	}
	return nil
}

func (r *autoRoutingRepository) CreateOrGetAutoRoutingExecution(
	ctx context.Context,
	input service.AutoRoutingExecutionInput,
) (*service.AutoRoutingExecution, bool, error) {
	if input.APIKeyID <= 0 || input.RequestID == "" || input.ProfileID <= 0 || input.ProfileRevision <= 0 || input.RoutingRevision <= 0 {
		return nil, false, errors.New("invalid Auto routing execution identity")
	}
	decodedPayloadHash, err := hex.DecodeString(input.PayloadHash)
	if err != nil || len(decodedPayloadHash) != 32 {
		return nil, false, errors.New("auto routing execution payload hash must be SHA-256 hex")
	}
	input.PayloadHash = hex.EncodeToString(decodedPayloadHash)
	if len(input.CandidateGroupIDs) == 0 || input.SelectedGroupID <= 0 {
		return nil, false, errors.New("auto routing execution candidate selection is empty")
	}
	seen := make(map[int64]struct{}, len(input.CandidateGroupIDs))
	for _, groupID := range input.CandidateGroupIDs {
		if groupID <= 0 {
			return nil, false, errors.New("auto routing execution has invalid candidate group")
		}
		if _, exists := seen[groupID]; exists {
			return nil, false, errors.New("auto routing execution has duplicate candidate group")
		}
		seen[groupID] = struct{}{}
	}
	if !containsInt64(input.CandidateGroupIDs, input.SelectedGroupID) {
		return nil, false, errors.New("selected group is not in Auto routing candidate snapshot")
	}
	candidates, err := json.Marshal(input.CandidateGroupIDs)
	if err != nil {
		return nil, false, fmt.Errorf("marshal Auto routing candidate snapshot: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	execution, created, err := insertOrGetAutoRouteExecution(ctx, tx, input, candidates)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit Auto routing execution: %w", err)
	}
	return execution, created, nil
}

func insertOrGetAutoRouteExecution(
	ctx context.Context,
	tx *sql.Tx,
	input service.AutoRoutingExecutionInput,
	candidates []byte,
) (*service.AutoRoutingExecution, bool, error) {
	execution, err := scanAutoRoutingExecution(tx.QueryRowContext(ctx, `
		SELECT id, api_key_id, request_id, payload_hash, profile_id, profile_revision, routing_revision,
		       candidate_group_ids, status, current_attempt, actual_group_id,
		       result_reference, evidence_code, settlement_status, created_at, updated_at, completed_at
		FROM auto_route_executions
		WHERE api_key_id = $1 AND request_id = $2
		FOR UPDATE
	`, input.APIKeyID, input.RequestID))
	if err == nil {
		return execution, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("check existing Auto routing execution: %w", err)
	}
	if err := validateAutoRoutingExecutionSnapshot(ctx, tx, input); err != nil {
		return nil, false, err
	}

	execution, err = scanAutoRoutingExecution(tx.QueryRowContext(ctx, `
		INSERT INTO auto_route_executions (
			api_key_id, request_id, payload_hash, profile_id, profile_revision,
			routing_revision, candidate_group_ids, actual_group_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
		ON CONFLICT (api_key_id, request_id) DO NOTHING
		RETURNING id, api_key_id, request_id, payload_hash, profile_id, profile_revision, routing_revision,
		          candidate_group_ids, status, current_attempt, actual_group_id,
		          result_reference, evidence_code, settlement_status, created_at, updated_at, completed_at
	`, input.APIKeyID, input.RequestID, input.PayloadHash, input.ProfileID, input.ProfileRevision, input.RoutingRevision, string(candidates), input.SelectedGroupID))
	if err == nil {
		return execution, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("create Auto routing execution: %w", err)
	}
	execution, err = scanAutoRoutingExecution(tx.QueryRowContext(ctx, `
		SELECT id, api_key_id, request_id, payload_hash, profile_id, profile_revision, routing_revision,
		       candidate_group_ids, status, current_attempt, actual_group_id,
		       result_reference, evidence_code, settlement_status, created_at, updated_at, completed_at
		FROM auto_route_executions
		WHERE api_key_id = $1 AND request_id = $2
	`, input.APIKeyID, input.RequestID))
	if err != nil {
		return nil, false, fmt.Errorf("load existing Auto routing execution: %w", err)
	}
	return execution, false, nil
}

func validateAutoRoutingExecutionSnapshot(ctx context.Context, tx *sql.Tx, input service.AutoRoutingExecutionInput) error {
	var groupID sql.NullInt64
	var routingMode string
	var routingRevision int64
	var deletedAtTime sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT group_id, routing_mode, routing_revision, deleted_at
		FROM api_keys
		WHERE id = $1
		FOR SHARE
	`, input.APIKeyID).Scan(&groupID, &routingMode, &routingRevision, &deletedAtTime); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrAPIKeyNotFound
		}
		return fmt.Errorf("validate API key for Auto routing execution: %w", err)
	}
	if deletedAtTime.Valid {
		return service.ErrAPIKeyNotFound
	}
	if groupID.Valid || routingMode != service.AutoRoutingModeAuto {
		return service.ErrAutoRoutingRevisionConflict
	}

	var profileID, configRevision, profileRevision int64
	var profileStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT routing.profile_id, routing.revision, profile.revision, profile.status
		FROM auto_key_routing AS routing
		JOIN auto_group_profiles AS profile ON profile.id = routing.profile_id
		WHERE routing.api_key_id = $1 AND routing.routing_mode = 'auto'
		FOR SHARE OF routing, profile
	`, input.APIKeyID).Scan(&profileID, &configRevision, &profileRevision, &profileStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrAutoRoutingRevisionConflict
		}
		return fmt.Errorf("validate Auto routing execution snapshot: %w", err)
	}
	if profileStatus != service.AutoRoutingProfileStatusActive ||
		profileID != input.ProfileID ||
		configRevision != input.RoutingRevision ||
		routingRevision != configRevision ||
		profileRevision != input.ProfileRevision {
		return service.ErrAutoRoutingRevisionConflict
	}
	return nil
}

type autoRoutingRowScanner interface {
	Scan(dest ...any) error
}

func scanAutoRoutingExecution(row autoRoutingRowScanner) (*service.AutoRoutingExecution, error) {
	var execution service.AutoRoutingExecution
	var candidates []byte
	err := row.Scan(
		&execution.ID,
		&execution.APIKeyID,
		&execution.RequestID,
		&execution.PayloadHash,
		&execution.ProfileID,
		&execution.ProfileRevision,
		&execution.RoutingRevision,
		&candidates,
		&execution.Status,
		&execution.CurrentAttempt,
		&execution.ActualGroupID,
		&execution.ResultReference,
		&execution.EvidenceCode,
		&execution.SettlementStatus,
		&execution.CreatedAt,
		&execution.UpdatedAt,
		&execution.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(candidates, &execution.CandidateGroupIDs); err != nil {
		return nil, fmt.Errorf("decode Auto routing candidate snapshot: %w", err)
	}
	return &execution, nil
}

func (r *autoRoutingRepository) GetAutoRoutingExecution(ctx context.Context, apiKeyID int64, requestID string) (*service.AutoRoutingExecution, error) {
	execution, err := scanAutoRoutingExecution(r.db.QueryRowContext(ctx, `
		SELECT id, api_key_id, request_id, payload_hash, profile_id, profile_revision, routing_revision,
		       candidate_group_ids, status, current_attempt, actual_group_id,
		       result_reference, evidence_code, settlement_status, created_at, updated_at, completed_at
		FROM auto_route_executions
		WHERE api_key_id = $1 AND request_id = $2
	`, apiKeyID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get Auto routing execution: %w", err)
	}
	return execution, nil
}

func (r *autoRoutingRepository) ClaimAutoRoutingAttemptDispatch(ctx context.Context, attempt *service.AutoRoutingAttempt) (bool, error) {
	if attempt == nil || attempt.ExecutionID <= 0 || attempt.AttemptNo <= 0 || attempt.GroupID <= 0 {
		return false, errors.New("invalid Auto routing dispatch attempt")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	var currentAttempt int
	var selectedGroupID sql.NullInt64
	var settlementStatus string
	var candidates []byte
	err = tx.QueryRowContext(ctx, `
		SELECT status, current_attempt, actual_group_id, candidate_group_ids, settlement_status
		FROM auto_route_executions
		WHERE id = $1
		FOR UPDATE
	`, attempt.ExecutionID).Scan(&status, &currentAttempt, &selectedGroupID, &candidates, &settlementStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock Auto routing execution for dispatch: %w", err)
	}
	var candidateIDs []int64
	if err := json.Unmarshal(candidates, &candidateIDs); err != nil {
		return false, fmt.Errorf("decode Auto routing candidate snapshot: %w", err)
	}
	if !containsInt64(candidateIDs, attempt.GroupID) {
		return false, errors.New("selected group is not in Auto routing candidate snapshot")
	}
	if !selectedGroupID.Valid || selectedGroupID.Int64 != attempt.GroupID {
		return false, errors.New("dispatch group does not match persisted Auto routing selection")
	}
	if settlementStatus != "pending" ||
		status != service.AutoRouteExecutionPrepared || currentAttempt+1 != attempt.AttemptNo {
		return false, nil
	}

	var existingGroupID int64
	err = tx.QueryRowContext(ctx, `
		SELECT group_id
		FROM auto_route_attempts
		WHERE execution_id = $1 AND attempt_no = $2
		FOR UPDATE
	`, attempt.ExecutionID, attempt.AttemptNo).Scan(&existingGroupID)
	if err == nil {
		if existingGroupID != attempt.GroupID {
			return false, errors.New("auto routing attempt group conflicts with existing attempt")
		}
		return false, errors.New("prepared Auto routing execution already has an attempt")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("check existing Auto routing attempt: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO auto_route_attempts (
			execution_id, attempt_no, group_id, status, account_id, platform,
			billing_snapshot_ref, upstream_request_id, upstream_task_id, dispatched_at
		) VALUES ($1, $2, $3, 'dispatching', $4, $5, $6, $7, $8, NOW())
		RETURNING id, status, dispatched_at, created_at, updated_at
	`, attempt.ExecutionID, attempt.AttemptNo, attempt.GroupID, attempt.AccountID, attempt.Platform, attempt.BillingSnapshotRef, attempt.UpstreamRequestID, attempt.UpstreamTaskID).
		Scan(&attempt.ID, &attempt.Status, &attempt.DispatchedAt, &attempt.CreatedAt, &attempt.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("create Auto routing attempt: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE auto_route_executions
		SET status = 'dispatching',
		    current_attempt = $2,
		    actual_group_id = $3,
		    updated_at = NOW()
		WHERE id = $1 AND status = 'prepared' AND current_attempt = $4
		  AND settlement_status = 'pending'
	`, attempt.ExecutionID, attempt.AttemptNo, attempt.GroupID, attempt.AttemptNo-1)
	if err != nil {
		return false, fmt.Errorf("claim Auto routing execution dispatch: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit Auto routing dispatch claim: %w", err)
	}
	return true, nil
}

func (r *autoRoutingRepository) CompleteAutoRoutingAttempt(ctx context.Context, executionID int64, attemptNo int, resultReference string) (bool, error) {
	return r.finishAttempt(ctx, executionID, attemptNo, service.AutoRouteAttemptCompleted, service.AutoRouteExecutionCompleted, resultReference, "")
}

func (r *autoRoutingRepository) MarkAutoRoutingAttemptUnknown(ctx context.Context, executionID int64, attemptNo int, evidenceCode string) (bool, error) {
	if evidenceCode == "" || len(evidenceCode) > 100 {
		return false, errors.New("invalid Auto routing unknown evidence code")
	}
	for _, char := range evidenceCode {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' {
			return false, errors.New("auto routing evidence code must be ASCII identifier text")
		}
	}
	return r.finishAttempt(ctx, executionID, attemptNo, service.AutoRouteAttemptUnknown, service.AutoRouteExecutionUnknown, "", evidenceCode)
}

func (r *autoRoutingRepository) finishAttempt(
	ctx context.Context,
	executionID int64,
	attemptNo int,
	attemptStatus string,
	executionStatus string,
	resultReference string,
	evidenceCode string,
) (bool, error) {
	if executionID <= 0 || attemptNo <= 0 {
		return false, errors.New("invalid Auto routing attempt identity")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		UPDATE auto_route_executions
		SET status = $2, result_reference = NULLIF($3, ''),
		    evidence_code = NULLIF($4, ''), completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'dispatching' AND current_attempt = $5
		  AND settlement_status = 'pending'
	`, executionID, executionStatus, resultReference, evidenceCode, attemptNo)
	if err != nil {
		return false, fmt.Errorf("transition Auto routing execution: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, nil
	}

	result, err = tx.ExecContext(ctx, `
		UPDATE auto_route_attempts
		SET status = $3, result_reference = NULLIF($4, ''),
		    completed_at = NOW(), updated_at = NOW()
		WHERE execution_id = $1 AND attempt_no = $2 AND status = 'dispatching'
	`, executionID, attemptNo, attemptStatus, resultReference)
	if err != nil {
		return false, fmt.Errorf("transition Auto routing attempt: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return false, err
	} else if affected != 1 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit Auto routing attempt result: %w", err)
	}
	return true, nil
}

func (r *autoRoutingRepository) PrepareNextAutoRoutingAttempt(
	ctx context.Context,
	executionID, attemptNo, nextGroupID int64,
	evidenceCode string,
) (bool, error) {
	if executionID <= 0 || attemptNo <= 0 || nextGroupID <= 0 || evidenceCode == "" {
		return false, errors.New("invalid Auto routing next attempt identity")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	var currentAttempt int
	var settlementStatus string
	var candidates []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT status, current_attempt, candidate_group_ids, settlement_status
		FROM auto_route_executions
		WHERE id = $1
		FOR UPDATE
	`, executionID).Scan(&status, &currentAttempt, &candidates, &settlementStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("lock Auto routing execution for next attempt: %w", err)
	}
	if settlementStatus != "pending" ||
		status != service.AutoRouteExecutionDispatching || currentAttempt != int(attemptNo) {
		return false, nil
	}
	var candidateIDs []int64
	if err := json.Unmarshal(candidates, &candidateIDs); err != nil {
		return false, fmt.Errorf("decode Auto routing candidate snapshot: %w", err)
	}
	if !containsInt64(candidateIDs, nextGroupID) {
		return false, errors.New("next group is not in Auto routing candidate snapshot")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE auto_route_attempts
		SET status = 'failed', result_reference = $3,
		    completed_at = NOW(), updated_at = NOW()
		WHERE execution_id = $1 AND attempt_no = $2 AND status = 'dispatching'
	`, executionID, attemptNo, evidenceCode)
	if err != nil {
		return false, fmt.Errorf("fail Auto routing attempt before next group: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return false, err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE auto_route_executions
		SET status = 'prepared',
		    actual_group_id = $2,
		    result_reference = NULL,
		    evidence_code = NULL,
		    completed_at = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND status = 'dispatching' AND current_attempt = $3
		  AND settlement_status = 'pending'
	`, executionID, nextGroupID, attemptNo)
	if err != nil {
		return false, fmt.Errorf("prepare Auto routing next group: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit Auto routing next group: %w", err)
	}
	return true, nil
}

func (r *autoRoutingRepository) FailAutoRoutingAttempt(
	ctx context.Context,
	executionID int64,
	attemptNo int,
	evidenceCode string,
) (bool, error) {
	if executionID <= 0 || attemptNo <= 0 || evidenceCode == "" {
		return false, errors.New("invalid Auto routing failed attempt")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		UPDATE auto_route_executions
		SET status = 'failed', evidence_code = $2,
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'dispatching' AND current_attempt = $3
		  AND settlement_status = 'pending'
	`, executionID, evidenceCode, attemptNo)
	if err != nil {
		return false, fmt.Errorf("fail Auto routing execution: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return false, err
	} else if affected != 1 {
		return false, nil
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE auto_route_attempts
		SET status = 'failed', result_reference = $3,
		    completed_at = NOW(), updated_at = NOW()
		WHERE execution_id = $1 AND attempt_no = $2 AND status = 'dispatching'
	`, executionID, attemptNo, evidenceCode)
	if err != nil {
		return false, fmt.Errorf("fail Auto routing attempt: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return false, err
	} else if affected != 1 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit Auto routing failed attempt: %w", err)
	}
	return true, nil
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (r *autoRoutingRepository) GetPlaygroundAutoKeyBinding(ctx context.Context, userID, profileID int64) (*service.PlaygroundAutoKeyBinding, error) {
	binding := &service.PlaygroundAutoKeyBinding{}
	err := r.db.QueryRowContext(ctx, `
		SELECT user_id, profile_id, api_key_id, created_at, updated_at
		FROM playground_auto_key_bindings
		WHERE user_id = $1 AND profile_id = $2
	`, userID, profileID).Scan(&binding.UserID, &binding.ProfileID, &binding.APIKeyID, &binding.CreatedAt, &binding.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get playground Auto key binding: %w", err)
	}
	return binding, nil
}

func (r *autoRoutingRepository) UpsertPlaygroundAutoKeyBinding(ctx context.Context, binding *service.PlaygroundAutoKeyBinding) error {
	if binding == nil || binding.UserID <= 0 || binding.ProfileID <= 0 || binding.APIKeyID <= 0 {
		return errors.New("invalid playground Auto key binding")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var ownerID, profileID int64
	err = tx.QueryRowContext(ctx, `
		SELECT key.user_id, routing.profile_id
		FROM api_keys AS key
		JOIN auto_key_routing AS routing ON routing.api_key_id = key.id
		WHERE key.id = $1 AND key.deleted_at IS NULL AND key.group_id IS NULL
		FOR UPDATE OF key
	`, binding.APIKeyID).Scan(&ownerID, &profileID)
	if err != nil {
		return fmt.Errorf("validate playground Auto key binding: %w", err)
	}
	if ownerID != binding.UserID || profileID != binding.ProfileID {
		return errors.New("playground Auto key binding owner or profile mismatch")
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO playground_auto_key_bindings (user_id, profile_id, api_key_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, binding.UserID, binding.ProfileID, binding.APIKeyID)
	if err != nil {
		return fmt.Errorf("insert playground Auto key binding if absent: %w", err)
	}

	var winner service.PlaygroundAutoKeyBinding
	err = tx.QueryRowContext(ctx, `
		SELECT user_id, profile_id, api_key_id, created_at, updated_at
		FROM playground_auto_key_bindings
		WHERE user_id = $1 AND profile_id = $2
		FOR UPDATE
	`, binding.UserID, binding.ProfileID).Scan(
		&winner.UserID,
		&winner.ProfileID,
		&winner.APIKeyID,
		&winner.CreatedAt,
		&winner.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("playground Auto key is already bound to another profile")
	}
	if err != nil {
		return fmt.Errorf("read authoritative playground Auto key binding: %w", err)
	}
	*binding = winner
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit playground Auto key binding: %w", err)
	}
	return nil
}

var _ service.AutoRoutingRepository = (*autoRoutingRepository)(nil)
