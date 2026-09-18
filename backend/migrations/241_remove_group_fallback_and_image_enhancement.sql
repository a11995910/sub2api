-- 下线分组自动承接，以及自动 4K 超分、2K 超分和 4K 提升功能。
DROP INDEX IF EXISTS idx_groups_auto_fallback_group_id;

ALTER TABLE api_keys
    DROP COLUMN IF EXISTS auto_group_fallback_enabled;

ALTER TABLE groups
    DROP COLUMN IF EXISTS auto_fallback_group_id,
    DROP COLUMN IF EXISTS image_super_resolution_enabled,
    DROP COLUMN IF EXISTS image_2k_enhancement_enabled,
    DROP COLUMN IF EXISTS image_2k_enhancement_group_id,
    DROP COLUMN IF EXISTS image_4k_enhancement_enabled,
    DROP COLUMN IF EXISTS image_4k_enhancement_group_id,
    DROP COLUMN IF EXISTS image_4k_enhancement_model;
