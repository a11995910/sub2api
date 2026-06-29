ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS image_4k_enhancement_enabled BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS image_4k_enhancement_group_id BIGINT NULL;

COMMENT ON COLUMN groups.image_4k_enhancement_enabled IS '是否在 4K 生图命中后调用指定图片分组做二段提升';
COMMENT ON COLUMN groups.image_4k_enhancement_group_id IS '4K 生图二段提升使用的目标图片分组 ID';
