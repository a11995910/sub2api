ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS image_4k_enhancement_model VARCHAR(100) NULL;

COMMENT ON COLUMN groups.image_4k_enhancement_model IS '4K 生图二段提升使用的目标图片模型；为空时沿用目标分组自动模型解析';
