package service

import "context"

import "testing"

func TestAPIKeyService_AuthSnapshotPreservesOpenAIFastModeEnabled(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := &APIKey{
		ID:                    11,
		UserID:                22,
		GroupID:               nil,
		Name:                  "fast-key",
		Status:                StatusActive,
		OpenAIFastModeEnabled: true,
		User:                  &User{ID: 22, Status: StatusActive, Role: RoleUser, Balance: 10, Concurrency: 1},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	if snapshot == nil {
		t.Fatalf("expected snapshot")
	}
	if !snapshot.OpenAIFastModeEnabled {
		t.Fatalf("expected snapshot to preserve openai_fast_mode_enabled")
	}

	roundTripped := svc.snapshotToAPIKey("sk-fast", snapshot)
	if roundTripped == nil {
		t.Fatalf("expected api key from snapshot")
	}
	if !roundTripped.OpenAIFastModeEnabled {
		t.Fatalf("expected api key to preserve openai_fast_mode_enabled after snapshot round trip")
	}
}

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelsListConfig(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after models_list_config was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV12AuthSnapshotWithoutCacheHitQuarterFlag(t *testing.T) {
	groupID := int64(31)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-cache-hit-quarter", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  12,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "邀请奖励分组",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   0.06,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v12 auth snapshot to be rejected after cache_hit_quarter_to_input_enabled was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV15AuthSnapshotWithoutReasoningEffortPolicy(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-reasoning-mappings", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 15},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v15 auth snapshot to be rejected after reasoning effort policy was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}
