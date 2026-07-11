ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS image_response_format VARCHAR(16) NOT NULL DEFAULT 'b64_json';

COMMENT ON COLUMN groups.image_response_format IS '图片响应默认传输方式：b64_json 或 url；客户显式 response_format 优先';
