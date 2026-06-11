package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpstreamRateMonitorFetchSnapshotUsesUserVisibleGroupsAndRateOverrides(t *testing.T) {
	const token = "upstream-test-token"
	requestedAdminGroups := false

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("login method = %s, want POST", r.Method)
		}
		writeUpstreamTestJSON(t, w, map[string]any{
			"code":    0,
			"message": "success",
			"data": map[string]any{
				"access_token": token,
			},
		})
	})
	mux.HandleFunc("/api/v1/groups/available", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("available groups method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("available groups authorization = %q", got)
		}
		writeUpstreamTestJSON(t, w, map[string]any{
			"code":    0,
			"message": "success",
			"data": []map[string]any{
				{
					"id":                                   4,
					"name":                                 "GPT分组",
					"platform":                             "openai",
					"rate_multiplier":                      0.08,
					"image_rate_multiplier":                1,
					"image_rate_independent":               false,
					"subscription_type":                    "standard",
					"is_exclusive":                         false,
					"status":                               "active",
					"rpm_limit":                            0,
					"allow_image_generation":               false,
					"image_price_1k":                       0.134,
					"allow_messages_dispatch":              true,
					"fallback_group_id":                    nil,
					"fallback_group_id_on_invalid_request": nil,
				},
				{
					"id":                7,
					"name":              "Claude专属",
					"platform":          "anthropic",
					"rate_multiplier":   0.5,
					"subscription_type": "standard",
					"is_exclusive":      true,
					"status":            "active",
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/groups/rates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("group rates method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("group rates authorization = %q", got)
		}
		writeUpstreamTestJSON(t, w, map[string]any{
			"code":    0,
			"message": "success",
			"data": map[string]float64{
				"4": 0.06,
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		requestedAdminGroups = true
		http.Error(w, "admin endpoint should not be requested", http.StatusForbidden)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	svc := NewUpstreamRateMonitorService(nil, nil)
	svc.client = srv.Client()
	snapshot, err := svc.fetchSnapshot(context.Background(), &UpstreamRateMonitor{
		BaseURL:  srv.URL,
		Username: "user@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatalf("fetchSnapshot returned error: %v", err)
	}
	if requestedAdminGroups {
		t.Fatal("fetchSnapshot should not request /api/v1/admin/groups")
	}
	if len(snapshot) != 2 {
		t.Fatalf("snapshot length = %d, want 2", len(snapshot))
	}
	if snapshot[0].ID != 4 || snapshot[0].RateMultiplier != 0.06 {
		t.Fatalf("first group = %+v, want id=4 effective rate=0.06", snapshot[0])
	}
	if snapshot[1].ID != 7 || snapshot[1].RateMultiplier != 0.5 {
		t.Fatalf("second group = %+v, want id=7 default rate=0.5", snapshot[1])
	}
}

func TestApplyUpstreamGroupRateOverridesRejectsInvalidRate(t *testing.T) {
	snapshot := UpstreamRateSnapshot{{ID: 1, Name: "默认分组", RateMultiplier: 1}}
	if err := applyUpstreamGroupRateOverrides(snapshot, map[int64]float64{1: 0}); err == nil {
		t.Fatal("expected invalid rate error")
	}
}

func writeUpstreamTestJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
