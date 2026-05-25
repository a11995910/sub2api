CREATE TABLE IF NOT EXISTS checkin_records (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  checkin_date DATE NOT NULL,
  daily_reward DECIMAL(20,8) NOT NULL DEFAULT 0,
  extra_reward DECIMAL(20,8) NOT NULL DEFAULT 0,
  month_count INTEGER NOT NULL DEFAULT 0,
  extra_milestones JSONB NOT NULL DEFAULT '[]'::jsonb,
  checked_in_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, checkin_date)
);
