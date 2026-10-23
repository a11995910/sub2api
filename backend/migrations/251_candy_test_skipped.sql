-- 不可用账号仅记录跳过，不参与智力判断或自动调组。
ALTER TABLE candy_test_results DROP CONSTRAINT candy_test_results_status_check;
ALTER TABLE candy_test_results ADD CONSTRAINT candy_test_results_status_check
 CHECK (status IN ('running', 'normal', 'degraded', 'error', 'skipped'));
