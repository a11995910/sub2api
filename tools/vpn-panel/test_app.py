"""验证鉴权、凭据保护、节点修改与订阅格式。"""
import base64
import os
import tempfile
import unittest

_test_data = tempfile.TemporaryDirectory()
os.environ['VPN_DATA_DIR'] = _test_data.name
os.environ['VPN_PUBLIC_URL'] = 'https://testserver/vpn'
import app
from fastapi.testclient import TestClient
import yaml


class PanelTests(unittest.TestCase):
    def setUp(self):
        with app.db() as c:
            for table in ('settings', 'nodes', 'sessions', 'attempts'):
                c.execute('DELETE FROM '+table)
            c.executemany('INSERT INTO settings VALUES (?,?)', [('username','admin'),('password',app.password_hash('test-password-123')),('subscription','test-token')])
        self.client = TestClient(app.app, base_url='https://testserver', headers={'Origin':'https://testserver'})

    def login(self):
        self.assertEqual(self.client.post('/vpn/api/login',json={'username':'admin','password':'test-password-123'}).status_code,200)

    def node(self, **extra):
        return dict(name='住宅测试',category='住宅',protocol='socks5',host='192.0.2.10',port=1080,username='tester',password='proxy-secret',**extra)

    def test_access_and_origin(self):
        self.assertEqual(self.client.get('/vpn/api/state').status_code,401)
        self.assertEqual(self.client.get('/vpn/api/qr').status_code,401)
        self.assertEqual(self.client.post('/vpn/api/login',headers={'Origin':'https://evil.test'},json={'username':'admin','password':'test-password-123'}).status_code,403)
        self.login()
        self.assertEqual(self.client.get('/vpn/api/state').status_code,200)
        self.assertIn('HttpOnly', self.client.post('/vpn/api/login',json={'username':'admin','password':'test-password-123'}).headers['set-cookie'])

    def test_edit_preserves_secret_and_disable(self):
        self.login()
        self.assertEqual(self.client.post('/vpn/api/nodes',json=self.node()).status_code,200)
        n=self.client.get('/vpn/api/state').json()['nodes'][0]
        self.assertNotIn('password',n)
        self.assertTrue(n['has_password'])
        body=self.node();body.update(password='',name='住宅改名')
        self.assertEqual(self.client.put('/vpn/api/nodes/'+str(n['id']),json=body).status_code,200)
        result=yaml.safe_load(self.client.get('/vpn/sub/test-token').text)
        self.assertEqual(result['proxies'][0]['password'],'proxy-secret')
        self.assertEqual(result['proxies'][0]['name'],'住宅改名')
        body['enabled']=False
        self.client.put('/vpn/api/nodes/'+str(n['id']),json=body)
        self.assertEqual(self.client.get('/vpn/sub/test-token').status_code,503)

    def test_rotation_and_formats(self):
        self.login();self.client.post('/vpn/api/nodes',json=self.node())
        self.assertIn('socks://',base64.b64decode(self.client.get('/vpn/sub/test-token?format=shadowrocket').text).decode())
        self.assertIn('<svg',self.client.get('/vpn/api/qr').text)
        self.assertEqual(self.client.get('/vpn/sub/wrong').status_code,404)
        self.client.post('/vpn/api/rotate')
        self.assertEqual(self.client.get('/vpn/sub/test-token').status_code,404)
        url=self.client.get('/vpn/api/state').json()['subscription_url']
        self.assertEqual(self.client.get(url).status_code,200)

    def test_password_revokes_sessions(self):
        self.login()
        result=self.client.post('/vpn/api/password',json={'old_password':'test-password-123','new_password':'new-long-password-123'})
        self.assertEqual(result.status_code,200)
        self.assertEqual(self.client.get('/vpn/api/state').status_code,401)

    def test_name_conflict_managed_and_invalid_address(self):
        self.login()
        self.client.post('/vpn/api/nodes',json=self.node())
        self.assertEqual(self.client.post('/vpn/api/nodes',json=self.node()).status_code,409)
        with app.db() as c:
            c.execute('UPDATE nodes SET managed=1')
        node=self.client.get('/vpn/api/state').json()['nodes'][0]
        self.assertEqual(self.client.delete('/vpn/api/nodes/'+str(node['id'])).status_code,400)
        body=self.node();body['host']='https://example.com'
        self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,400)
        body=self.node();body['host']='192.0.2.11'
        self.assertEqual(self.client.put('/vpn/api/nodes/'+str(node['id']),json=body).status_code,400)

    def test_relay_subscription_and_dependency(self):
        self.login()
        with app.db() as c:
            c.execute("INSERT INTO nodes(name,category,protocol,host,port,password,cipher,managed) VALUES ('自建','自建','ss','192.0.2.1',24443,'own-secret','aes-256-gcm',1)")
        body=self.node(relay=True)
        self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,200)
        result=yaml.safe_load(self.client.get('/vpn/sub/test-token').text)
        self.assertEqual(result['proxies'][1]['dialer-proxy'],'自建')
        own=self.client.get('/vpn/api/state').json()['nodes'][0]
        own.update(password='',enabled=False)
        self.assertEqual(self.client.put('/vpn/api/nodes/'+str(own['id']),json=own).status_code,400)

    def test_login_rate_limit(self):
        for i in range(10):
            self.assertEqual(self.client.post('/vpn/api/login',json={'username':'admin','password':'bad'}).status_code,401)
        self.assertEqual(self.client.post('/vpn/api/login',json={'username':'admin','password':'bad'}).status_code,429)


if __name__=='__main__':
    unittest.main()
