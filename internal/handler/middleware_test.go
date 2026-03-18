package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminAuth(t *testing.T) {
	const token = "test-secret-token"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})
	handler := AdminAuth(token)(inner)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantOK     bool
	}{
		{
			name:       "valid token",
			authHeader: "Bearer test-secret-token",
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "wrong token",
			authHeader: "Bearer wrong-token",
			wantStatus: http.StatusUnauthorized,
			wantOK:     false,
		},
		{
			name:       "missing header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
			wantOK:     false,
		},
		{
			name:       "malformed header",
			authHeader: "Token test-secret-token",
			wantStatus: http.StatusUnauthorized,
			wantOK:     false,
		},
		{
			name:       "bearer lowercase",
			authHeader: "bearer test-secret-token",
			wantStatus: http.StatusUnauthorized,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/v1/benchmark-sets", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
