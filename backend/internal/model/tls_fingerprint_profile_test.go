package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTLSFingerprintProfileValidate(t *testing.T) {
	tests := []struct {
		name       string
		profile    TLSFingerprintProfile
		wantErr    bool
		wantField  string
		wantInMsg  string
		wantNoDiff string
	}{
		{
			name:      "name required",
			profile:   TLSFingerprintProfile{},
			wantErr:   true,
			wantField: "name",
		},
		{
			name:    "empty ALPN uses built-in default",
			profile: TLSFingerprintProfile{Name: "node"},
		},
		{
			name:    "explicit http/1.1 accepted",
			profile: TLSFingerprintProfile{Name: "node", ALPNProtocols: []string{"http/1.1"}},
		},
		{
			// The transport is hard-wired to HTTP/1.1. Advertising h2 lets the server
			// pick a protocol the client cannot speak, and every request on accounts
			// bound to this profile dies with an opaque upstream reset.
			name:      "h2 rejected",
			profile:   TLSFingerprintProfile{Name: "node", ALPNProtocols: []string{"h2"}},
			wantErr:   true,
			wantField: "alpn_protocols",
			wantInMsg: "h2",
		},
		{
			name:      "h2 alongside http/1.1 rejected",
			profile:   TLSFingerprintProfile{Name: "node", ALPNProtocols: []string{"h2", "http/1.1"}},
			wantErr:   true,
			wantField: "alpn_protocols",
			wantInMsg: "h2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.profile.Validate()
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			require.Equal(t, tt.wantField, verr.Field)
			if tt.wantInMsg != "" {
				require.Contains(t, verr.Message, tt.wantInMsg)
			}
		})
	}
}
