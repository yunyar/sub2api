package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoRoutingModelCatalogFilterMigrationAddsDisabledJSONBDefault(t *testing.T) {
	content, err := FS.ReadFile("248_auto_routing_model_catalog_filter.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ALTER TABLE auto_group_profiles")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS model_catalog_filter JSONB NOT NULL")
	require.Contains(t, sql, `DEFAULT '{"enabled":false,"models":[]}'::jsonb`)
}
