//go:build unit

package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResolveCangyuanVideoCapabilities(t *testing.T) {
	tests := []struct {
		model                                        string
		maxDuration, maxImages, maxVideos, maxAudios int
		fixedResolution, audioField                  string
		omitResolution                               bool
	}{
		{"sd4-seedance-2.0-mini", 15, 4, 3, 1, "", "audio", false},
		{"sd4-seedance-2.0-fast", 15, 4, 3, 1, "", "generate_audio", false},
		{"sd7-seedance-2.0-720p", 15, 5, 3, 3, "720p", "generate_audio", true},
		{"seedance-2.0", 15, 5, 3, 3, "720p", "generate_audio", true},
		{"sd7-seedance-2.0-1080p", 15, 5, 3, 3, "1080p", "generate_audio", true},
		{"sd8-seedance-2.0", 15, 9, 3, 3, "", "", true},
		{"sd4-seedance-2.5-480p", 30, 30, 10, 10, "480p", "generate_audio", true},
		{"seedance-2.5-720p", 29, 30, 10, 10, "720p", "generate_audio", true},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got, ok := resolveCangyuanVideoCapabilities(tt.model)
			require.True(t, ok)
			require.Equal(t, tt.maxDuration, got.MaxDuration)
			require.Equal(t, tt.maxImages, got.MaxImages)
			require.Equal(t, tt.maxVideos, got.MaxVideos)
			require.Equal(t, tt.maxAudios, got.MaxAudios)
			require.Equal(t, tt.fixedResolution, got.FixedResolution)
			require.Equal(t, tt.audioField, got.AudioField)
			require.Equal(t, tt.omitResolution, got.OmitResolution)
		})
	}

	_, ok := resolveCangyuanVideoCapabilities("future-motion-pro")
	require.False(t, ok)
}

func TestPrepareUnifiedOpenAIVideoCreateBodyUsesMiniAudioAndMediaFields(t *testing.T) {
	payload, req, err := ParseOpenAIVideoCreateBody([]byte(`{
		"model":"public-mini","prompt":"x","duration":8,"resolution":"720p",
		"generate_audio":false,
		"reference_image_urls":["https://cdn.test/a.png"],
		"reference_videos":["https://cdn.test/a.mp4"],
		"reference_audios":["https://cdn.test/a.mp3"]
	}`))
	require.NoError(t, err)

	prepared, err := PrepareUnifiedOpenAIVideoCreateBody(payload, req, "sd4-seedance-2.0-mini")

	require.NoError(t, err)
	require.Equal(t, "720p", prepared.Request.Resolution)
	require.False(t, gjson.GetBytes(prepared.Body, "generate_audio").Exists())
	require.True(t, gjson.GetBytes(prepared.Body, "audio").Exists())
	require.False(t, gjson.GetBytes(prepared.Body, "audio").Bool())
	require.Equal(t, "https://cdn.test/a.png", gjson.GetBytes(prepared.Body, "reference_image_urls.0").String())
	require.Equal(t, "https://cdn.test/a.mp4", gjson.GetBytes(prepared.Body, "reference_videos.0").String())
	require.Equal(t, "https://cdn.test/a.mp3", gjson.GetBytes(prepared.Body, "reference_audios.0").String())
}

func TestPrepareUnifiedOpenAIVideoCreateBodyOmitsFixedResolution(t *testing.T) {
	payload, req, err := ParseOpenAIVideoCreateBody([]byte(`{
		"model":"public-25","prompt":"x","duration":30,"resolution":"480p"
	}`))
	require.NoError(t, err)

	prepared, err := PrepareUnifiedOpenAIVideoCreateBody(payload, req, "sd4-seedance-2.5-480p")

	require.NoError(t, err)
	require.Equal(t, "480p", prepared.Request.Resolution)
	require.False(t, gjson.GetBytes(prepared.Body, "resolution").Exists())
	require.Equal(t, int64(30), gjson.GetBytes(prepared.Body, "duration").Int())
}

func TestPrepareUnifiedOpenAIVideoCreateBodyValidatesModelRules(t *testing.T) {
	tests := []struct {
		name, model, body, message string
	}{
		{"2.5 720p rejects 30 seconds", "seedance-2.5-720p", `{"model":"x","prompt":"x","duration":30}`, "duration must be between 4 and 29 seconds"},
		{"2.0 rejects 16 seconds", "sd4-seedance-2.0", `{"model":"x","prompt":"x","duration":16}`, "duration must be between 4 and 15 seconds"},
		{"sd8 only accepts listed durations", "sd8-seedance-2.0", `{"model":"x","prompt":"x","duration":8}`, "duration must be one of 5, 10, 15 seconds"},
		{"frame pair is required", "sd4-seedance-2.0", `{"model":"x","prompt":"x","first_image_url":"https://cdn.test/first.png"}`, "first_image_url and last_image_url must be provided together"},
		{"frames conflict with references", "sd4-seedance-2.0", `{"model":"x","prompt":"x","first_image_url":"https://cdn.test/first.png","last_image_url":"https://cdn.test/last.png","reference_image_urls":["https://cdn.test/a.png"]}`, "frame inputs cannot be combined with reference media"},
		{"fixed resolution rejects mismatch", "sd4-seedance-2.5-480p", `{"model":"x","prompt":"x","resolution":"720p"}`, "resolution 720p is not supported by sd4-seedance-2.5-480p"},
		{"sd8 rejects audio switch", "sd8-seedance-2.0", `{"model":"x","prompt":"x","generate_audio":true}`, "generate_audio is not supported by sd8-seedance-2.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, req, err := ParseOpenAIVideoCreateBody([]byte(tt.body))
			require.NoError(t, err)
			_, err = PrepareUnifiedOpenAIVideoCreateBody(payload, req, tt.model)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestPrepareUnifiedOpenAIVideoCreateBodyRejectsReferenceLimit(t *testing.T) {
	images := ""
	for i := 0; i < 5; i++ {
		if i > 0 {
			images += ","
		}
		images += fmt.Sprintf(`"https://cdn.test/%d.png"`, i)
	}
	body := []byte(fmt.Sprintf(`{"model":"x","prompt":"x","reference_image_urls":[%s]}`, images))
	payload, req, err := ParseOpenAIVideoCreateBody(body)
	require.NoError(t, err)

	_, err = PrepareUnifiedOpenAIVideoCreateBody(payload, req, "sd4-seedance-2.0-fast")
	require.ErrorContains(t, err, "reference_image_urls must not contain more than 4 items")
}
