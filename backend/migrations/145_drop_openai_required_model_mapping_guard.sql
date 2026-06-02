-- 停用 OpenAI model_mapping 自动补齐保护。
-- OpenAI 官方模型支持会变化，模型限制必须以管理员当前配置为准，不能继续写入旧模型白名单。

DROP TRIGGER IF EXISTS accounts_openai_required_model_mapping_guard ON accounts;
DROP FUNCTION IF EXISTS ensure_openai_required_model_mapping();
DROP FUNCTION IF EXISTS openai_required_model_mapping_is_candidate(jsonb);
DROP FUNCTION IF EXISTS openai_required_model_mapping_defaults();

DROP TRIGGER IF EXISTS accounts_openai_gpt55_model_mapping_guard ON accounts;
DROP FUNCTION IF EXISTS ensure_openai_gpt55_model_mapping();
