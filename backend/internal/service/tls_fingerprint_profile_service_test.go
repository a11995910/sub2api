package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

func newTLSProfileServiceWithProfiles(profiles ...*model.TLSFingerprintProfile) *TLSFingerprintProfileService {
	svc := &TLSFingerprintProfileService{localCache: make(map[int64]*model.TLSFingerprintProfile, len(profiles))}
	for _, p := range profiles {
		svc.localCache[p.ID] = p
	}
	return svc
}

func tlsFingerprintAccount(id int64, profileID int64) *Account {
	return &Account{
		ID:       id,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": profileID,
		},
	}
}

// "随机" must mean "spread accounts across profiles", not "reroll on every request":
// the profile takes part in upstream connection-pool reuse, so a per-request reroll
// would rebuild the transport and redo the TLS handshake for every single request.
func TestResolveTLSProfileRandomIsStablePerAccount(t *testing.T) {
	svc := newTLSProfileServiceWithProfiles(
		&model.TLSFingerprintProfile{ID: 1, Name: "alpha"},
		&model.TLSFingerprintProfile{ID: 2, Name: "beta"},
		&model.TLSFingerprintProfile{ID: 3, Name: "gamma"},
	)
	account := tlsFingerprintAccount(7, -1)

	first := svc.ResolveTLSProfile(account)
	require.NotNil(t, first)

	for i := 0; i < 20; i++ {
		got := svc.ResolveTLSProfile(account)
		require.NotNil(t, got)
		require.Equal(t, first.Name, got.Name, "same account must keep the same fingerprint")
		require.Equal(t, first.CacheKey(), got.CacheKey())
	}
}

func TestResolveTLSProfileRandomSpreadsAcrossAccounts(t *testing.T) {
	svc := newTLSProfileServiceWithProfiles(
		&model.TLSFingerprintProfile{ID: 1, Name: "alpha"},
		&model.TLSFingerprintProfile{ID: 2, Name: "beta"},
		&model.TLSFingerprintProfile{ID: 3, Name: "gamma"},
	)

	seen := make(map[string]int)
	for accountID := int64(1); accountID <= 9; accountID++ {
		profile := svc.ResolveTLSProfile(tlsFingerprintAccount(accountID, -1))
		require.NotNil(t, profile)
		seen[profile.Name]++
	}
	require.Len(t, seen, 3, "accounts should be spread over every available profile")
}

func TestResolveTLSProfileRandomFallsBackToBuiltinWhenNoProfiles(t *testing.T) {
	svc := newTLSProfileServiceWithProfiles()

	profile := svc.ResolveTLSProfile(tlsFingerprintAccount(1, -1))
	require.NotNil(t, profile)
	require.Contains(t, profile.Name, "Built-in Default")
}

func TestResolveTLSProfileDisabledAccountReturnsNil(t *testing.T) {
	svc := newTLSProfileServiceWithProfiles(&model.TLSFingerprintProfile{ID: 1, Name: "alpha"})

	account := tlsFingerprintAccount(1, 1)
	account.Extra["enable_tls_fingerprint"] = false

	require.Nil(t, svc.ResolveTLSProfile(account))
	require.Nil(t, svc.ResolveTLSProfile(nil))
}

func TestResolveTLSProfileBoundProfileWins(t *testing.T) {
	svc := newTLSProfileServiceWithProfiles(
		&model.TLSFingerprintProfile{ID: 1, Name: "alpha"},
		&model.TLSFingerprintProfile{ID: 2, Name: "beta"},
	)

	profile := svc.ResolveTLSProfile(tlsFingerprintAccount(99, 2))
	require.NotNil(t, profile)
	require.Equal(t, "beta", profile.Name)
}
