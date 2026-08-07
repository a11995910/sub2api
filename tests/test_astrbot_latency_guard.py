from astrbot.sub2api_latency_guard.main import build_priority_proposals, validate_proposal
from astrbot.sub2api_latency_guard.policy import LatencyEvaluation


def _evaluation(penalty, state):
    return LatencyEvaluation(30, 2000, 8000, 3000, 0.8, 4, 1, penalty, state)


def test_slow_plus_prefers_healthy_plus():
    accounts = [
        {"id": 1, "priority": 10, "type": "plus", "group_ids": [7], "status": "active"},
        {"id": 2, "priority": 20, "type": "plus", "group_ids": [7], "status": "active"},
        {"id": 3, "priority": 5, "type": "pro", "group_ids": [7], "status": "active"},
    ]
    proposals = build_priority_proposals(accounts, {"1": _evaluation(5, "slow"), "2": _evaluation(0, "healthy"), "3": _evaluation(0, "healthy")})
    assert proposals[0]["same_tier_available"] is True
    assert proposals[0]["replacement_account_ids"] == ["2"]


def test_expired_proposal_is_rejected():
    assert not validate_proposal({"nonce": "x", "expires_at": 10}, now=11)
