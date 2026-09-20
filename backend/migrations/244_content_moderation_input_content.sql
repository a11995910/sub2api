-- 原始输入与摘要分开保存，仅管理员详情接口按需读取；NULL 表示历史记录未采集原文。
ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS input_content JSONB;
