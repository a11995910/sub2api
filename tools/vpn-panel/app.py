"""独立订阅后台；运行数据与 Sub2API 数据库隔离。"""
import base64
import hashlib
import hmac
import io
import json
import os
import secrets
import sqlite3
import time
from contextlib import contextmanager
from pathlib import Path
from urllib.parse import quote, urlparse

import qrcode
import qrcode.image.svg
import yaml
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import FileResponse, JSONResponse, Response
from pydantic import BaseModel, Field

ROOT = Path(__file__).resolve().parent
DATA = Path(os.environ.get('VPN_DATA_DIR', '/var/lib/sub-vpn'))
BASE = '/vpn'
PUBLIC = os.environ.get('VPN_PUBLIC_URL', 'https://38.47.115.99/vpn').rstrip('/')
COOKIE = 'vpn_session'
app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)


@contextmanager
def db():
    connection = sqlite3.connect(DATA / 'panel.db')
    connection.row_factory = sqlite3.Row
    try:
        with connection:
            yield connection
    finally:
        connection.close()


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


def password_hash(value, salt=None):
    salt = salt or secrets.token_hex(16)
    key = hashlib.scrypt(value.encode(), salt=salt.encode(), n=16384, r=8, p=1).hex()
    return salt + ':' + key


def password_ok(value, stored):
    return hmac.compare_digest(password_hash(value, stored.split(':')[0]), stored)


def init():
    DATA.mkdir(parents=True, exist_ok=True, mode=0o700)
    with db() as c:
        c.executescript('''
        CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,expires INTEGER NOT NULL);
        CREATE TABLE IF NOT EXISTS attempts(ip TEXT PRIMARY KEY,count INTEGER NOT NULL,until INTEGER NOT NULL);
        CREATE TABLE IF NOT EXISTS nodes(id INTEGER PRIMARY KEY,name TEXT NOT NULL UNIQUE,category TEXT NOT NULL,
          protocol TEXT NOT NULL,host TEXT NOT NULL,port INTEGER NOT NULL,username TEXT NOT NULL DEFAULT '',
          password TEXT NOT NULL DEFAULT '',cipher TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,
          managed INTEGER NOT NULL DEFAULT 0,note TEXT NOT NULL DEFAULT '',relay INTEGER NOT NULL DEFAULT 0);
        ''')
        if 'relay' not in {row['name'] for row in c.execute('PRAGMA table_info(nodes)')}:
            c.execute('ALTER TABLE nodes ADD COLUMN relay INTEGER NOT NULL DEFAULT 0')
    os.chmod(DATA / 'panel.db', 0o600)


init()


def setting(key):
    with db() as c:
        row = c.execute('SELECT value FROM settings WHERE key=?', (key,)).fetchone()
    if not row:
        raise HTTPException(503, '服务尚未初始化')
    return row['value']


def short_subscription_token():
    # 从现有随机凭据派生 96 位短码；重置原凭据时短码同步失效。
    raw = hashlib.sha256(setting('subscription').encode()).digest()[:12]
    return base64.urlsafe_b64encode(raw).decode()


def subscription_url():
    public = urlparse(PUBLIC)
    return public.scheme + '://' + public.netloc + '/s/' + short_subscription_token()


@app.middleware('http')
async def security(request: Request, call_next):
    path = request.url.path
    if path.startswith(BASE + '/api/'):
        if request.method not in ('GET', 'HEAD'):
            if request.headers.get('origin') != urlparse(PUBLIC).scheme + '://' + urlparse(PUBLIC).netloc:
                return JSONResponse({'detail': '请求来源不匹配，请从管理页面操作'}, status_code=403)
        if path != BASE + '/api/login':
            token = request.cookies.get(COOKIE, '')
            with db() as c:
                session = c.execute('SELECT expires FROM sessions WHERE token=?', (digest(token),)).fetchone()
            if not session or session['expires'] < time.time():
                return JSONResponse({'detail': '请先登录'}, status_code=401)
    response = await call_next(request)
    response.headers['Cache-Control'] = 'no-store'
    response.headers['X-Content-Type-Options'] = 'nosniff'
    response.headers['Referrer-Policy'] = 'no-referrer'
    response.headers['X-Frame-Options'] = 'DENY'
    response.headers['Content-Security-Policy'] = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
    return response


class Login(BaseModel):
    username: str = Field(max_length=100)
    password: str = Field(max_length=256)


@app.post(BASE + '/api/login')
def login(body: Login, request: Request):
    ip = request.client.host
    now = int(time.time())
    with db() as c:
        c.execute('DELETE FROM attempts WHERE until<?', (now,))
        row = c.execute('SELECT count FROM attempts WHERE ip=?', (ip,)).fetchone()
        if row and row['count'] >= 10:
            raise HTTPException(429, '尝试次数过多，请 15 分钟后重试')
        valid_password = password_ok(body.password, setting('password'))
        if body.username != setting('username') or not valid_password:
            c.execute('INSERT INTO attempts VALUES (?,1,?) ON CONFLICT(ip) DO UPDATE SET count=count+1', (ip, now + 900))
            c.commit()
            raise HTTPException(401, '账号或密码不正确')
        c.execute('DELETE FROM attempts WHERE ip=?', (ip,))
        c.execute('DELETE FROM sessions WHERE expires<?', (now,))
        token = secrets.token_urlsafe(32)
        c.execute('INSERT INTO sessions VALUES (?,?)', (digest(token), now + 43200))
    response = JSONResponse({'ok': True})
    response.set_cookie(COOKIE, token, max_age=43200, secure=True, httponly=True, samesite='strict', path=BASE)
    return response


@app.post(BASE + '/api/logout')
def logout(request: Request):
    with db() as c:
        c.execute('DELETE FROM sessions WHERE token=?', (digest(request.cookies.get(COOKIE, '')),))
    response = JSONResponse({'ok': True})
    response.delete_cookie(COOKIE, path=BASE)
    return response


@app.get(BASE + '/api/state')
def state():
    with db() as c:
        rows = c.execute('SELECT * FROM nodes ORDER BY managed DESC, id').fetchall()
    nodes = []
    for row in rows:
        node = dict(row)
        node['has_password'] = bool(node.pop('password'))
        nodes.append(node)
    return {'nodes': nodes, 'subscription_url': subscription_url(), 'username': setting('username')}


class Node(BaseModel):
    name: str = Field(min_length=1, max_length=100)
    category: str = Field(min_length=1, max_length=50)
    protocol: str
    host: str = Field(min_length=1, max_length=253)
    port: int = Field(ge=1, le=65535)
    username: str = Field(default='', max_length=256)
    password: str = Field(default='', max_length=512)
    cipher: str = ''
    enabled: bool = True
    relay: bool = False
    note: str = Field(default='', max_length=500)

    def values_checked(self):
        data = self.model_dump()
        data['name'] = self.name.strip()
        data['category'] = self.category.strip()
        data['host'] = self.host.strip().strip('[]')
        if not data['name'] or not data['category'] or not data['host'] or any(ch in data['host'] for ch in '/?#@ \t\r\n'):
            raise HTTPException(400, '名称、分类或服务器地址不正确；地址只填写 IP 或域名')
        if self.name.strip() in ('DIRECT', 'REJECT', '节点选择', '自动选择'):
            raise HTTPException(400, '此名称为订阅保留名称')
        if self.protocol not in ('socks5', 'http', 'https', 'ss', 'anytls'):
            raise HTTPException(400, '不支持此协议')
        if self.protocol == 'ss' and self.cipher not in ('aes-128-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305', '2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm'):
            raise HTTPException(400, '请选择有效的 Shadowsocks 加密方式')
        return data


def save_node(body, node_id=None):
    data = body.values_checked()
    with db() as c:
        previous = c.execute('SELECT * FROM nodes WHERE id=?', (node_id,)).fetchone() if node_id else None
        if node_id and not previous:
            raise HTTPException(404, '节点不存在')
        if previous and previous['managed']:
            for key in ('protocol', 'host', 'port', 'username', 'cipher'):
                if data[key] != previous[key]:
                    raise HTTPException(400, '自建节点的连接参数由服务管理，不能在这里修改')
            if data['relay']:
                raise HTTPException(400, '自建节点不能经自身中转')
            if not data['enabled'] and c.execute('SELECT 1 FROM nodes WHERE relay=1 AND enabled=1 LIMIT 1').fetchone():
                raise HTTPException(400, '仍有启用的节点经自建节点中转，请先关闭这些节点的中转或停用它们')
            if data['password']:
                raise HTTPException(400, '自建节点密码由服务管理')
        if data['relay'] and data['enabled'] and not c.execute('SELECT 1 FROM nodes WHERE managed=1 AND enabled=1 LIMIT 1').fetchone():
            raise HTTPException(400, '请先启用自建节点，再使用中转')
        if previous and not data['password']:
            data['password'] = previous['password']
        if data['protocol'] in ('ss', 'anytls') and not data['password']:
            raise HTTPException(400, 'Shadowsocks / AnyTLS 必须填写密码')
        if data['protocol'] == 'ss' and data['cipher'].startswith('2022-'):
            try:
                key = base64.b64decode(data['password'], validate=True)
                expected = 16 if 'aes-128' in data['cipher'] else 32
                if len(key) != expected:
                    raise ValueError()
            except ValueError:
                raise HTTPException(400, '此加密方式需要指定长度的 Base64 密钥')
        try:
            if node_id:
                c.execute('UPDATE nodes SET ' + ','.join(k+'=?' for k in data) + ' WHERE id=?', [*data.values(), node_id])
            else:
                c.execute('INSERT INTO nodes ('+','.join(data)+') VALUES ('+','.join('?' for _ in data)+')', list(data.values()))
        except sqlite3.IntegrityError:
            raise HTTPException(409, '节点名称已存在，请使用不同名称')
    return {'ok': True}


@app.post(BASE + '/api/nodes')
def create_node(body: Node):
    return save_node(body)


@app.put(BASE + '/api/nodes/{node_id}')
def update_node(node_id: int, body: Node):
    return save_node(body, node_id)


@app.delete(BASE + '/api/nodes/{node_id}')
def delete_node(node_id: int):
    with db() as c:
        row = c.execute('SELECT managed FROM nodes WHERE id=?', (node_id,)).fetchone()
        if not row:
            raise HTTPException(404, '节点不存在')
        if row['managed']:
            raise HTTPException(400, '自建节点不能删除，可从订阅中停用')
        c.execute('DELETE FROM nodes WHERE id=?', (node_id,))
    return {'ok': True}


@app.post(BASE + '/api/rotate')
def rotate():
    with db() as c:
        c.execute("UPDATE settings SET value=? WHERE key='subscription'", (secrets.token_urlsafe(32),))
    return {'ok': True}


class PasswordChange(BaseModel):
    old_password: str = Field(max_length=256)
    new_password: str = Field(min_length=12, max_length=256)


@app.post(BASE + '/api/password')
def change_password(body: PasswordChange):
    if not password_ok(body.old_password, setting('password')):
        raise HTTPException(400, '当前密码不正确')
    with db() as c:
        c.execute("UPDATE settings SET value=? WHERE key='password'", (password_hash(body.new_password),))
        c.execute('DELETE FROM sessions')
    return {'ok': True}


def clash_node(row):
    node = {'name': row['name'], 'type': 'http' if row['protocol'] == 'https' else row['protocol'], 'server': row['host'], 'port': row['port']}
    if row['protocol'] == 'anytls':
        node.update(password=row['password'], sni=row['host'], udp=True, **{'skip-cert-verify': False})
    elif row['protocol'] == 'ss':
        node.update(cipher=row['cipher'], password=row['password'], udp=True)
    else:
        if row['username']:
            node['username'] = row['username']
        if row['password']:
            node['password'] = row['password']
        if row['protocol'] == 'https':
            node['tls'] = True
            node['skip-cert-verify'] = False
    return node


def node_uri(row):
    host = '[' + row['host'] + ']' if ':' in row['host'] else row['host']
    name = quote(row['name'])
    if row['protocol'] == 'anytls':
        raise HTTPException(400, '当前直连格式尚未实现 AnyTLS 导出，请使用 Clash/Mihomo')
    if row['protocol'] == 'ss':
        user = base64.urlsafe_b64encode((row['cipher']+':'+row['password']).encode()).decode().rstrip('=')
        return f'ss://{user}@{host}:{row["port"]}#{name}'
    auth = base64.b64encode((row['username']+':'+row['password']).encode()).decode()
    scheme = {'socks5': 'socks', 'http': 'http', 'https': 'https'}[row['protocol']]
    return f'{scheme}://{auth}@{host}:{row["port"]}#{name}'


@app.get('/s/{token}')
@app.get(BASE + '/sub/{token}')
def subscription(token: str, format: str = 'clash'):
    if not (hmac.compare_digest(token.encode(), setting('subscription').encode()) or
            hmac.compare_digest(token.encode(), short_subscription_token().encode())):
        raise HTTPException(404, '订阅不存在或已重置')
    with db() as c:
        rows = c.execute('SELECT * FROM nodes WHERE enabled=1 ORDER BY managed DESC,id').fetchall()
    if not rows:
        raise HTTPException(503, '没有启用的节点，请联系管理员')
    if format == 'shadowrocket':
        if any(row['protocol'] == 'anytls' for row in rows):
            raise HTTPException(400, '当前订阅包含 AnyTLS，自建节点请使用 Clash/Mihomo 格式')
        text = base64.b64encode('\n'.join(node_uri(row) for row in rows).encode()).decode()
        return Response(text, media_type='text/plain', headers={'profile-update-interval': '24'})
    if format != 'clash':
        raise HTTPException(400, '不支持此订阅格式')
    names = [row['name'] for row in rows]
    proxies = [clash_node(row) for row in rows]
    own_row = next((row for row in rows if row['managed'] and row['protocol'] == 'anytls'), None) or next((row for row in rows if row['managed']), None)
    own = own_row['name'] if own_row else None
    for node, row in zip(proxies, rows):
        if row['relay']:
            if not own:
                raise HTTPException(503, '中转节点未启用，请联系管理员')
            node['dialer-proxy'] = own
    config = {'mixed-port': 7890, 'allow-lan': False, 'mode': 'rule', 'log-level': 'warning',
              'proxies': proxies,
              'proxy-groups': [{'name': '节点选择', 'type': 'select', 'proxies': names + ['DIRECT']}],
              'rules': ['GEOIP,LAN,DIRECT', 'GEOIP,CN,DIRECT', 'MATCH,节点选择']}
    return Response(yaml.safe_dump(config, allow_unicode=True, sort_keys=False), media_type='text/yaml',
                    headers={'Content-Disposition': 'attachment; filename="sub-vpn.yaml"', 'profile-update-interval': '24'})


@app.get(BASE + '/api/qr')
def qr(format: str = 'clash'):
    if format not in ('clash', 'shadowrocket'):
        raise HTTPException(400, '不支持此订阅格式')
    url = subscription_url() + ('?format=shadowrocket' if format == 'shadowrocket' else '')
    image = qrcode.make(url, image_factory=qrcode.image.svg.SvgPathImage, border=4)
    output = io.BytesIO()
    image.save(output)
    return Response(output.getvalue(), media_type='image/svg+xml')


@app.get(BASE + '/health')
def health():
    return {'status': 'ok'}


@app.get(BASE + '/')
def index():
    return FileResponse(ROOT / 'static/index.html')


@app.get(BASE + '/static/{name}')
def static(name: str):
    if name not in ('app.js', 'style.css'):
        raise HTTPException(404)
    return FileResponse(ROOT / 'static' / name)
