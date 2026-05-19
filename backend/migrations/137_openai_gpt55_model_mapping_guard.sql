-- OpenAI GPT-5 系列账号使用非空模型白名单时，自动补齐 gpt-5.5。
-- 旧前端、外部同步或批量导入可能仍提交旧白名单；缺少该项会导致调度层把真实可用账号过滤掉。
-- 空 model_mapping 表示允许所有模型，图片等非 GPT-5 白名单也不应被扩大。

UPDATE accounts
SET credentials = jsonb_set(
        credentials,
        '{model_mapping}',
        (credentials->'model_mapping') || '{"gpt-5.5":"gpt-5.5"}'::jsonb,
        true
    ),
    updated_at = NOW()
WHERE platform = 'openai'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials) = 'object'
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND credentials->'model_mapping' <> '{}'::jsonb
  AND NOT (credentials->'model_mapping' ? 'gpt-5.5')
  AND EXISTS (
      SELECT 1
      FROM jsonb_object_keys(credentials->'model_mapping') AS k(model)
      WHERE k.model = 'gpt-5'
         OR k.model LIKE 'gpt-5.%'
         OR k.model LIKE 'gpt-5-%'
  );

CREATE OR REPLACE FUNCTION ensure_openai_gpt55_model_mapping()
RETURNS trigger AS $$
BEGIN
  IF NEW.platform = 'openai'
     AND NEW.deleted_at IS NULL
     AND NEW.credentials IS NOT NULL
     AND jsonb_typeof(NEW.credentials) = 'object'
     AND jsonb_typeof(NEW.credentials->'model_mapping') = 'object'
     AND NEW.credentials->'model_mapping' <> '{}'::jsonb
     AND NOT (NEW.credentials->'model_mapping' ? 'gpt-5.5')
     AND EXISTS (
       SELECT 1
       FROM jsonb_object_keys(NEW.credentials->'model_mapping') AS k(model)
       WHERE k.model = 'gpt-5'
          OR k.model LIKE 'gpt-5.%'
          OR k.model LIKE 'gpt-5-%'
     ) THEN
    NEW.credentials := jsonb_set(
      NEW.credentials,
      '{model_mapping}',
      (NEW.credentials->'model_mapping') || '{"gpt-5.5":"gpt-5.5"}'::jsonb,
      true
    );
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS accounts_openai_gpt55_model_mapping_guard ON accounts;
CREATE TRIGGER accounts_openai_gpt55_model_mapping_guard
BEFORE INSERT OR UPDATE OF platform, credentials ON accounts
FOR EACH ROW
EXECUTE FUNCTION ensure_openai_gpt55_model_mapping();

COMMENT ON FUNCTION ensure_openai_gpt55_model_mapping() IS 'OpenAI GPT-5 系列非空 model_mapping 自动补 gpt-5.5，避免新增或同步账号遗漏模型白名单。';
