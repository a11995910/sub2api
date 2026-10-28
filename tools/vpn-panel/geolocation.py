"""按公网 IP 查询地理名称；只发送 IP，不发送节点认证信息。"""
import ipaddress
import json
from http.client import HTTPException
from urllib.parse import quote
from urllib.request import urlopen


def lookup_location(host):
    try:
        address = ipaddress.ip_address(host)
        if not address.is_global:
            return ''
        url = 'https://ipwho.is/' + quote(str(address), safe='') + '?lang=zh-CN'
        with urlopen(url, timeout=4) as response:
            data = json.loads(response.read(65536))
        if not isinstance(data, dict) or data.get('success') is not True:
            return ''
        if ipaddress.ip_address(data.get('ip', '')) != address:
            return ''
        parts = []
        for key in ('country', 'region', 'city'):
            value = data.get(key)
            if isinstance(value, str):
                value = ' '.join(value.split())
                if value and value not in parts:
                    parts.append(value)
        return '·'.join(parts)[:100]
    except (OSError, ValueError, TypeError, HTTPException):
        # 服务不可达、格式异常或非公网 IP 均不阻止节点保存。
        return ''
