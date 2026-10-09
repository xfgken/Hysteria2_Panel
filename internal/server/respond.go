package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// apiError 是统一的错误响应体。
type apiError struct {
	Error string `json:"error"`
}

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 响应头已写出，此时只能放弃。
		return
	}
}

// writeError 输出统一格式的错误。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// timeoutContext 在请求上下文之上叠加超时。
//
// 更新 / 下载这类长耗时操作需要比默认更长的时间预算。
func timeoutContext(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}