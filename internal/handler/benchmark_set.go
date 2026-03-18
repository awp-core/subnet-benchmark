package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

// BenchmarkSetHandler handles BenchmarkSet HTTP requests.
type BenchmarkSetHandler struct {
	Store *store.Store
}

// PublicBenchmarkSetView is the public API view of a BenchmarkSet, excluding internal fields.
type PublicBenchmarkSetView struct {
	SetID                string `json:"set_id"`
	Description          string `json:"description"`
	QuestionRequirements string `json:"question_requirements"`
	AnswerRequirements   string `json:"answer_requirements"`
	QuestionMaxLen       int    `json:"question_maxlen"`
	AnswerMaxLen         int    `json:"answer_maxlen"`
	Status               string `json:"status"`
	TotalQuestions       int    `json:"total_questions"`
	QualifiedQuestions   int    `json:"qualified_questions"`
}

func toPublicView(bs model.BenchmarkSet) PublicBenchmarkSetView {
	return PublicBenchmarkSetView{
		SetID:                bs.SetID,
		Description:          bs.Description,
		QuestionRequirements: bs.QuestionRequirements,
		AnswerRequirements:   bs.AnswerRequirements,
		QuestionMaxLen:       bs.QuestionMaxLen,
		AnswerMaxLen:         bs.AnswerMaxLen,
		Status:               bs.Status,
		TotalQuestions:       bs.TotalQuestions,
		QualifiedQuestions:   bs.QualifiedQuestions,
	}
}

// HandlePublicList handles GET /api/v1/benchmark-sets
func (h *BenchmarkSetHandler) HandlePublicList(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = model.BSStatusActive
	}

	sets, err := h.Store.ListBenchmarkSets(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]PublicBenchmarkSetView, len(sets))
	for i, bs := range sets {
		views[i] = toPublicView(bs)
	}
	writeJSON(w, http.StatusOK, views)
}

// HandlePublicGet handles GET /api/v1/benchmark-sets/{set_id}
func (h *BenchmarkSetHandler) HandlePublicGet(w http.ResponseWriter, r *http.Request) {
	setID := r.PathValue("set_id")
	if setID == "" {
		writeError(w, http.StatusBadRequest, "missing set_id")
		return
	}

	bs, err := h.Store.GetBenchmarkSet(r.Context(), setID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if bs == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toPublicView(*bs))
}

// AdminBenchmarkSetView is the admin API view with all fields.
type AdminBenchmarkSetView struct {
	SetID                string `json:"set_id"`
	Description          string `json:"description"`
	QuestionRequirements string `json:"question_requirements"`
	AnswerRequirements   string `json:"answer_requirements"`
	QuestionMaxLen       int    `json:"question_maxlen"`
	AnswerMaxLen         int    `json:"answer_maxlen"`
	AnswerCheckMethod    string `json:"answer_check_method"`
	Status               string `json:"status"`
	TotalQuestions       int    `json:"total_questions"`
	QualifiedQuestions   int    `json:"qualified_questions"`
	CreatedAt            string `json:"created_at"`
}

func toAdminView(bs model.BenchmarkSet) AdminBenchmarkSetView {
	return AdminBenchmarkSetView{
		SetID:                bs.SetID,
		Description:          bs.Description,
		QuestionRequirements: bs.QuestionRequirements,
		AnswerRequirements:   bs.AnswerRequirements,
		QuestionMaxLen:       bs.QuestionMaxLen,
		AnswerMaxLen:         bs.AnswerMaxLen,
		AnswerCheckMethod:    bs.AnswerCheckMethod,
		Status:               bs.Status,
		TotalQuestions:       bs.TotalQuestions,
		QualifiedQuestions:   bs.QualifiedQuestions,
		CreatedAt:            bs.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// HandleAdminCreate handles POST /admin/v1/benchmark-sets
// Accepts a single object or an array of objects.
func (h *BenchmarkSetHandler) HandleAdminCreate(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	// Try array first, fall back to single object
	var reqs []model.BenchmarkSet
	if err := json.Unmarshal(raw, &reqs); err != nil {
		var single model.BenchmarkSet
		if err := json.Unmarshal(raw, &single); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json: expected object or array")
			return
		}
		reqs = []model.BenchmarkSet{single}
	}

	var results []AdminBenchmarkSetView
	for _, req := range reqs {
		if req.SetID == "" {
			writeError(w, http.StatusBadRequest, "set_id is required")
			return
		}
		if req.AnswerCheckMethod == "" {
			req.AnswerCheckMethod = "exact"
		}
		if req.Status == "" {
			req.Status = model.BSStatusActive
		}
		if req.QuestionMaxLen <= 0 {
			req.QuestionMaxLen = 1000
		}
		if req.AnswerMaxLen <= 0 {
			req.AnswerMaxLen = 1000
		}

		bs, err := h.Store.CreateBenchmarkSet(r.Context(), req)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				writeError(w, http.StatusConflict, req.SetID+": already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		results = append(results, toAdminView(*bs))
	}

	if len(results) == 1 {
		writeJSON(w, http.StatusCreated, results[0])
	} else {
		writeJSON(w, http.StatusCreated, results)
	}
}

// HandleAdminUpdate handles PUT /admin/v1/benchmark-sets/{set_id}
func (h *BenchmarkSetHandler) HandleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	setID := r.PathValue("set_id")
	if setID == "" {
		writeError(w, http.StatusBadRequest, "missing set_id")
		return
	}

	var fields map[string]any
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	bs, err := h.Store.UpdateBenchmarkSet(r.Context(), setID, fields)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toAdminView(*bs))
}

// HandleAdminList handles GET /admin/v1/benchmark-sets
func (h *BenchmarkSetHandler) HandleAdminList(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	sets, err := h.Store.ListBenchmarkSets(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]AdminBenchmarkSetView, len(sets))
	for i, bs := range sets {
		views[i] = toAdminView(bs)
	}
	writeJSON(w, http.StatusOK, views)
}

// HandleAdminGet handles GET /admin/v1/benchmark-sets/{set_id}
func (h *BenchmarkSetHandler) HandleAdminGet(w http.ResponseWriter, r *http.Request) {
	setID := r.PathValue("set_id")
	if setID == "" {
		writeError(w, http.StatusBadRequest, "missing set_id")
		return
	}

	bs, err := h.Store.GetBenchmarkSet(r.Context(), setID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if bs == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toAdminView(*bs))
}
