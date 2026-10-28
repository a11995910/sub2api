"""验证地理查询不泄露非公网地址，且失败不影响节点保存。"""
import io
import json
import unittest
from unittest.mock import patch
from http.client import IncompleteRead

from geolocation import lookup_location


class LocationTests(unittest.TestCase):
    def response(self, **overrides):
        data = dict(success=True, ip='48.47.37.254', country='美国', region='新泽西州', city='泽西')
        data.update(overrides)
        return io.BytesIO(json.dumps(data).encode())

    def test_chinese_location_and_public_ipv6(self):
        with patch('geolocation.urlopen', return_value=self.response()) as request:
            self.assertEqual(lookup_location('48.47.37.254'), '美国·新泽西州·泽西')
            request.assert_called_once_with('https://ipwho.is/48.47.37.254?lang=zh-CN', timeout=4)
        with patch('geolocation.urlopen', return_value=self.response(ip='2606:4700::1111', city='新泽西州')):
            self.assertEqual(lookup_location('2606:4700::1111'), '美国·新泽西州')

    def test_private_reserved_and_domains_not_sent(self):
        with patch('geolocation.urlopen') as request:
            for host in ('127.0.0.1', '10.0.0.1', '169.254.169.254', '192.0.2.10', '::1', 'fc00::1', 'example.com', 'bad/ip'):
                self.assertEqual(lookup_location(host), '')
            request.assert_not_called()

    def test_provider_failure_or_wrong_ip(self):
        for error in (TimeoutError(), OSError(), IncompleteRead(b'')):
            with patch('geolocation.urlopen', side_effect=error):
                self.assertEqual(lookup_location('48.47.37.254'), '')
        for response in (self.response(success=False), self.response(ip='1.1.1.1'), io.BytesIO(b'not-json'), io.BytesIO(b'[]')):
            with patch('geolocation.urlopen', return_value=response):
                self.assertEqual(lookup_location('48.47.37.254'), '')
