-- 动态提取的代理没有持久代理 ID，单独标记来源，避免将其误记为直连。
ALTER TABLE openai_healthy_turn_state_probes
    ADD COLUMN IF NOT EXISTS temporary_proxy BOOLEAN NOT NULL DEFAULT FALSE;
