"""Sub2API 管理接口的最小异步客户端。"""

from __future__ import annotations

import asyncio
from typing import Any, Iterable

import aiohttp


class Sub2APIError(RuntimeError):
    """管理接口返回非成功状态。"""


class PriorityConflict(Sub2APIError):
    """写入前发现管理员已手工修改 priority。"""


class Sub2APIClient:
    def __init__(
        self,
        base_url: str,
        *,
        admin_api_key: str = "",
        jwt: str = "",
        timeout_seconds: float = 15.0,
        session: Any = None,
    ):
        self.base_url = base_url.rstrip("/")
        self.admin_api_key = admin_api_key.strip()
        self.jwt = jwt.strip()
        self.timeout_seconds = timeout_seconds
        self._session = session
        self._owned_session = session is None

    def _headers(self) -> dict[str, str]:
        headers = {"Accept": "application/json", "User-Agent": "sub2api-latency-guard"}
        if self.admin_api_key:
            headers["X-API-Key"] = self.admin_api_key
        elif self.jwt:
            headers["Authorization"] = f"Bearer {self.jwt}"
        return headers

    async def close(self) -> None:
        if self._owned_session and self._session is not None:
            await self._session.close()
            self._session = None

    async def __aenter__(self) -> "Sub2APIClient":
        return self

    async def __aexit__(self, *_exc: Any) -> None:
        await self.close()

    async def _request(self, method: str, path: str, **kwargs: Any) -> Any:
        if self._session is None:
            self._session = aiohttp.ClientSession(
                timeout=aiohttp.ClientTimeout(total=self.timeout_seconds),
                headers=self._headers(),
            )
        kwargs.setdefault("headers", self._headers())
        url = f"{self.base_url}{path}"
        try:
            async with self._session.request(method, url, **kwargs) as response:
                if response.status < 200 or response.status >= 300:
                    raise Sub2APIError(f"Sub2API 管理接口 HTTP {response.status}")
                return await response.json()
        except asyncio.TimeoutError as exc:
            raise Sub2APIError("Sub2API 管理接口请求超时") from exc
        except aiohttp.ClientError as exc:
            raise Sub2APIError("Sub2API 管理接口网络请求失败") from exc

    @staticmethod
    def _data(payload: Any) -> Any:
        if isinstance(payload, dict) and "data" in payload:
            return payload["data"]
        return payload

    async def list_accounts(self) -> list[dict[str, Any]]:
        result: list[dict[str, Any]] = []
        page = 1
        while True:
            data = self._data(await self._request("GET", "/api/v1/admin/accounts", params={"page": page, "page_size": 1000}))
            page_items = data.get("items", data.get("accounts", [])) if isinstance(data, dict) else data
            page_items = [item for item in (page_items or []) if isinstance(item, dict)]
            result.extend(page_items)
            pages = data.get("pages", page) if isinstance(data, dict) else page
            if not page_items or page >= int(pages or page) or len(page_items) < 1000:
                break
            page += 1
        return result

    async def get_account(self, account_id: int | str) -> dict[str, Any]:
        payload = self._data(await self._request("GET", f"/api/v1/admin/accounts/{account_id}"))
        return payload if isinstance(payload, dict) else {}

    async def list_usage(
        self,
        account_id: int | str | None = None,
        *,
        start_date: str | None = None,
        end_date: str | None = None,
        last_usage_id: int | str | None = None,
        page_size: int = 1000,
    ) -> list[dict[str, Any]]:
        items: list[dict[str, Any]] = []
        page = 1
        while True:
            params: dict[str, Any] = {
                "page": page,
                "page_size": min(1000, max(1, page_size)),
                "sort_by": "created_at",
                "sort_order": "desc",
            }
            if account_id is not None:
                params["account_id"] = account_id
            if start_date:
                params["start_date"] = start_date
            if end_date:
                params["end_date"] = end_date
            data = self._data(await self._request("GET", "/api/v1/admin/usage", params=params))
            page_items = data.get("items", []) if isinstance(data, dict) else data
            page_items = [item for item in (page_items or []) if isinstance(item, dict)]
            stop = False
            for item in page_items:
                if last_usage_id is not None and str(item.get("id")) == str(last_usage_id):
                    stop = True
                    break
                items.append(item)
            if stop or not page_items:
                break
            pages = data.get("pages", page) if isinstance(data, dict) else page
            if page >= int(pages or page) or len(page_items) < params["page_size"]:
                break
            page += 1
        return items

    async def update_account_priority(
        self, account_id: int | str, expected_current: int, new_priority: int
    ) -> dict[str, Any]:
        current = await self.get_account(account_id)
        actual = int(current.get("priority", expected_current))
        if actual != int(expected_current):
            raise PriorityConflict(f"账号 {account_id} 的 priority 已由 {expected_current} 变为 {actual}")
        payload = self._data(
            await self._request(
                "PUT",
                f"/api/v1/admin/accounts/{account_id}",
                json={"priority": int(new_priority)},
            )
        )
        return payload if isinstance(payload, dict) else {}
