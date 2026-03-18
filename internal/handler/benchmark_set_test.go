package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	s := testStoreAndDB(t)
	h := &BenchmarkSetHandler{Store: s}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/benchmark-sets", h.HandlePublicList)
	mux.HandleFunc("GET /api/v1/benchmark-sets/{set_id}", h.HandlePublicGet)
	mux.HandleFunc("POST /admin/v1/benchmark-sets", h.HandleAdminCreate)
	mux.HandleFunc("PUT /admin/v1/benchmark-sets/{set_id}", h.HandleAdminUpdate)
	mux.HandleFunc("GET /admin/v1/benchmark-sets", h.HandleAdminList)
	mux.HandleFunc("GET /admin/v1/benchmark-sets/{set_id}", h.HandleAdminGet)
	return mux
}

func parseResponse(t *testing.T, rec *httptest.ResponseRecorder) (bool, json.RawMessage, string) {
	t.Helper()
	var resp struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.OK, resp.Data, resp.Error
}

func TestPublicListAndGet(t *testing.T) {
	mux := setupTestMux(t)

	// Create data via admin first
	body := `{"set_id":"bs_pub","description":"Public Test","answer_check_method":"exact","status":"active","question_maxlen":500}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/benchmark-sets", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d, body=%s", rec.Code, rec.Body.String())
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantOK     bool
		check      func(t *testing.T, data json.RawMessage)
	}{
		{
			name:       "list active",
			method:     http.MethodGet,
			path:       "/api/v1/benchmark-sets?status=active",
			wantStatus: http.StatusOK,
			wantOK:     true,
			check: func(t *testing.T, data json.RawMessage) {
				var list []PublicBenchmarkSetView
				json.Unmarshal(data, &list)
				if len(list) != 1 {
					t.Errorf("got %d items, want 1", len(list))
				}
				if len(list) > 0 && list[0].QuestionMaxLen != 500 {
					t.Errorf("QuestionMaxLen = %d, want 500", list[0].QuestionMaxLen)
				}
			},
		},
		{
			name:       "get existing",
			method:     http.MethodGet,
			path:       "/api/v1/benchmark-sets/bs_pub",
			wantStatus: http.StatusOK,
			wantOK:     true,
			check: func(t *testing.T, data json.RawMessage) {
				var v PublicBenchmarkSetView
				json.Unmarshal(data, &v)
				if v.SetID != "bs_pub" {
					t.Errorf("SetID = %q, want bs_pub", v.SetID)
				}
			},
		},
		{
			name:       "get not found",
			method:     http.MethodGet,
			path:       "/api/v1/benchmark-sets/bs_none",
			wantStatus: http.StatusNotFound,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			ok, data, _ := parseResponse(t, rec)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.check != nil {
				tt.check(t, data)
			}
		})
	}
}

func TestAdminCRUD(t *testing.T) {
	mux := setupTestMux(t)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantOK     bool
		check      func(t *testing.T, data json.RawMessage)
	}{
		{
			name:       "create",
			method:     http.MethodPost,
			path:       "/admin/v1/benchmark-sets",
			body:       `{"set_id":"bs_admin","description":"Admin Test","answer_check_method":"exact"}`,
			wantStatus: http.StatusCreated,
			wantOK:     true,
			check: func(t *testing.T, data json.RawMessage) {
				var v AdminBenchmarkSetView
				json.Unmarshal(data, &v)
				if v.SetID != "bs_admin" {
					t.Errorf("SetID = %q, want bs_admin", v.SetID)
				}
				if v.AnswerCheckMethod != "exact" {
					t.Errorf("AnswerCheckMethod = %q, want exact", v.AnswerCheckMethod)
				}
				if v.CreatedAt == "" {
					t.Error("CreatedAt should not be empty")
				}
			},
		},
		{
			name:       "create duplicate",
			method:     http.MethodPost,
			path:       "/admin/v1/benchmark-sets",
			body:       `{"set_id":"bs_admin","description":"Dup"}`,
			wantStatus: http.StatusConflict,
			wantOK:     false,
		},
		{
			name:       "update",
			method:     http.MethodPut,
			path:       "/admin/v1/benchmark-sets/bs_admin",
			body:       `{"description":"Updated"}`,
			wantStatus: http.StatusOK,
			wantOK:     true,
			check: func(t *testing.T, data json.RawMessage) {
				var v AdminBenchmarkSetView
				json.Unmarshal(data, &v)
				if v.Description != "Updated" {
					t.Errorf("Description = %q, want Updated", v.Description)
				}
			},
		},
		{
			name:       "update not found",
			method:     http.MethodPut,
			path:       "/admin/v1/benchmark-sets/bs_nope",
			body:       `{"description":"x"}`,
			wantStatus: http.StatusNotFound,
			wantOK:     false,
		},
		{
			name:       "admin get",
			method:     http.MethodGet,
			path:       "/admin/v1/benchmark-sets/bs_admin",
			wantStatus: http.StatusOK,
			wantOK:     true,
			check: func(t *testing.T, data json.RawMessage) {
				var v AdminBenchmarkSetView
				json.Unmarshal(data, &v)
				if v.AnswerCheckMethod != "exact" {
					t.Errorf("AnswerCheckMethod = %q, want exact", v.AnswerCheckMethod)
				}
			},
		},
		{
			name:       "admin list all",
			method:     http.MethodGet,
			path:       "/admin/v1/benchmark-sets",
			wantStatus: http.StatusOK,
			wantOK:     true,
			check: func(t *testing.T, data json.RawMessage) {
				var list []AdminBenchmarkSetView
				json.Unmarshal(data, &list)
				if len(list) != 1 {
					t.Errorf("got %d items, want 1", len(list))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader *bytes.Buffer
			if tt.body != "" {
				bodyReader = bytes.NewBufferString(tt.body)
			} else {
				bodyReader = &bytes.Buffer{}
			}
			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			ok, data, _ := parseResponse(t, rec)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.check != nil {
				tt.check(t, data)
			}
		})
	}
}

func TestPublicViewExcludesInternalFields(t *testing.T) {
	mux := setupTestMux(t)

	body := `{"set_id":"bs_internal","description":"Test","answer_check_method":"exact"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/benchmark-sets", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/benchmark-sets/bs_internal", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	_, data, _ := parseResponse(t, rec)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	if _, exists := raw["answer_check_method"]; exists {
		t.Error("public view should not contain answer_check_method")
	}
	if _, exists := raw["created_at"]; exists {
		t.Error("public view should not contain created_at")
	}
}
