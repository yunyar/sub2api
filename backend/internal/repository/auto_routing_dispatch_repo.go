package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *autoRoutingRepository) ReleaseAutoRoutingDispatchClaim(ctx context.Context, executionID, groupID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE auto_route_executions
		SET status = 'prepared', current_attempt = 0, updated_at = NOW()
		WHERE id = $1 AND actual_group_id = $2 AND status = 'dispatching'
		AND current_attempt = 1 AND settlement_status = 'pending'`, executionID, groupID)
	if err != nil {
		return err
	}
	if err := requireOneRow(result); err != nil {
		return service.ErrAutoRoutingExecutionStateConflict
	}
	result, err = tx.ExecContext(ctx, `DELETE FROM auto_route_attempts
		WHERE execution_id = $1 AND group_id = $2 AND attempt_no = 1 AND status = 'dispatching'`, executionID, groupID)
	if err != nil {
		return err
	}
	if err := requireOneRow(result); err != nil {
		return service.ErrAutoRoutingExecutionStateConflict
	}
	return tx.Commit()
}

func (r *autoRoutingRepository) RecordAutoRoutingDispatchSnapshot(ctx context.Context, executionID int64, attemptNo int, groupID, accountID int64, platform string, snapshot []byte) error {
	result, err := r.db.ExecContext(ctx, `UPDATE auto_route_attempts
		SET account_id = $1, platform = $2, billing_snapshot = $3::jsonb, updated_at = NOW()
		WHERE execution_id = $4 AND attempt_no = $5 AND group_id = $6
		AND status = 'dispatching'`,
		accountID, platform, string(snapshot), executionID, attemptNo, groupID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return service.ErrAutoRoutingExecutionStateConflict
	}
	return nil
}
