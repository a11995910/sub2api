"""验证鉴权、凭据保护、节点修改与订阅格式。"""
import base64
import os
import tempfile
import unittest
from unittest.mock import patch

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
        self.assertEqual(result['proxies'][0]['name'],'住宅改名 · 192.0.2.10')
        body=self.node(location='美国·测试地址'); self.client.put('/vpn/api/nodes/'+str(n['id']),json={**body, 'name':'住宅改名', 'password':''})
        result=yaml.safe_load(self.client.get('/vpn/sub/test-token').text)
        self.assertEqual(result['proxies'][0]['name'],'住宅改名 · 192.0.2.10 · 美国·测试地址')
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

    def test_short_url_compatibility_qr_and_rotation(self):
        self.login()
        self.client.post('/vpn/api/nodes',json=self.node())
        short=self.client.get('/vpn/api/state').json()['subscription_url']
        self.assertRegex(short,r'^https://testserver/s/[A-Za-z0-9_-]{16}$')
        self.assertEqual(self.client.get(short).text,self.client.get('/vpn/sub/test-token').text)
        with patch('app.qrcode.make',wraps=app.qrcode.make) as make:
            self.assertEqual(self.client.get('/vpn/api/qr').status_code,200)
            self.assertEqual(make.call_args.args[0],short)
        self.assertEqual(self.client.get('/s/invalid').status_code,404)
        self.assertEqual(self.client.get('/s/错误').status_code,404)
        self.client.post('/vpn/api/rotate')
        self.assertEqual(self.client.get(short).status_code,404)
        self.assertEqual(self.client.get('/vpn/sub/test-token').status_code,404)
        fresh=self.client.get('/vpn/api/state').json()['subscription_url']
        self.assertNotEqual(fresh,short)
        self.assertEqual(self.client.get(fresh).status_code,200)

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

    def test_anytls_subscription_and_shadowrocket_guard(self):
        self.login()
        with app.db() as c:
            c.execute("INSERT INTO nodes(name,category,protocol,host,port,password,managed) VALUES ('自建 AnyTLS','自建','anytls','198.51.100.10',24444,'anytls-secret',1)")
        result=yaml.safe_load(self.client.get('/vpn/sub/test-token').text)
        self.assertEqual(result['proxies'][0]['type'],'anytls')
        self.assertEqual(result['proxies'][0]['name'],'自建 AnyTLS · 198.51.100.10')
        self.assertEqual(result['proxies'][0]['sni'],'198.51.100.10')
        self.assertEqual(self.client.get('/vpn/sub/test-token?format=shadowrocket').status_code,400)

    def test_relay_subscription_and_dependency(self):
        self.login()
        with app.db() as c:
            c.execute("INSERT INTO nodes(name,category,protocol,host,port,password,cipher,managed) VALUES ('自建','自建','ss','192.0.2.1',24443,'own-secret','aes-256-gcm',1)")
        body=self.node(relay=True)
        self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,200)
        result=yaml.safe_load(self.client.get('/vpn/sub/test-token').text)
        self.assertEqual(result['proxies'][1]['dialer-proxy'],'自建 · 192.0.2.1')
        own=self.client.get('/vpn/api/state').json()['nodes'][0]
        own.update(password='',enabled=False)
        self.assertEqual(self.client.put('/vpn/api/nodes/'+str(own['id']),json=own).status_code,400)

    def test_anytls_certificate_fingerprint(self):
        self.login()
        body=self.node(fingerprint='AA:'*31+'AA');body['protocol']='anytls'
        self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,200)
        saved=self.client.get('/vpn/api/state').json()['nodes'][0]
        self.assertEqual(saved['fingerprint'],'aa'*32)
        result=yaml.safe_load(self.client.get('/vpn/sub/test-token').text)['proxies'][0]
        self.assertEqual(result['fingerprint'],'aa'*32)
        self.assertFalse(result['skip-cert-verify'])
        saved.update(password='',note='编辑后保留证书指纹')
        self.assertEqual(self.client.put('/vpn/api/nodes/'+str(saved['id']),json=saved).status_code,200)
        self.assertEqual(yaml.safe_load(self.client.get('/vpn/sub/test-token').text)['proxies'][0]['fingerprint'],'aa'*32)
        saved.pop('fingerprint')
        self.assertEqual(self.client.put('/vpn/api/nodes/'+str(saved['id']),json=saved).status_code,200)
        self.assertEqual(yaml.safe_load(self.client.get('/vpn/sub/test-token').text)['proxies'][0]['fingerprint'],'aa'*32)
        for invalid in ('aa','zz'*32):
            body['fingerprint']=invalid
            self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,400)
        body.update(protocol='http',fingerprint='aa'*32)
        self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,400)

    def test_login_rate_limit(self):
        for i in range(10):
            self.assertEqual(self.client.post('/vpn/api/login',json={'username':'admin','password':'bad'}).status_code,401)
        self.assertEqual(self.client.post('/vpn/api/login',json={'username':'admin','password':'bad'}).status_code,429)

    def test_location_auto_fill_and_host_change(self):
        self.login()
        body=self.node();body['host']='48.47.37.254'
        with patch('app.lookup_location',return_value='美国·新泽西州·泽西') as lookup:
            self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,200)
            saved=self.client.get('/vpn/api/state').json()['nodes'][0]
            self.assertEqual(saved['location'],'美国·新泽西州·泽西')
            self.assertEqual(saved['location_auto'],1)
            result=yaml.safe_load(self.client.get('/vpn/sub/test-token').text)
            self.assertIn('美国·新泽西州·泽西',result['proxies'][0]['name'])
            saved.update(password='',note='只修改备注')
            self.assertEqual(self.client.put('/vpn/api/nodes/'+str(saved['id']),json=saved).status_code,200)
            lookup.assert_called_once_with('48.47.37.254')
        with patch('app.lookup_location',return_value='美国·纽约州·纽约') as lookup:
            saved['host']='48.46.54.149'
            self.assertEqual(self.client.put('/vpn/api/nodes/'+str(saved['id']),json=saved).status_code,200)
            lookup.assert_called_once_with('48.46.54.149')
            self.assertEqual(self.client.get('/vpn/api/state').json()['nodes'][0]['location'],'美国·纽约州·纽约')

    def test_location_manual_priority_and_retry(self):
        self.login()
        body=self.node(location='我的地址')
        with patch('app.lookup_location') as lookup:
            self.assertEqual(self.client.post('/vpn/api/nodes',json=body).status_code,200)
            saved=self.client.get('/vpn/api/state').json()['nodes'][0]
            saved.update(host='48.47.37.254',password='')
            saved.pop('location')
            self.assertEqual(self.client.put('/vpn/api/nodes/'+str(saved['id']),json=saved).status_code,200)
            self.assertEqual(self.client.get('/vpn/api/state').json()['nodes'][0]['location'],'我的地址')
            lookup.assert_not_called()
        saved['location']=''
        with patch('app.lookup_location',return_value=''):
            result=self.client.put('/vpn/api/nodes/'+str(saved['id']),json=saved)
            self.assertEqual(result.status_code,200)
            self.assertIn('warning',result.json())
        with patch('app.lookup_location',return_value='美国·新泽西州·泽西'):
            result=self.client.put('/vpn/api/nodes/'+str(saved['id']),json=saved)
            self.assertNotIn('warning',result.json())
            self.assertEqual(self.client.get('/vpn/api/state').json()['nodes'][0]['location'],'美国·新泽西州·泽西')


if __name__=='__main__':
    unittest.main()
