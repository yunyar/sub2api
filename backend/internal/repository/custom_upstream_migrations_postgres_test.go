package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestCustomUpstreamMigrationUpgradeWithPostgres(t *testing.T) {
	dsn := os.Getenv("PLAYGROUND_BILLING_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PLAYGROUND_BILLING_POSTGRES_DSN to an isolated PostgreSQL server")
	}
	root, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	databaseName := fmt.Sprintf("custom_upstream_upgrade_%d", time.Now().UnixNano())
	_, err = root.ExecContext(t.Context(), "CREATE DATABASE "+databaseName)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := root.ExecContext(context.Background(), "DROP DATABASE "+databaseName)
		require.NoError(t, cleanupErr)
	})
	connectionURL, err := url.Parse(dsn)
	require.NoError(t, err)
	connectionURL.Path = "/" + databaseName
	database, err := sql.Open("postgres", connectionURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	baseline := fstest.MapFS{}
	filenames, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	for _, filename := range filenames {
		if filename == "246_enable_gemini_image_generation_groups.sql" || filename == "247_scoped_admin_api_keys.sql" || filename == "248_group_image_count_billing.sql" {
			continue
		}
		content, readErr := migrations.FS.ReadFile(filename)
		require.NoError(t, readErr)
		baseline[filename] = &fstest.MapFile{Data: content, Mode: 0o444}
	}
	require.NoError(t, applyMigrationsFS(t.Context(), database, baseline))
	for _, filename := range []string{"246_auto_routing.sql", "247_auto_routing_cross_group_retry.sql", "248_auto_routing_model_catalog_filter.sql"} {
		var count int
		require.NoError(t, database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM schema_migrations WHERE filename=$1", filename).Scan(&count))
		require.Equal(t, 1, count)
	}
	require.NoError(t, ApplyMigrations(t.Context(), database))
	require.NoError(t, ApplyMigrations(t.Context(), database))
	for _, filename := range []string{"246_enable_gemini_image_generation_groups.sql", "247_scoped_admin_api_keys.sql", "248_group_image_count_billing.sql"} {
		var count int
		require.NoError(t, database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM schema_migrations WHERE filename=$1", filename).Scan(&count))
		require.Equal(t, 1, count)
	}
	var nullable, columnType, defaultValue string
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT is_nullable, data_type, column_default FROM information_schema.columns WHERE table_schema='public' AND table_name='groups' AND column_name='image_count_billing'").Scan(&nullable, &columnType, &defaultValue))
	require.Equal(t, "NO", nullable)
	require.Equal(t, "boolean", columnType)
	require.Equal(t, "false", defaultValue)
}
