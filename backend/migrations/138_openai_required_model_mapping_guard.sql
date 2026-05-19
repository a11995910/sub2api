-- OpenAI 固定模型白名单账号自动补齐必须透传的模型。
-- 空 model_mapping 表示允许所有模型，不能写入白名单，否则会把全量能力收窄。

DROP TRIGGER IF EXISTS accounts_openai_gpt55_model_mapping_guard ON accounts;
DROP FUNCTION IF EXISTS ensure_openai_gpt55_model_mapping();

CREATE OR REPLACE FUNCTION openai_required_model_mapping_defaults()
RETURNS jsonb AS $$
BEGIN
  RETURN '{
    "codex-auto-review": "codex-auto-review",
    "gpt-4o-audio-preview": "gpt-4o-audio-preview",
    "gpt-4o-realtime-preview": "gpt-4o-realtime-preview",
    "gpt-5.2": "gpt-5.2",
    "gpt-5.2-2025-12-11": "gpt-5.2-2025-12-11",
    "gpt-5.2-chat-latest": "gpt-5.2-chat-latest",
    "gpt-5.2-pro": "gpt-5.2-pro",
    "gpt-5.2-pro-2025-12-11": "gpt-5.2-pro-2025-12-11",
    "gpt-5.3-codex": "gpt-5.3-codex",
    "gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
    "gpt-5.4": "gpt-5.4",
    "gpt-5.4-2026-03-05": "gpt-5.4-2026-03-05",
    "gpt-5.4-mini": "gpt-5.4-mini",
    "gpt-5.5": "gpt-5.5",
    "gpt-image-1": "gpt-image-1",
    "gpt-image-1.5": "gpt-image-1.5",
    "gpt-image-2": "gpt-image-2"
  }'::jsonb;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE OR REPLACE FUNCTION openai_required_model_mapping_is_candidate(mapping jsonb)
RETURNS boolean AS $$
BEGIN
  RETURN EXISTS (
    SELECT 1
    FROM jsonb_object_keys(mapping) AS k(model)
    WHERE k.model = 'codex-auto-review'
       OR k.model = 'gpt-5'
       OR k.model LIKE 'gpt-5.%'
       OR k.model LIKE 'gpt-5-%'
  );
END;
$$ LANGUAGE plpgsql IMMUTABLE;

UPDATE accounts
SET credentials = jsonb_set(
        credentials,
        '{model_mapping}',
        openai_required_model_mapping_defaults() || (credentials->'model_mapping'),
        true
    ),
    updated_at = NOW()
WHERE platform = 'openai'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials) = 'object'
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND credentials->'model_mapping' <> '{}'::jsonb
  AND openai_required_model_mapping_is_candidate(credentials->'model_mapping')
  AND EXISTS (
      SELECT 1
      FROM jsonb_object_keys(openai_required_model_mapping_defaults()) AS required(model)
      WHERE NOT (credentials->'model_mapping' ? required.model)
  );

CREATE OR REPLACE FUNCTION ensure_openai_required_model_mapping()
RETURNS trigger AS $$
BEGIN
  IF NEW.platform = 'openai'
     AND NEW.deleted_at IS NULL
     AND NEW.credentials IS NOT NULL
     AND jsonb_typeof(NEW.credentials) = 'object'
     AND jsonb_typeof(NEW.credentials->'model_mapping') = 'object'
     AND NEW.credentials->'model_mapping' <> '{}'::jsonb
     AND openai_required_model_mapping_is_candidate(NEW.credentials->'model_mapping')
     AND EXISTS (
       SELECT 1
       FROM jsonb_object_keys(openai_required_model_mapping_defaults()) AS required(model)
       WHERE NOT (NEW.credentials->'model_mapping' ? required.model)
     ) THEN
    NEW.credentials := jsonb_set(
      NEW.credentials,
      '{model_mapping}',
      openai_required_model_mapping_defaults() || (NEW.credentials->'model_mapping'),
      true
    );
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS accounts_openai_required_model_mapping_guard ON accounts;
CREATE TRIGGER accounts_openai_required_model_mapping_guard
BEFORE INSERT OR UPDATE OF platform, credentials ON accounts
FOR EACH ROW
EXECUTE FUNCTION ensure_openai_required_model_mapping();

COMMENT ON FUNCTION ensure_openai_required_model_mapping() IS 'OpenAI 非空固定 model_mapping 自动补齐 Codex、GPT-5、图片等必备模型，避免新增或同步账号遗漏模型白名单。';
