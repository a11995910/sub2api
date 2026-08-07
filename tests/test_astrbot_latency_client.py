import asyncio
import json
from pathlib import Path

from astrbot.sub2api_latency_guard.client import PriorityConflict, Sub2APIClient
from astrbot.sub2api_latency_guard.state import StateStore


class _Response:
    def __init__(self, payload, status=200):
        self.status = status
        self.payload = payload

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None

    async def json(self):
        return self.payload


class _Session:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def request(self, method, url, **kwargs):
        self.calls.append((method, url, kwargs))
        return self.responses.pop(0)


def test_usage_pagination_and_last_id():
    session = _Session([
        _Response({"data": {"items": [{"id": 3}, {"id": 2}], "pages": 2}}),
        _Response({"data": {"items": [{"id": 1}], "pages": 2}}),
    ])
    client = Sub2APIClient("http://sub2api", session=session)
    result = asyncio.run(client.list_usage(last_usage_id=1, page_size=2))
    assert [item["id"] for item in result] == [3, 2]
    assert session.calls[0][2]["params"]["page_size"] == 2
    assert session.calls[0][2]["params"]["sort_order"] == "desc"


def test_priority_update_refuses_manual_change():
    session = _Session([_Response({"data": {"id": 4, "priority": 9}})])
    client = Sub2APIClient("http://sub2api", session=session)
    try:
        asyncio.run(client.update_account_priority(4, expected_current=5, new_priority=10))
    except PriorityConflict:
        pass
    else:
        raise AssertionError("应拒绝覆盖管理员 priority")


def test_state_store_replaces_file_atomically(tmp_path: Path):
    path = tmp_path / "state.json"
    store = StateStore(path)
    store.set("proposals", [{"nonce": "abc"}])
    store.save()
    assert json.loads(path.read_text(encoding="utf-8"))["proposals"][0]["nonce"] == "abc"
