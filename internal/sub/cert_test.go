package sub

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/config"
)

// writeSelfSignedCert 生成一份自签名证书，返回证书路径与期望的 pinSHA256。
func writeSelfSignedCert(t *testing.T, dir string, cn string, dns []string) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "server.crt")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(der)
	return path, hex.EncodeToString(sum[:])
}

func TestInspectCertSelfSigned(t *testing.T) {
	dir := t.TempDir()
	certPath, wantPin := writeSelfSignedCert(t, dir, "test.local", []string{"test.local"})

	info, err := InspectCert(certPath)
	if err != nil {
		t.Fatalf("解析证书失败: %v", err)
	}
	if !info.SelfSigned {
		t.Error("自签名证书应被识别为 SelfSigned")
	}
	if info.Pin != wantPin {
		t.Errorf("pin 期望 %s，实际 %s", wantPin, info.Pin)
	}
	if len(info.DNSNames) != 1 || info.DNSNames[0] != "test.local" {
		t.Errorf("DNS SAN 解析错误: %v", info.DNSNames)
	}
}

func TestInspectCertMissingFile(t *testing.T) {
	if _, err := InspectCert("/nonexistent/cert.pem"); err == nil {
		t.Error("不存在的证书应返回错误")
	}
}

func TestClientTLSUsesACMEWithoutInsecure(t *testing.T) {
	sc := &config.ServerConfig{
		ACME: &config.ACMEConfig{Domains: []string{"*.example.com"}},
	}
	sni, insecure, pin := clientTLS(sc)
	if sni != "example.com" {
		t.Errorf("SNI 期望 example.com，实际 %q", sni)
	}
	if insecure || pin != "" {
		t.Error("ACME 模式不应跳过校验或输出指纹")
	}
}

func TestClientTLSSelfSignedAddsInsecureAndPin(t *testing.T) {
	dir := t.TempDir()
	certPath, wantPin := writeSelfSignedCert(t, dir, "srv.local", []string{"srv.local"})

	sc := &config.ServerConfig{TLS: &config.TLSConfig{Cert: certPath, Key: "k"}}
	sni, insecure, pin := clientTLS(sc)

	if sni != "srv.local" {
		t.Errorf("SNI 应取自证书 DNS SAN，实际 %q", sni)
	}
	if !insecure {
		t.Error("自签名证书必须要求客户端跳过校验")
	}
	if pin != wantPin {
		t.Errorf("pin 期望 %s，实际 %s", wantPin, pin)
	}
}

// 核心回归：自签名证书的 URI 必须自带 insecure 与 pinSHA256，否则客户端连不上
func TestBuildURIIncludesTLSHints(t *testing.T) {
	dir := t.TempDir()
	certPath, wantPin := writeSelfSignedCert(t, dir, "srv.local", []string{"srv.local"})

	sc := &config.ServerConfig{
		Listen: ":443",
		Auth:   &config.AuthConfig{Type: "userpass"},
		TLS:    &config.TLSConfig{Cert: certPath, Key: "k"},
		Obfs: &config.ObfsConfig{
			Type:       "salamander",
			Salamander: &config.SalamanderConfig{Password: "12345678"},
		},
	}

	uri, err := BuildURI(sc, "user1", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(uri, "insecure=1") {
		t.Errorf("URI 应包含 insecure=1: %s", uri)
	}
	if !strings.Contains(uri, "pinSHA256="+wantPin) {
		t.Errorf("URI 应包含正确的 pinSHA256: %s", uri)
	}
	if !strings.Contains(uri, "sni=srv.local") {
		t.Errorf("URI 应包含证书中的 SNI: %s", uri)
	}
	if !strings.Contains(uri, "obfs=salamander") || !strings.Contains(uri, "obfs-password=12345678") {
		t.Errorf("URI 应包含混淆参数: %s", uri)
	}
}

func TestBuildClashDataMarksInsecureForSelfSigned(t *testing.T) {
	dir := t.TempDir()
	certPath, _ := writeSelfSignedCert(t, dir, "srv.local", []string{"srv.local"})

	sc := &config.ServerConfig{
		Listen: ":443",
		TLS:    &config.TLSConfig{Cert: certPath, Key: "k"},
	}
	data, err := BuildClashData(sc, "alice", "pw", "1.2.3.4", 7890)
	if err != nil {
		t.Fatal(err)
	}
	if !data.Insecure {
		t.Error("自签名证书时 Clash 数据应标记 Insecure")
	}
	if data.SNI != "srv.local" {
		t.Errorf("SNI 期望 srv.local，实际 %q", data.SNI)
	}
}

func TestBuildClashDataAcmeNotInsecure(t *testing.T) {
	sc := &config.ServerConfig{
		Listen: ":443",
		ACME:   &config.ACMEConfig{Domains: []string{"example.com"}},
	}
	data, err := BuildClashData(sc, "alice", "pw", "1.2.3.4", 7890)
	if err != nil {
		t.Fatal(err)
	}
	if data.Insecure {
		t.Error("ACME 证书不应标记 Insecure")
	}
	if data.SNI != "example.com" {
		t.Errorf("SNI 期望 example.com，实际 %q", data.SNI)
	}
}

func TestTrimWildcard(t *testing.T) {
	cases := map[string]string{
		"*.example.com": "example.com",
		"example.com":   "example.com",
		"*.a.b.c":       "a.b.c",
	}
	for in, want := range cases {
		if got := trimWildcard(in); got != want {
			t.Errorf("trimWildcard(%q) = %q，期望 %q", in, got, want)
		}
	}
}