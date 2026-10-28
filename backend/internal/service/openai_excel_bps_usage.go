package service

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 按账号策略归一化下游缓存创建用量。总输入已包含缓存创建，保留总量和缓存读取量，清除各缓存写入别名及时间分项。
func excelBPSDownstreamUsage(payload []byte) ([]byte, error) {
	for _, path := range []string{"usage", "response.usage"} {
		usage := gjson.GetBytes(payload, path)
		if !usage.IsObject() {
			continue
		}
		normalized := []byte(usage.Raw)
		changed := false
		for _, field := range []string{
			"input_tokens_details.cache_write_tokens", "prompt_tokens_details.cache_write_tokens",
			"input_tokens_details.cache_creation_tokens", "prompt_tokens_details.cache_creation_tokens",
			"cache_write_tokens", "cache_creation_input_tokens", "cache_write_input_tokens", "cache_creation_tokens",
			"cache_creation.ephemeral_5m_input_tokens", "cache_creation.ephemeral_1h_input_tokens",
		} {
			if value := usage.Get(field); !value.Exists() || value.Raw == "0" {
				continue
			}
			var err error
			normalized, err = sjson.SetBytes(normalized, field, 0)
			if err != nil {
				return nil, err
			}
			changed = true
		}
		if changed {
			var err error
			payload, err = sjson.SetRawBytes(payload, path, normalized)
			if err != nil {
				return nil, err
			}
		}
	}
	return payload, nil
}
