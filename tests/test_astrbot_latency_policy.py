from astrbot.sub2api_latency_guard.policy import evaluate_account_latency


def test_insufficient_samples_do_not_penalize():
    result = evaluate_account_latency([1000, 5000, 9000], baseline_ms=1000)
    assert result.penalty == 0
    assert result.state == "observe"


def test_sustained_slow_samples_penalize():
    samples = [4200] * 20 + [1800] * 10
    result = evaluate_account_latency(samples, baseline_ms=2000)
    assert result.penalty >= 5
    assert result.state in {"slow", "critical"}
    assert result.slow_ratio > 0.5


def test_single_spike_is_not_a_slow_account():
    samples = [2000] * 29 + [60000]
    result = evaluate_account_latency(samples, baseline_ms=2000)
    assert result.penalty <= 2
    assert result.state in {"healthy", "degraded"}


def test_recovery_requires_three_normal_cycles():
    result = evaluate_account_latency(
        [1800] * 30,
        baseline_ms=2000,
        previous_penalty=8,
        normal_streak=2,
    )
    assert result.penalty == 8
    assert result.state == "recovering"

    recovered = evaluate_account_latency(
        [1800] * 30,
        baseline_ms=2000,
        previous_penalty=8,
        normal_streak=3,
    )
    assert recovered.penalty == 0
    assert recovered.state == "healthy"


def test_high_tail_latency_adds_volatility_penalty():
    samples = [2000] * 28 + [5000, 12000]
    result = evaluate_account_latency(samples, baseline_ms=2000)
    assert result.p50 == 2000
    assert result.p95 >= 5000
    assert result.volatility >= 2.5
    assert result.penalty == 2
