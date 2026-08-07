"""根据真实使用记录评估账号首字节延迟健康度。

本模块只做确定性的统计和惩罚计算，不发起网络请求，也不直接修改账号。
"""

from __future__ import annotations

from dataclasses import dataclass
from math import ceil
from typing import Iterable


@dataclass(frozen=True)
class LatencyEvaluation:
    """一个账号在一个模型/同级窗口内的延迟评价。"""

    sample_count: int
    p50: float
    p95: float
    ewma: float
    slow_ratio: float
    volatility: float
    confidence: float
    penalty: int
    state: str


def _percentile(values: list[float], fraction: float) -> float:
    """nearest-rank 百分位，避免插值制造不存在的延迟值。"""

    index = max(0, min(len(values) - 1, ceil(len(values) * fraction) - 1))
    return values[index]


def _ewma(values: Iterable[float], alpha: float = 0.3) -> float:
    current = 0.0
    for value in values:
        current = value if current == 0 else alpha * value + (1 - alpha) * current
    return current


def evaluate_account_latency(
    samples: Iterable[object],
    *,
    baseline_ms: float,
    previous_penalty: int = 0,
    normal_streak: int = 0,
    min_samples: int = 10,
    confidence_samples: int = 30,
    slow_multiplier: float = 2.0,
    normal_multiplier: float = 1.5,
    max_latency_ms: float = 60_000,
) -> LatencyEvaluation:
    """计算延迟惩罚；惩罚值越高，建议 priority 数字越大。

    样本不足只观察；惩罚仅取 0、2、5、10。恢复时要求连续三个正常周期，
    由调用方把本次正常周期数传入 ``normal_streak``。
    """

    try:
        baseline = max(1.0, float(baseline_ms))
    except (TypeError, ValueError):
        baseline = 1.0
    cleaned: list[float] = []
    for raw in samples:
        try:
            value = float(raw)  # type: ignore[arg-type]
        except (TypeError, ValueError):
            continue
        if value > 0:
            cleaned.append(min(value, max_latency_ms))

    count = len(cleaned)
    confidence = min(1.0, count / max(1, confidence_samples))
    if count == 0:
        return LatencyEvaluation(0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0, "observe")

    ordered = sorted(cleaned)
    p50 = _percentile(ordered, 0.50)
    p95 = _percentile(ordered, 0.95)
    ewma = _ewma(cleaned)
    slow_ratio = sum(value > baseline * slow_multiplier for value in cleaned) / count
    volatility = p95 / p50 if p50 > 0 else 0.0

    if count < min_samples:
        return LatencyEvaluation(
            count, p50, p95, ewma, slow_ratio, volatility, confidence, 0, "observe"
        )

    normal = p95 <= baseline * normal_multiplier and slow_ratio <= 0.2
    if previous_penalty > 0 and normal:
        if normal_streak >= 3:
            return LatencyEvaluation(
                count, p50, p95, ewma, slow_ratio, volatility, confidence, 0, "healthy"
            )
        return LatencyEvaluation(
            count,
            p50,
            p95,
            ewma,
            slow_ratio,
            volatility,
            confidence,
            previous_penalty,
            "recovering",
        )

    # 持续慢需要同时满足尾延迟和比例条件，孤立尖峰不会触发重惩罚。
    if p95 > baseline * 3.0 and slow_ratio >= 0.5:
        raw_penalty = 10
        state = "critical"
    elif (p95 > baseline * slow_multiplier and slow_ratio >= 0.5) or (
        ewma > baseline * 2.0 and slow_ratio >= 0.3
    ):
        raw_penalty = 5
        state = "slow"
    elif volatility >= 2.5 and slow_ratio <= 0.35:
        raw_penalty = 2
        state = "degraded"
    else:
        raw_penalty = 0
        state = "healthy"

    penalty = int(round(raw_penalty * confidence))
    penalty = min(10, max(0, penalty))
    if penalty == 0 and raw_penalty:
        penalty = 2 if raw_penalty == 2 and confidence >= 0.5 else 0
    return LatencyEvaluation(
        count, p50, p95, ewma, slow_ratio, volatility, confidence, penalty, state
    )
