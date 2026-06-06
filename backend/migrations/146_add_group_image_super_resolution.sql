ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS image_super_resolution_enabled BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN groups.image_super_resolution_enabled IS '是否对图片生成结果自动执行 4K 超分';
