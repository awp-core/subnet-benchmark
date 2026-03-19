package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/service"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type AdminHandler struct {
	Store      *store.Store
	Settlement *service.SettlementService
	Onchain    *service.OnchainService
	RtConfig   *service.RuntimeConfig
}

// --- Miners ---

type adminWorkerView struct {
	Address         string  `json:"address"`
	Suspended       bool    `json:"suspended"`
	SuspendedUntil  *string `json:"suspended_until,omitempty"`
	EpochViolations int     `json:"epoch_violations"`
	LastPollAt      *string `json:"last_poll_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

func toAdminWorkerView(w model.Worker) adminWorkerView {
	v := adminWorkerView{
		Address:         w.Address,
		Suspended:       w.IsSuspended(),
		EpochViolations: w.EpochViolations,
		CreatedAt:       w.CreatedAt.Format(time.RFC3339),
	}
	if w.SuspendedUntil != nil {
		s := w.SuspendedUntil.Format(time.RFC3339)
		v.SuspendedUntil = &s
	}
	if w.LastPollAt != nil {
		s := w.LastPollAt.Format(time.RFC3339)
		v.LastPollAt = &s
	}
	return v
}

// HandleListWorkers handles GET /admin/v1/miners
func (h *AdminHandler) HandleListWorkers(w http.ResponseWriter, r *http.Request) {
	miners, err := h.Store.ListWorkers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]adminWorkerView, len(miners))
	for i, m := range miners {
		views[i] = toAdminWorkerView(m)
	}
	writeJSON(w, http.StatusOK, views)
}

// HandleGetWorker handles GET /admin/v1/miners/{address}
func (h *AdminHandler) HandleGetWorker(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	m, err := h.Store.GetWorker(r.Context(), address)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if m == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toAdminWorkerView(*m))
}

// --- Questions ---

type adminQuestionView struct {
	QuestionID int64   `json:"question_id"`
	BSID       string  `json:"bs_id"`
	Questioner string  `json:"questioner"`
	Question   string  `json:"question"`
	Answer     string  `json:"answer"`
	Status     string  `json:"status"`
	Score      int     `json:"score"`
	Share      float64 `json:"share"`
	PassRate   float64 `json:"pass_rate"`
	Benchmark  bool    `json:"benchmark"`
	CreatedAt  string  `json:"created_at"`
	ScoredAt   string  `json:"scored_at,omitempty"`
}

func toAdminQuestionView(q model.Question) adminQuestionView {
	v := adminQuestionView{
		QuestionID: q.QuestionID,
		BSID:       q.BSID,
		Questioner: q.Questioner,
		Question:   q.Question,
		Answer:     q.Answer,
		Status:     q.Status,
		Score:      q.Score,
		Share:      q.Share,
		PassRate:   q.PassRate,
		Benchmark:  q.Benchmark,
		CreatedAt:  q.CreatedAt.Format(time.RFC3339),
	}
	if q.ScoredAt != nil {
		v.ScoredAt = q.ScoredAt.Format(time.RFC3339)
	}
	return v
}

// HandleListQuestions handles GET /admin/v1/questions?status=&limit=
func (h *AdminHandler) HandleListQuestions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	questions, err := h.Store.ListAllQuestions(r.Context(), status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]adminQuestionView, len(questions))
	for i, q := range questions {
		views[i] = toAdminQuestionView(q)
	}
	writeJSON(w, http.StatusOK, views)
}

// HandleGetQuestion handles GET /admin/v1/questions/{question_id}
func (h *AdminHandler) HandleGetQuestion(w http.ResponseWriter, r *http.Request) {
	qID, err := strconv.ParseInt(r.PathValue("question_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid question_id")
		return
	}

	q, err := h.Store.GetQuestion(r.Context(), qID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if q == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toAdminQuestionView(*q))
}

// --- Invitations ---

type adminAssignmentView struct {
	AssignmentID int64   `json:"assignment_id"`
	QuestionID   int64   `json:"question_id"`
	Worker       string  `json:"worker"`
	Status       string  `json:"status"`
	Score        int     `json:"score"`
	Share        float64 `json:"share"`
	ReplyDDL     string  `json:"reply_ddl"`
	ReplyValid   *bool   `json:"reply_valid,omitempty"`
	ReplyAnswer  *string `json:"reply_answer,omitempty"`
	RepliedAt    *string `json:"replied_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func toAdminAssignmentView(a model.Assignment) adminAssignmentView {
	v := adminAssignmentView{
		AssignmentID: a.AssignmentID,
		QuestionID:   a.QuestionID,
		Worker:       a.Worker,
		Status:       a.Status,
		Score:        a.Score,
		Share:        a.Share,
		ReplyDDL:     a.ReplyDDL.Format(time.RFC3339),
		ReplyValid:   a.ReplyValid,
		ReplyAnswer:  a.ReplyAnswer,
		CreatedAt:    a.CreatedAt.Format(time.RFC3339),
	}
	if a.RepliedAt != nil {
		s := a.RepliedAt.Format(time.RFC3339)
		v.RepliedAt = &s
	}
	return v
}

// HandleListAssignments handles GET /admin/v1/questions/{question_id}/assignments
func (h *AdminHandler) HandleListAssignments(w http.ResponseWriter, r *http.Request) {
	qID, err := strconv.ParseInt(r.PathValue("question_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid question_id")
		return
	}

	assignments, err := h.Store.ListAssignmentsByQuestion(r.Context(), qID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]adminAssignmentView, len(assignments))
	for i, a := range assignments {
		views[i] = toAdminAssignmentView(a)
	}
	writeJSON(w, http.StatusOK, views)
}

// --- Epochs ---

type adminEpochView struct {
	EpochDate   string  `json:"epoch_date"`
	TotalReward int64   `json:"total_reward"`
	TotalScored int     `json:"total_scored"`
	SettledAt   *string `json:"settled_at,omitempty"`
	MerkleRoot  *string `json:"merkle_root,omitempty"`
	PublishedAt *string `json:"published_at,omitempty"`
}

func toAdminEpochView(e model.Epoch) adminEpochView {
	v := adminEpochView{
		EpochDate:   e.EpochDate.Format("2006-01-02"),
		TotalReward: e.TotalReward,
		TotalScored: e.TotalScored,
		MerkleRoot:  e.MerkleRoot,
	}
	if e.SettledAt != nil {
		s := e.SettledAt.Format(time.RFC3339)
		v.SettledAt = &s
	}
	if e.PublishedAt != nil {
		s := e.PublishedAt.Format(time.RFC3339)
		v.PublishedAt = &s
	}
	return v
}

// HandleListEpochs handles GET /admin/v1/epochs
func (h *AdminHandler) HandleListEpochs(w http.ResponseWriter, r *http.Request) {
	epochs, err := h.Store.ListEpochs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]adminEpochView, len(epochs))
	for i, e := range epochs {
		views[i] = toAdminEpochView(e)
	}
	writeJSON(w, http.StatusOK, views)
}

// HandleGetEpoch handles GET /admin/v1/epochs/{epoch_date}
func (h *AdminHandler) HandleGetEpoch(w http.ResponseWriter, r *http.Request) {
	dateStr := r.PathValue("epoch_date")
	epochDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid epoch_date, use YYYY-MM-DD")
		return
	}

	e, err := h.Store.GetEpoch(r.Context(), epochDate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toAdminEpochView(*e))
}

// --- Settlement trigger ---

// HandleTriggerSettlement handles POST /admin/v1/settle
func (h *AdminHandler) HandleTriggerSettlement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EpochDate string `json:"epoch_date"` // YYYY-MM-DD
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	epochDate, err := time.Parse("2006-01-02", req.EpochDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid epoch_date, use YYYY-MM-DD")
		return
	}

	if h.Settlement == nil {
		writeError(w, http.StatusServiceUnavailable, "settlement service not configured")
		return
	}

	if err := h.Settlement.Settle(r.Context(), epochDate); err != nil {
		writeError(w, http.StatusInternalServerError, "settlement failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"settled": req.EpochDate})
}

// HandlePublishMerkleRoot handles POST /admin/v1/publish
func (h *AdminHandler) HandlePublishMerkleRoot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EpochDate string `json:"epoch_date"` // YYYY-MM-DD
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	epochDate, err := time.Parse("2006-01-02", req.EpochDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid epoch_date, use YYYY-MM-DD")
		return
	}

	if h.Onchain == nil {
		writeError(w, http.StatusServiceUnavailable, "on-chain publishing not configured")
		return
	}

	if err := h.Onchain.PublishMerkleRoot(r.Context(), epochDate); err != nil {
		writeError(w, http.StatusInternalServerError, "publish failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"published": req.EpochDate})
}

// --- Config ---

type configView struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
}

// HandleListConfig handles GET /admin/v1/config
func (h *AdminHandler) HandleListConfig(w http.ResponseWriter, r *http.Request) {
	configs, err := h.Store.ListConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]configView, len(configs))
	for i, c := range configs {
		views[i] = configView{
			Key:         c.Key,
			Value:       c.Value,
			Description: c.Description,
			UpdatedAt:   c.UpdatedAt.Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, views)
}

// HandleUpdateConfig handles PUT /admin/v1/config
func (h *AdminHandler) HandleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Key == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "key and value are required")
		return
	}

	if err := h.Store.SetConfig(r.Context(), req.Key, req.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Reload runtime config so changes take effect immediately
	if h.RtConfig != nil {
		if err := h.RtConfig.Load(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "config updated but reload failed: "+err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"updated": req.Key})
}
