"""验证登录子进程的秘密隔离、输出脱敏和临时文件清理，不连接官方服务。"""
import json
import os
from pathlib import Path
import signal
import unittest
from unittest.mock import patch
import runner


class Process:
    def __init__(self, command, **kwargs):
        self.command = command
        self.kwargs = kwargs
        self.pid = 987654321
        self.returncode = 1
    def communicate(self, timeout):
        return b'password=synthetic-password token=synthetic-token EXCEL_ADDITIONAL_VERIFICATION_REQUIRED', None
    def wait(self):
        return self.returncode


class RunnerTests(unittest.TestCase):
    def test_secrets_only_in_environment_and_raw_errors_never_escape(self):
        processes = []
        paths = []
        def prepare(_root, temporary):
            paths.append(temporary)
            script = temporary / 'script.mjs'
            script.write_text('// 离线测试')
            return script
        def process(command, **kwargs):
            value = Process(command, **kwargs)
            processes.append(value)
            return value
        previous = (signal.getsignal(signal.SIGINT), signal.getsignal(signal.SIGTERM))
        try:
            with patch.object(runner, 'prepare_excel_runtime', side_effect=prepare), patch.object(runner.subprocess, 'Popen', side_effect=process), patch.object(runner.os, 'killpg') as kill, patch.dict(os.environ, {'OPENAI_API_KEY': 'must-not-inherit', 'AUTH_BASE': 'https://untrusted.invalid', 'CHATGPT_TOTP_SECRET': 'old-secret'}):
                result = runner.run({'email': 'synthetic@example.invalid', 'password': 'synthetic-password', 'workspace': 'workspace', 'proxy_url': 'http://user:synthetic-proxy@localhost:8080'})
                self.assertEqual(result, {'error': 'verification_required'})
                child = processes[0]
                for secret in ['synthetic-password', 'synthetic@example.invalid', 'synthetic-proxy']:
                    self.assertNotIn(secret, ' '.join(child.command))
                    self.assertNotIn(secret, json.dumps(result))
                self.assertEqual(child.kwargs['env']['CHATGPT_EMAIL'], 'synthetic@example.invalid')
                self.assertEqual(child.kwargs['env']['CHATGPT_TOTP_SECRET'], '')
                self.assertNotIn('OPENAI_API_KEY', child.kwargs['env'])
                self.assertNotIn('AUTH_BASE', child.kwargs['env'])
                self.assertEqual(child.kwargs['env']['CHATGPT_PROXY_URL'], 'http://user:synthetic-proxy@localhost:8080')
                kill.assert_called_once_with(child.pid, signal.SIGKILL)
            self.assertFalse(paths[0].exists())
        finally:
            signal.signal(signal.SIGINT, previous[0])
            signal.signal(signal.SIGTERM, previous[1])

    def test_success_is_validated_and_only_token_fields_are_returned(self):
        from test_openai_excel_oauth_adapter import ExcelAdapterTests
        credentials = ExcelAdapterTests().credentials()
        credentials['expires_at'] = '2099-01-01T00:00:00Z'
        credentials['unexpected'] = 'not-returned'
        class Success(Process):
            def communicate(self, timeout):
                target = Path(self.command[self.command.index('--sub2api-out') + 1])
                target.write_text(json.dumps({'accounts': [{'credentials': credentials}]}))
                self.returncode = 0
                return b'private-success-output', None
        def prepare(_root, temporary):
            script = temporary / 'script.mjs'
            script.write_text('// 离线成功路径')
            return script
        previous = (signal.getsignal(signal.SIGINT), signal.getsignal(signal.SIGTERM))
        try:
            with patch.object(runner, 'prepare_excel_runtime', side_effect=prepare), patch.object(runner.subprocess, 'Popen', Success), patch.object(runner.os, 'killpg'):
                result = runner.run({'email': 'test@example.invalid', 'password': 'synthetic', 'workspace': 'workspace-a'})
                self.assertEqual(result['token']['client_id'], credentials['client_id'])
                self.assertNotIn('expires_at', result['token'])
                self.assertNotIn('unexpected', result['token'])
                self.assertNotIn('private-success-output', json.dumps(result))
        finally:
            signal.signal(signal.SIGINT, previous[0])
            signal.signal(signal.SIGTERM, previous[1])

    def test_missing_identity_stops_before_starting_process(self):
        with patch.object(runner.subprocess, 'Popen') as process:
            self.assertEqual(runner.run({'email': 'a@example.invalid', 'password': 'synthetic'}), {'error': 'login_config_invalid'})
            process.assert_not_called()

    def test_unknown_failure_is_fixed_message(self):
        self.assertEqual(runner.classify('secret arbitrary callback?code=private'), 'login_failed')


if __name__ == '__main__':
    unittest.main()
