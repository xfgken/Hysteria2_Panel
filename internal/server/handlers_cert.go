package server

import (
	"net/http"

	"github.com/hy2-panel/hy2-panel/internal/sub"
)

// certificateView 是「服务器 → 证书」的展示数据。
type certificateView struct {
	// Mode: none | tls | acme
	Mode        string        `json:"mode"`
	Path        string        `json:"path,omitempty"`
	Domains     []string      `json:"domains,omitempty"`
	Info        *sub.CertInfo `json:"info,omitempty"`
	Message     string        `json:"message,omitempty"`
	Error       string        `json:"error,omitempty"`
}

// handleCertificateInfo 返回当前官方配置实际使用的证书情况。
//
// 面板只服务一个账号，证书是这条链路上最容易出问题的一环
// （例如自签名证书若没告诉客户端跳过校验，就会直接连不上），
// 因此这里把证书的真实状态直接展示出来。
func (s *Server) handleCertificateInfo(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.mgr.Current()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败: "+err.Error())
		return
	}
	sc := &cfg.Server

	switch {
	case sc.ACME != nil:
		writeJSON(w, http.StatusOK, certificateView{
			Mode:    "acme",
			Domains: sc.ACME.Domains,
			Message: "由 ACME 自动申请并续期（公信 CA 签发，客户端无需跳过校验）",
		})

	case sc.TLS != nil && sc.TLS.Cert != "":
		v := certificateView{Mode: "tls", Path: sc.TLS.Cert}
		if info, ierr := sub.InspectCert(sc.TLS.Cert); ierr != nil {
			v.Error = ierr.Error()
		} else {
			v.Info = info
		}
		writeJSON(w, http.StatusOK, v)

	default:
		writeJSON(w, http.StatusOK, certificateView{
			Mode:    "none",
			Message: "未配置证书。官方 Core 需要 tls 或 acme 之一才能启动。",
		})
	}
}