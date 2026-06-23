package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannel_IsWebSearchEmulationEnabled_Enabled(t *testing.T) {
	c := &Channel{
		FeaturesConfig: map[string]any{
			featureKeyWebSearchEmulation: map[string]any{"anthropic": true},
		},
	}
	require.True(t, c.IsWebSearchEmulationEnabled("anthropic"))
}

func TestChannel_IsWebSearchEmulationEnabled_DifferentPlatform(t *testing.T) {
	c := &Channel{
		FeaturesConfig: map[string]any{
			featureKeyWebSearchEmulation: map[string]any{"anthropic": true},
		},
	}
	require.False(t, c.IsWebSearchEmulationEnabled("openai"))
}

func TestChannel_IsWebSearchEmulationEnabled_Disabled(t *testing.T) {
	c := &Channel{
		FeaturesConfig: map[string]any{
			featureKeyWebSearchEmulation: map[string]any{"anthropic": false},
		},
	}
	require.False(t, c.IsWebSearchEmulationEnabled("anthropic"))
}

func TestChannel_IsWebSearchEmulationEnabled_NilFeaturesConfig(t *testing.T) {
	c := &Channel{FeaturesConfig: nil}
	require.False(t, c.IsWebSearchEmulationEnabled("anthropic"))
}

func TestChannel_IsWebSearchEmulationEnabled_NilChannel(t *testing.T) {
	var c *Channel
	require.False(t, c.IsWebSearchEmulationEnabled("anthropic"))
}

func TestChannel_IsWebSearchEmulationEnabled_WrongStructure(t *testing.T) {
	c := &Channel{
		FeaturesConfig: map[string]any{
			featureKeyWebSearchEmulation: true, // not a map
		},
	}
	require.False(t, c.IsWebSearchEmulationEnabled("anthropic"))
}

func TestChannel_IsWebSearchEmulationEnabled_PlatformValueNotBool(t *testing.T) {
	c := &Channel{
		FeaturesConfig: map[string]any{
			featureKeyWebSearchEmulation: map[string]any{"anthropic": "yes"},
		},
	}
	require.False(t, c.IsWebSearchEmulationEnabled("anthropic"))
}

func TestChannel_ShouldForwardOpenAIImagesViaChatCompletions(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{
			name: "mode chat completions",
			cfg: map[string]any{
				featureKeyOpenAIImagesUpstream: map[string]any{"mode": "chat_completions"},
			},
			want: true,
		},
		{
			name: "boolean shorthand",
			cfg: map[string]any{
				featureKeyOpenAIImagesUpstream: true,
			},
			want: true,
		},
		{
			name: "native mode",
			cfg: map[string]any{
				featureKeyOpenAIImagesUpstream: map[string]any{"mode": "native"},
			},
			want: false,
		},
		{
			name: "wrong structure",
			cfg: map[string]any{
				featureKeyOpenAIImagesUpstream: "chat_completions",
			},
			want: false,
		},
		{
			name: "empty",
			cfg:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Channel{FeaturesConfig: tt.cfg}
			require.Equal(t, tt.want, c.ShouldForwardOpenAIImagesViaChatCompletions())
		})
	}
}
