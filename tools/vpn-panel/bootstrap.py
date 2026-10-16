"""从受限运行文件初始化；不覆盖已存在的管理数据。"""
import json
import os
import secrets
import sys
from pathlib import Path

from app import DATA, db, password_hash


def bootstrap(source):
    with db() as c:
        if c.execute('SELECT count(*) FROM settings').fetchone()[0]:
            raise SystemExit('管理数据已存在，拒绝覆盖')
        password = secrets.token_urlsafe(21)
        node_password = secrets.token_urlsafe(32)
        c.executemany('INSERT INTO settings VALUES (?,?)', [('username', 'admin'), ('password', password_hash(password)), ('subscription', secrets.token_urlsafe(32))])
        c.execute('INSERT INTO nodes(name,category,protocol,host,port,password,cipher,managed,note) VALUES (?,?,?,?,?,?,?,?,?)',
                  ('自建 · 新跳板 38.47.115.99', '自建节点', 'ss', '38.47.115.99', 24443, node_password, 'aes-256-gcm', 1, '新跳板自建加密节点；支持 TCP / UDP'))
        wanted = {'surflare 住宅 IP', 'cc 静态代理', '静态代理107'}
        rows = json.loads(Path(source).read_text())
        found = {row['name'] for row in rows if row['name'] in wanted}
        if found != wanted:
            raise SystemExit('来源节点不完整，拒绝初始化')
        for row in rows:
            if row['name'] not in wanted:
                continue
            note = '从线上 Sub 导入，2026-10-16 实测出口可用。'
            if row['status'] == 'expired':
                note += '来源记录已标记过期，请核对续费；实测仍可连接。'
            c.execute('INSERT INTO nodes(name,category,protocol,host,port,username,password,note) VALUES (?,?,?,?,?,?,?,?)',
                      (row['name'], row['name'], row['protocol'], row['host'], row['port'], row['username'] or '', row['password'] or '', note))
    config = {'log': {'level': 'warn'}, 'inbounds': [{'type': 'shadowsocks', 'tag': 'own-ss', 'listen': '0.0.0.0', 'listen_port': 24443, 'method': 'aes-256-gcm', 'password': node_password}], 'outbounds': [{'type': 'direct', 'tag': 'direct'}]}
    for filename, contents in [('sing-box.json', json.dumps(config)), ('admin-initial.txt', 'admin\n' + password + '\n')]:
        path = DATA / filename
        fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(fd, 'w') as handle:
            handle.write(contents)
    print('已初始化 4 个节点；初始管理凭据位于受限运行目录。')


if __name__ == '__main__':
    bootstrap(sys.argv[1])
