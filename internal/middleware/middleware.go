// middleware.go - Middleware HTTP (auth, rate limit, logging).
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ContextKey es un tipo para las claves de contexto.
type ContextKey string

const (
	KeyTenantID = ContextKey("tenant_id")
)

// TenantAuther valida el header X-Tenant-Key contra el store.
type TenantAuther struct {
	validKeys map[string]string // key -> tenantID
	mu        sync.RWMutex
	logger    *slog.Logger
}

// NewTenantAuther crea un autenticador de tenants.
func NewTenantAuther(logger *slog.Logger) *TenantAuther {
	return &TenantAuther{
		validKeys: make(map[string]string),
		logger:    logger,
	}
}

// RegisterKey registra una API key para un tenant.
func (a *TenantAuther) RegisterKey(apiKey, tenantID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.validKeys[apiKey] = tenantID
}

// Middleware retorna la función middleware.
func (a *TenantAuther) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Permitir health y webhook sin auth
		if r.URL.Path == "/health" || r.URL.Path == "/webhook" {
			next.ServeHTTP(w, r)
			return
		}

		apiKey := r.Header.Get("X-Tenant-Key")
		if apiKey == "" {
			apiKey = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if apiKey == "" {
			http.Error(w, `{"success":false,"message":"missing tenant key"}`, http.StatusUnauthorized)
			return
		}

		a.mu.RLock()
		tenantID, ok := a.validKeys[apiKey]
		a.mu.RUnlock()

		if !ok {
			http.Error(w, `{"success":false,"message":"invalid tenant key"}`, http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), KeyTenantID, tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware registra todas las solicitudes.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(rw, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration", time.Since(start),
				"ip", r.RemoteAddr,
			)
		})
	}
}

// statusWriter captura el status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
