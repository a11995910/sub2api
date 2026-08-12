-- 迁移 220 的早期版本误将 OpenAI 分组视为不支持视频并清空了定制价格。
-- 仅恢复四个价格字段仍全部为空的记录，避免覆盖迁移 220 后的人工配置。
DO $$
BEGIN
    IF to_regclass('public.groups_video_price_backup_220') IS NOT NULL THEN
        UPDATE groups AS g
        SET video_price_480p = b.video_price_480p,
            video_price_720p = b.video_price_720p,
            video_price_1080p = b.video_price_1080p,
            video_model_prices = b.video_model_prices
        FROM groups_video_price_backup_220 AS b
        WHERE g.id = b.group_id
          AND g.platform = 'openai'
          AND b.platform = 'openai'
          AND g.video_price_480p IS NULL
          AND g.video_price_720p IS NULL
          AND g.video_price_1080p IS NULL
          AND g.video_model_prices IS NULL;
    END IF;
END
$$;
