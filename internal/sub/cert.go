package sub

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/config"
)

// CertInfo 描述服务端证书的关键信息。
//
// 导出给 Panel 的「服务器 → 证书」展示使用，同时用于推导客户端连接参数。
type CertInfo struct {
	// SelfSigned 表示证书为自签名（签发者与主体相同），客户端需要跳过校验。
	SelfSigned bool `json:"selfSigned"`
	// Subject 证书主体。
	Subject string `json:"subject"`
	// Issuer 签发者。
	Issuer string `json:"issuer"`
	// DNSNames 是证书中的 DNS SAN，可作为客户端的 SNI。
	DNSNames []string `json:"dnsNames"`
	// NotBefore / NotAfter 有效期。
	NotBefore string `json:"notBefore"`
	NotAfter  string `json:"notAfter"`
	// DaysLeft 距过期剩余天数（负数表示已过期）。
	DaysLeft int `json:"daysLeft"`
	// Pin 是证书 DER 的 SHA-256（小写十六进制），
	// 与官方 `hysteria cert` 输出的 pinSHA256 完全一致。
	Pin string `json:"pin"`
}

// InspectCert 读取并解析 PEM 证书文件。
func InspectCert(path string) (*CertInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取证书失败: %w", err)
	}

	var block *pem.Block
	rest := data
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			break
		}
	}
	if block == nil {
		return nil, fmt.Errorf("证书文件中没有 CERTIFICATE 块: %s", path)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析证书失败: %w", err)
	}

	sum := sha256.Sum256(cert.Raw)
	daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)

	return &CertInfo{
		SelfSigned: cert.Issuer.String() == cert.Subject.String(),
		Subject:    cert.Subject.String(),
		Issuer:     cert.Issuer.String(),
		DNSNames:   cert.DNSNames,
		NotBefore:  cert.NotBefore.Format(time.RFC3339),
		NotAfter:   cert.NotAfter.Format(time.RFC3339),
		DaysLeft:   daysLeft,
		Pin:        hex.EncodeToString(sum[:]),
	}, nil
}

// clientTLS 推导客户端所需的 TLS 参数：SNI、是否需要跳过校验、证书指纹。
//
// 规则：
//   - ACME：证书由公信 CA 签发，只需 SNI，不跳过校验；
//   - 静态证书：读取证书文件，
//     有 DNS SAN 就用它做 SNI（否则客户端无法通过域名校验）；
//     自签名则额外给出 insecure + pinSHA256
//     （官方推荐组合，既兼容又具备抗中间人能力）。
func clientTLS(sc *config.ServerConfig) (sni string, insecure bool, pin string) {
	// ACME 模式：使用第一个域名（去掉通配符）作为 SNI
	if sc.ACME != nil && len(sc.ACME.Domains) > 0 {
		return trimWildcard(sc.ACME.Domains[0]), false, ""
	}

	// 静态证书模式
	if sc.TLS != nil && sc.TLS.Cert != "" {
		info, err := InspectCert(sc.TLS.Cert)
		if err != nil {
			// 读不到证书时保守处理：按自签名对待，保证客户端能用
			return "", true, ""
		}
		if len(info.DNSNames) > 0 {
			sni = trimWildcard(info.DNSNames[0])
		}
		if info.SelfSigned {
			insecure = true
			pin = info.Pin
		}
	}
	return sni, insecure, pin
}

// trimWildcard 去掉域名的通配符与前导点：*.example.com → example.com
func trimWildcard(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "*")
	domain = strings.TrimPrefix(domain, ".")
	return domain
}