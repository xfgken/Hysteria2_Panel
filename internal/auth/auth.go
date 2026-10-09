// Package auth 提供面板自身的认证原语。
//
// 与 HY2 用户无关：这里处理的是「谁能登录面板」。
package auth

import (
	"crypto/subtle"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieName 是会话 Cookie 名。
	CookieName = "hy2p_session"

	// CSRFHeader 是 CSRF 校验使用的请求头。
	CSRFHeader = "X-CSRF-Token"

	// SessionTTL 是会话有效期。
	SessionTTL = 12 * time.Hour
)

// HashPassword 生成密码哈希。
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword 校验密码。
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// SecureCompare 恒定时间比较，用于 Token / CSRF 校验。
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}