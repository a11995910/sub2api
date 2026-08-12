-- 保留当前定制仓库中支持视频的平台，只清理无法路由视频请求的分组配置。
-- 字段沿用迁移 170/217 的 video_price_* / video_model_prices；本分支从未应用
-- 独立的 allow_video_generation 字段。

-- 清理前创建快照，避免管理员为其他平台设置的视频价格不可恢复。
-- CREATE TABLE IF NOT EXISTS ... AS SELECT 重复执行时不会覆盖已有快照，保持幂等。
CREATE TABLE IF NOT EXISTS groups_video_price_backup_220 AS
SELECT id AS group_id,
       platform,
       video_price_480p,
       video_price_720p,
       video_price_1080p,
       video_model_prices,
       now() AS backed_up_at
FROM groups
WHERE platform IS DISTINCT FROM 'grok'
  AND platform IS DISTINCT FROM 'openai'
  AND platform IS DISTINCT FROM 'composite'
  AND (
      video_price_480p IS NOT NULL
      OR video_price_720p IS NOT NULL
      OR video_price_1080p IS NOT NULL
      OR video_model_prices IS NOT NULL
  );

COMMENT ON TABLE groups_video_price_backup_220 IS
    '迁移 220 清空不支持视频的分组配置前的快照。OpenAI 支持定制视频，composite 可能路由到视频账号，两者均予以保留。确认无需回滚后可安全 DROP；回滚方式：UPDATE groups g SET video_price_480p = b.video_price_480p, ... FROM groups_video_price_backup_220 b WHERE g.id = b.group_id';

UPDATE groups
SET video_price_480p = NULL,
    video_price_720p = NULL,
    video_price_1080p = NULL,
    video_model_prices = NULL
WHERE platform IS DISTINCT FROM 'grok'
  AND platform IS DISTINCT FROM 'openai'
  AND platform IS DISTINCT FROM 'composite'
  AND (
      video_price_480p IS NOT NULL
      OR video_price_720p IS NOT NULL
      OR video_price_1080p IS NOT NULL
      OR video_model_prices IS NOT NULL
  );
