package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeImageResponseFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "旧数据空值回退 Base64", input: "", want: ImageResponseFormatB64JSON},
		{name: "保留 Base64", input: "b64_json", want: ImageResponseFormatB64JSON},
		{name: "保留 URL", input: "url", want: ImageResponseFormatURL},
		{name: "忽略首尾空白和大小写", input: " URL ", want: ImageResponseFormatURL},
		{name: "拒绝未知格式", input: "base64", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeImageResponseFormat(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
