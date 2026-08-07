"""AstrBot Sub2API 账号首字节延迟守卫插件。"""

from __future__ import annotations

import asyncio
import json
import os
import time
import uuid
from statistics import median
from typing import Any

try:
    from .client import PriorityConflict, Sub2APIClient, Sub2APIError
    from .policy import LatencyEvaluation, evaluate_account_latency
    from .state import StateStore
except ImportError:  # AstrBot 某些版本按 main.py 独立模块加载插件
    from client import PriorityConflict, Sub2APIClient, Sub2APIError  # type: ignore[no-redef]
    from policy import LatencyEvaluation, evaluate_account_latency  # type: ignore[no-redef]
    from state import StateStore  # type: ignore[no-redef]

try:  # AstrBot 运行时提供；本地静态检查不要求安装 AstrBot。
    from astrbot.api import AstrBotConfig, logger
    from astrbot.api.event import AstrMessageEvent, filter
    from astrbot.api.star import Context, Star, StarTools, register
except ImportError:  # pragma: no cover - 仅用于脱离 AstrBot 的单元测试
    AstrBotConfig = dict  # type: ignore[assignment,misc]
    Context = Any  # type: ignore[misc,assignment]

    class Star:  # type: ignore[no-redef]
        def __init__(self, context: Any = None):
            self.context = context

    class StarTools:  # type: ignore[no-redef]
        @staticmethod
        def get_data_dir():
            from pathlib import Path

            return Path(os.getenv("ASTRBOT_DATA_DIR", ".astrbot-data"))

    class _Logger:
        def info(self, *_args: Any, **_kwargs: Any) -> None: pass
        def warning(self, *_args: Any, **_kwargs: Any) -> None: pass
        def error(self, *_args: Any, **_kwargs: Any) -> None: pass

    logger = _Logger()

    def register(*_args: Any, **_kwargs: Any):
        return lambda cls: cls

    class _Filter:
        @staticmethod
        def command(*_args: Any, **_kwargs: Any):
            return lambda func: func

    filter = _Filter()
    AstrMessageEvent = Any  # type: ignore[misc,assignment]


def _cfg(config: Any, key: str, default: Any = None) -> Any:
    if isinstance(config, dict):
        return config.get(key, default)
    return getattr(config, key, default)


def _tier(account: dict[str, Any]) -> str:
    extra = account.get("extra") if isinstance(account.get("extra"), dict) else {}
    for key in ("plan_type", "subscription_type", "tier", "plan"):
        value = account.get(key) or extra.get(key)
        if value:
            return str(value).lower()
    return str(account.get("type") or "default").lower()


def _groups(account: dict[str, Any]) -> set[str]:
    values = account.get("group_ids") or []
    if not values and isinstance(account.get("groups"), list):
        values = [item.get("id") for item in account["groups"] if isinstance(item, dict)]
    return {str(value) for value in values if value is not None} or {"_ungrouped"}


def build_priority_proposals(
    accounts: list[dict[str, Any]],
    evaluations: dict[str, LatencyEvaluation],
    *,
    max_penalty: int = 10,
) -> list[dict[str, Any]]:
    """按同级账号生成建议；同级有健康账号时不会跨套餐切换。"""

    active = [a for a in accounts if str(a.get("status", "active")) == "active" and a.get("schedulable", True)]
    proposals: list[dict[str, Any]] = []
    for account in active:
        account_id = str(account.get("id"))
        evaluation = evaluations.get(account_id)
        if not evaluation or evaluation.penalty <= 0 or evaluation.state not in {"slow", "critical", "recovering"}:
            continue
        same_tier = [
            candidate
            for candidate in active
            if str(candidate.get("id")) != account_id
            and _tier(candidate) == _tier(account)
            and _groups(candidate) & _groups(account)
            and evaluations.get(str(candidate.get("id")), LatencyEvaluation(0, 0, 0, 0, 0, 0, 0, 0, "observe")).penalty == 0
        ]
        replacement_ids = [str(candidate.get("id")) for candidate in sorted(same_tier, key=lambda item: int(item.get("priority", 50)))]
        current_priority = int(account.get("priority", 50))
        base_priority = int(account.get("_manual_priority", current_priority))
        new_priority = base_priority + min(max_penalty, max(0, evaluation.penalty))
        proposals.append(
            {
                "nonce": uuid.uuid4().hex,
                "created_at": int(time.time()),
                "expires_at": int(time.time()) + 600,
                "account_id": int(account.get("id")),
                "expected_priority": current_priority,
                "new_priority": new_priority,
                "penalty": evaluation.penalty,
                "state": evaluation.state,
                "tier": _tier(account),
                "replacement_account_ids": replacement_ids,
                "same_tier_available": bool(replacement_ids),
                "clear_sticky_recommended": bool(
                    account.get("sticky")
                    or account.get("sticky_session")
                    or account.get("session_window_status") == "sticky"
                ),
            }
        )
    return proposals


def validate_proposal(proposal: dict[str, Any], *, now: int | None = None) -> bool:
    now = int(time.time()) if now is None else int(now)
    return bool(proposal.get("nonce") and int(proposal.get("expires_at", 0)) >= now)


@register("sub2api_latency_guard", "local", "Sub2API 账号延迟调度守卫", "0.1.0")
class Sub2APILatencyGuard(Star):
    """读取使用记录并通过飞书提出或执行 priority 调整。"""

    def __init__(self, context: Context, config: AstrBotConfig | None = None):
        super().__init__(context)
        self.context = context
        self.config = config or {}
        data_dir = StarTools.get_data_dir()
        data_dir.mkdir(parents=True, exist_ok=True)
        self.store = StateStore(data_dir / "sub2api_latency_guard_state.json")
        self.client = Sub2APIClient(
            str(_cfg(self.config, "sub2api_base_url", "http://127.0.0.1:8080")),
            admin_api_key=str(_cfg(self.config, "sub2api_admin_api_key", "") or ""),
            jwt=str(_cfg(self.config, "sub2api_jwt", "") or ""),
        )
        self._task: asyncio.Task[Any] | None = None
        try:
            self._task = asyncio.create_task(self._monitor_loop())
        except RuntimeError:
            self._task = None

    def _mode(self) -> str:
        value = str(_cfg(self.config, "mode", "confirm") or "confirm").lower()
        return value if value in {"observe", "confirm", "auto"} else "confirm"

    async def terminate(self) -> None:
        if self._task and not self._task.done():
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
        self.store.save()
        await self.client.close()

    async def _monitor_loop(self) -> None:
        interval = max(10, int(_cfg(self.config, "poll_interval_seconds", 60)))
        while True:
            try:
                await self.scan_once()
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                logger.warning(f"Sub2API 延迟守卫轮询失败: {exc}")
            await asyncio.sleep(interval)

    async def scan_once(self) -> list[dict[str, Any]]:
        accounts = await self.client.list_accounts()
        records = await self.client.list_usage(
            start_date=str(_cfg(self.config, "start_date", "") or "") or None,
            last_usage_id=self.store.get("last_usage_id"),
        )
        rolling: dict[str, list[float]] = self.store.data.setdefault("samples", {})
        for record in records:
            account_id = record.get("account_id")
            value = record.get("first_token_ms")
            if account_id is None or value is None:
                continue
            key = str(account_id)
            rolling.setdefault(key, []).append(value)
            rolling[key] = rolling[key][-50:]
        if records:
            # 接口按 created_at desc 返回，保留首条 ID 作为下次增量边界。
            newest_id = records[0].get("id")
            if newest_id is not None:
                self.store.set("last_usage_id", newest_id)
        grouped = {key: list(values) for key, values in rolling.items()}
        evaluations: dict[str, LatencyEvaluation] = {}
        for account in accounts:
            key = str(account.get("id"))
            priority_state = self.store.data.setdefault("priority_state", {}).setdefault(key, {})
            current_priority = int(account.get("priority", 50))
            last_applied = priority_state.get("last_applied_priority")
            if last_applied is not None and int(last_applied) == current_priority:
                manual_priority = int(priority_state.get("manual_priority", current_priority))
            else:
                # 管理员手工改过值，新的值成为基准，旧的插件写入不再叠加。
                manual_priority = current_priority
                priority_state["manual_priority"] = manual_priority
                priority_state.pop("last_applied_priority", None)
            account["_manual_priority"] = manual_priority
            values = grouped.get(key, [])
            peers = [median(v) for aid, v in grouped.items() if aid != key and v]
            baseline = median(peers) if peers else float(_cfg(self.config, "baseline_ms", 2000))
            previous = self.store.get("penalties", {}).get(key, {})
            evaluation = evaluate_account_latency(
                values,
                baseline_ms=baseline,
                previous_penalty=int(previous.get("penalty", 0)),
                normal_streak=int(previous.get("normal_streak", 0)),
                min_samples=int(_cfg(self.config, "min_samples", 10)),
            )
            evaluations[key] = evaluation
            self.store.data.setdefault("penalties", {})[key] = {
                "penalty": evaluation.penalty,
                "normal_streak": (int(previous.get("normal_streak", 0)) + 1 if evaluation.state in {"healthy", "recovering"} else 0),
            }
        proposals = build_priority_proposals(accounts, evaluations, max_penalty=int(_cfg(self.config, "max_priority_penalty", 10)))
        self.store.set("last_scan_at", int(time.time()))
        self.store.set("proposals", proposals)
        self.store.save()
        if self._mode() == "auto":
            for proposal in proposals:
                await self.apply_proposal(proposal.get("nonce", ""))
        elif self._mode() == "confirm" and proposals:
            await self._notify_proposals(proposals, evaluations)
        return proposals

    def _is_admin(self, event: Any) -> bool:
        configured = {str(item) for item in (_cfg(self.config, "admin_ids", []) or [])}
        sender = getattr(event, "get_sender_id", lambda: "")()
        return not configured or str(sender) in configured

    def _card(self, proposals: list[dict[str, Any]], evaluations: dict[str, LatencyEvaluation], explanation: str = "") -> dict[str, Any]:
        lines = ["**Sub2API 首字节延迟调度建议**"]
        if explanation:
            lines.append(f"AI 说明（仅供参考）：{explanation}")
        for proposal in proposals[:10]:
            evaluation = evaluations.get(str(proposal["account_id"]))
            metrics = ""
            if evaluation:
                metrics = f"p50={evaluation.p50:.0f}ms，p95={evaluation.p95:.0f}ms，慢请求={evaluation.slow_ratio:.0%}"
            replacement = ", ".join(proposal["replacement_account_ids"]) or "同级暂无健康账号"
            lines.append(
                f"账号 `{proposal['account_id']}`（{proposal['tier']}）{proposal['state']}：{metrics}\n"
                f"priority {proposal['expected_priority']} -> {proposal['new_priority']}；同级替代：{replacement}\n"
                f"确认码：`{proposal['nonce']}`"
            )
        return {
            "schema": "2.0",
            "body": {
                "elements": [
                    {"tag": "markdown", "content": "\n\n".join(lines)},
                    {"tag": "button", "text": {"tag": "plain_text", "content": "确认全部调整"}, "type": "primary", "behaviors": [{"type": "callback", "value": {"action": "confirm_all", "nonces": [p["nonce"] for p in proposals]}}]},
                ]
            },
        }

    async def _ai_explain(self, proposals: list[dict[str, Any]], evaluations: dict[str, LatencyEvaluation]) -> str:
        """AI 只解释已计算的指标，不接收也不生成写入指令。"""
        if not bool(_cfg(self.config, "ai_enabled", False)) or not hasattr(self.context, "llm_generate"):
            return ""
        provider_id = str(_cfg(self.config, "ai_provider_id", "") or "").strip()
        if not provider_id and hasattr(self.context, "get_current_chat_provider_id"):
            try:
                provider_id = await self.context.get_current_chat_provider_id()
            except Exception:
                provider_id = ""
        if not provider_id:
            return ""
        summary = []
        for proposal in proposals[:20]:
            evaluation = evaluations.get(str(proposal["account_id"]))
            if evaluation:
                summary.append({"tier": proposal["tier"], "state": evaluation.state, "p50_ms": round(evaluation.p50), "p95_ms": round(evaluation.p95), "slow_ratio": round(evaluation.slow_ratio, 3), "volatility": round(evaluation.volatility, 3), "penalty": evaluation.penalty})
        try:
            response = await self.context.llm_generate(
                chat_provider_id=provider_id,
                system_prompt="你是延迟监控解释器，只能解释给定聚合指标，不能决定账号、priority 或执行命令。用简短中文说明风险。",
                prompt=json.dumps(summary, ensure_ascii=False),
                temperature=0,
            )
            text = str(getattr(response, "completion_text", response) or "").strip()
            return text[:1000]
        except Exception as exc:
            logger.warning(f"Sub2API 延迟守卫 AI 说明失败: {exc}")
            return ""

    async def _notify_proposals(self, proposals: list[dict[str, Any]], evaluations: dict[str, LatencyEvaluation]) -> None:
        sent = set(self.store.get("notified_nonces", []))
        fresh = [p for p in proposals if p.get("nonce") not in sent]
        if not fresh:
            return
        settings = {
            "platform": str(_cfg(self.config, "notify_platform", "") or ""),
            "target_id": str(_cfg(self.config, "notify_target_id", "") or ""),
        }
        if not settings["platform"] or not settings["target_id"]:
            logger.warning("Sub2API 延迟守卫发现调整建议，但未配置飞书通知目标")
            return
        try:
            from lark_oapi.api.cardkit.v1 import CreateCardRequest, CreateCardRequestBody
            from lark_oapi.api.im.v1 import CreateMessageRequest, CreateMessageRequestBody

            manager = getattr(self.context, "platform_manager", None)
            platform = next((item for item in getattr(manager, "platform_insts", []) if item.meta().name == settings["platform"]), None) if manager else None
            client = getattr(platform, "lark_api", None) if platform else None
            if not client or not client.cardkit or not client.im:
                return
            explanation = await self._ai_explain(fresh, evaluations)
            card = await client.cardkit.v1.card.acreate(CreateCardRequest.builder().request_body(CreateCardRequestBody.builder().type("card_json").data(json.dumps(self._card(fresh, evaluations, explanation), ensure_ascii=False)).build()).build())
            if not card.success() or not card.data:
                return
            content = json.dumps({"type": "card", "data": {"card_id": card.data.card_id}}, ensure_ascii=False)
            await client.im.v1.message.acreate(CreateMessageRequest.builder().receive_id_type("chat_id").request_body(CreateMessageRequestBody.builder().receive_id(settings["target_id"]).content(content).msg_type("interactive").build()).build())
            sent.update(p["nonce"] for p in fresh)
            self.store.set("notified_nonces", list(sent)[-100:])
            self.store.save()
        except Exception as exc:
            logger.warning(f"Sub2API 延迟守卫飞书通知失败: {exc}")

    @filter.command("latency_guard")
    async def latency_guard_command(self, event: AstrMessageEvent, action: str = ""):
        """提供手工扫描和确认入口，飞书按钮也会转为同一命令。"""
        if not self._is_admin(event):
            yield event.plain_result("无权执行延迟调度操作。")
            return
        parts = (action or "scan").strip().split()
        if parts[0].startswith("button:confirm_all:"):
            parts = ["confirm_all", parts[0].split(":", 2)[2]]
        if parts[0] == "confirm_all":
            nonces = parts[1].split(",") if len(parts) > 1 else [p.get("nonce", "") for p in self.store.get("proposals", [])]
            applied = sum(1 for nonce in nonces if await self.apply_proposal(nonce))
            yield event.plain_result(f"已执行 {applied} 条 priority 调整；过期或手工变更的提案已跳过。")
            return
        if parts[0] in {"scan", "检查"}:
            proposals = await self.scan_once()
            yield event.plain_result(f"延迟扫描完成，发现 {len(proposals)} 条调整建议。")
            return
        if parts[0] in {"confirm", "确认"} and len(parts) == 2:
            ok = await self.apply_proposal(parts[1])
            yield event.plain_result("已执行 priority 调整。" if ok else "提案已过期、无效或账号已被手工修改。")
            return
        yield event.plain_result("用法：/latency_guard scan | confirm <确认码>")

    async def apply_proposal(self, nonce: str) -> bool:
        proposal = next((item for item in self.store.get("proposals", []) if item.get("nonce") == nonce), None)
        if not proposal or not validate_proposal(proposal):
            return False
        if self._mode() == "observe":
            return False
        try:
            await self.client.update_account_priority(
                proposal["account_id"], proposal["expected_priority"], proposal["new_priority"]
            )
        except (PriorityConflict, Sub2APIError) as exc:
            logger.warning(f"Sub2API 延迟守卫未执行 priority 调整: {exc}")
            return False
        proposal["applied_at"] = int(time.time())
        priority_state = self.store.data.setdefault("priority_state", {}).setdefault(str(proposal["account_id"]), {})
        priority_state["last_applied_priority"] = int(proposal["new_priority"])
        priority_state.setdefault("manual_priority", int(proposal["expected_priority"]))
        self.store.save()
        return True
