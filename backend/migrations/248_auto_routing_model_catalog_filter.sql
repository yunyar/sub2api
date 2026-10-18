ALTER TABLE auto_group_profiles
    ADD COLUMN IF NOT EXISTS model_catalog_filter JSONB NOT NULL
        DEFAULT '{"enabled":false,"models":[]}'::jsonb;
