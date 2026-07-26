// Package model 定义服务层使用的数据模型。
package model

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// TLSFingerprintProfile TLS 指纹配置模板
// 包含完整的 ClientHello 参数，用于模拟特定客户端的 TLS 握手特征
type TLSFingerprintProfile struct {
	ID                  int64     `json:"id"`
	Name                string    `json:"name"`
	Description         *string   `json:"description"`
	EnableGREASE        bool      `json:"enable_grease"`
	CipherSuites        []uint16  `json:"cipher_suites"`
	Curves              []uint16  `json:"curves"`
	PointFormats        []uint16  `json:"point_formats"`
	SignatureAlgorithms []uint16  `json:"signature_algorithms"`
	ALPNProtocols       []string  `json:"alpn_protocols"`
	SupportedVersions   []uint16  `json:"supported_versions"`
	KeyShareGroups      []uint16  `json:"key_share_groups"`
	PSKModes            []uint16  `json:"psk_modes"`
	Extensions          []uint16  `json:"extensions"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Validate 验证模板配置的有效性
func (p *TLSFingerprintProfile) Validate() error {
	if p.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	// 指纹 transport 恒为 HTTP/1.1（详见 tlsfingerprint.ALPNHTTP11）。若这里放行 "h2"，
	// 服务端会在 ALPN 中选中 h2，而客户端仍然发 HTTP/1.1 请求行，结果是该账号的每一个
	// 请求都被上游以协议错误重置——且错误信息完全不指向这份模板。宁可在保存时就拒绝。
	if bad := tlsfingerprint.UnsupportedALPNProtocols(p.ALPNProtocols); len(bad) > 0 {
		return &ValidationError{
			Field:   "alpn_protocols",
			Message: "unsupported ALPN protocol(s) " + strings.Join(bad, ", ") + `; only "` + tlsfingerprint.ALPNHTTP11 + `" is supported (leave empty to use the built-in default)`,
		}
	}
	return nil
}

// ToTLSProfile 将领域模型转换为运行时使用的 tlsfingerprint.Profile
// 空切片字段会在 dialer 中 fallback 到内置默认值
func (p *TLSFingerprintProfile) ToTLSProfile() *tlsfingerprint.Profile {
	return &tlsfingerprint.Profile{
		Name:                p.Name,
		EnableGREASE:        p.EnableGREASE,
		CipherSuites:        p.CipherSuites,
		Curves:              p.Curves,
		PointFormats:        p.PointFormats,
		SignatureAlgorithms: p.SignatureAlgorithms,
		ALPNProtocols:       p.ALPNProtocols,
		SupportedVersions:   p.SupportedVersions,
		KeyShareGroups:      p.KeyShareGroups,
		PSKModes:            p.PSKModes,
		Extensions:          p.Extensions,
	}
}
