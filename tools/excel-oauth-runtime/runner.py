"""从标准输入接收单次授权资料，仅向标准输出返回凭据或固定错误码。"""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
from openai_excel_oauth_adapter import prepare_excel_runtime, validate_excel_credentials


def classify(output):
    for marker, reason in (
        ('EXCEL_ADDITIONAL_VERIFICATION_REQUIRED', 'verification_required'),
        ('security-check', 'verification_required'), ('PROXY_RISK_CONTROL', 'verification_required'),
        ('EXCEL_EXPECTED_WORKSPACE_NOT_LISTED', 'workspace_mismatch'),
        ('Password was rejected', 'password_rejected'), ('2FA key was rejected', 'totp_rejected'),
    ):
        if marker in output:
            return reason
    return 'login_failed'


def run(data):
    root = Path(__file__).resolve().parent
    dependency = Path(os.environ.get('EXCEL_TOSUB2_ROOT', str(root / 'tosub2')))
    node = os.environ.get('EXCEL_NODE', str(root / 'node'))
    if not data.get('email') or not data.get('password') or not data.get('workspace'):
        return {'error': 'login_config_invalid'}
    with tempfile.TemporaryDirectory(prefix='excel-oauth-') as directory:
        temporary = Path(directory)
        script = prepare_excel_runtime(dependency, temporary)
        output = temporary / 'result.json'
        # 登录秘密不进入进程参数；运行目录仅本用户可读，离开任务后清理。
        env = {key: value for key, value in os.environ.items() if key in (
            'PATH', 'HOME', 'TMPDIR', 'SSL_CERT_FILE', 'CURL_CA_BUNDLE', 'PYTHONHOME',
        )}
        env.update(CHATGPT_LOGIN_PASSWORD=data['password'], CHATGPT_TOTP_SECRET=data.get('totp_secret', ''),
                   OPENAI_EXCEL_EXPECTED_WORKSPACE=data['workspace'], CHATGPT_PROXY_MAX_ATTEMPTS='1',
                   NODE_EXECUTABLE=node, TOSUB2_PYTHON=os.environ.get('EXCEL_PYTHON', str(root / 'python')))
        # 邮箱和代理也通过环境传递，避免带认证信息的代理暴露在进程参数中。
        env['CHATGPT_PROXY_URL'] = data.get('proxy_url', '')
        env['CHATGPT_EMAIL'] = data['email']
        command = [node, str(script), '--output-mode', 'sub2api', '--sub2api-out', str(output)]
        process = subprocess.Popen(command, cwd=directory, env=env, stdin=subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
        def stop(_sig, _frame):
            raise InterruptedError()
        signal.signal(signal.SIGINT, stop)
        signal.signal(signal.SIGTERM, stop)
        try:
            raw, _ = process.communicate(timeout=600)
            if process.returncode:
                return {'error': classify(raw.decode('utf-8', errors='replace'))}
            payload = json.loads(output.read_text())
            accounts = payload.get('accounts', [])
            if len(accounts) != 1:
                return {'error': 'invalid_result'}
            credentials = accounts[0]['credentials']
            validate_excel_credentials(credentials, data['email'], data['workspace'])
            # 服务端另行验证用户、工作区和官方客户端，不信任返回的派生字段。
            return {'token': {key: credentials.get(key, '') for key in ('access_token', 'refresh_token', 'id_token', 'client_id')}}
        finally:
            # 同时结束协议进程及它启动的传输子进程，取消时也不能留下登录任务。
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()


def main():
    os.umask(0o077)
    try:
        if '--check' in sys.argv:
            root = Path(__file__).resolve().parent
            dependency = Path(os.environ.get('EXCEL_TOSUB2_ROOT', str(root / 'tosub2')))
            with tempfile.TemporaryDirectory() as directory:
                prepare_excel_runtime(dependency, Path(directory))
            import curl_cffi
            result = {'ready': True}
        else:
            result = run(json.loads(sys.stdin.read(65537)))
    except InterruptedError:
        result = {'error': 'cancelled'}
    except subprocess.TimeoutExpired:
        result = {'error': 'timeout'}
    except Exception:
        # 外部异常可能包含密码、代理或授权回调，禁止输出异常正文。
        result = {'error': 'runtime_error'}
    print(json.dumps(result))
    return 0 if 'error' not in result else 1


if __name__ == '__main__':
    sys.exit(main())
