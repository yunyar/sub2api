package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoRoutingMigrationPersistsRevisionedRoutingAndExecutionEvidence(t *testing.T) {
	content, err := FS.ReadFile("246_auto_routing.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	for _, expected := range []string{
		"CREATE TABLE IF NOT EXISTS auto_group_profiles",
		"CREATE TABLE IF NOT EXISTS auto_group_profile_members",
		"CREATE TABLE IF NOT EXISTS auto_key_routing",
		"CREATE TABLE IF NOT EXISTS auto_key_routing_groups",
		"ADD COLUMN IF NOT EXISTS routing_mode TEXT NOT NULL DEFAULT 'fixed'",
		"ADD COLUMN IF NOT EXISTS routing_revision BIGINT NOT NULL DEFAULT 0",
		"api_keys_routing_mode_check",
		"CREATE TABLE IF NOT EXISTS playground_auto_key_bindings",
		"CREATE TABLE IF NOT EXISTS auto_route_executions",
		"UNIQUE (api_key_id, request_id)",
		"CREATE TABLE IF NOT EXISTS auto_route_attempts",
		"UNIQUE (execution_id, attempt_no)",
		"billing_snapshot JSONB",
		"actual_group_id BIGINT REFERENCES groups(id)",
		"candidate_group_ids JSONB NOT NULL",
		"routing_revision BIGINT NOT NULL",
		"enforce_api_key_auto_routing_group_null",
		"enforce_auto_routing_key_group_null",
		"sync_api_key_auto_routing_mode",
		"FOR UPDATE",
		"ADD COLUMN IF NOT EXISTS auto_execution_id BIGINT",
		"ADD COLUMN IF NOT EXISTS actual_group_id BIGINT",
		"ADD COLUMN IF NOT EXISTS auto_attempt_no INTEGER",
		"ADD COLUMN IF NOT EXISTS pricing_snapshot JSONB",
		"ADD COLUMN IF NOT EXISTS routing_mode TEXT NOT NULL DEFAULT 'fixed'",
		"ADD COLUMN IF NOT EXISTS auto_profile_id BIGINT",
		"ADD COLUMN IF NOT EXISTS routing_revision BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS route_attempt_count INTEGER NOT NULL DEFAULT 0",
	} {
		require.Contains(t, sql, expected)
	}
	require.NotContains(t, sql, "ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS auto_profile_id")
}
