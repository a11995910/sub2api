-- 保存每次签到完成后的连续天数；0 保留给历史记录，由服务层兼容补算。
ALTER TABLE checkin_records
  ADD COLUMN IF NOT EXISTS consecutive_count INTEGER NOT NULL DEFAULT 0;
