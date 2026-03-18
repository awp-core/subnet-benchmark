package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/awp-core/subnet-benchmark/internal/store"
)

// PublicHandler serves public, unauthenticated protocol data.
type PublicHandler struct {
	Store *store.Store
}

// HandleStats returns aggregate protocol statistics.
func (h *PublicHandler) HandleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.Store.GetProtocolStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// HandleLeaderboard returns top miners by reward.
func (h *PublicHandler) HandleLeaderboard(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := h.Store.GetLeaderboard(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if entries == nil {
		entries = []store.LeaderboardEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// HandlePublicQuestions returns scored questions.
// GET /api/v1/questions?miner=&limit=
func (h *PublicHandler) HandlePublicQuestions(w http.ResponseWriter, r *http.Request) {
	worker := strings.ToLower(r.URL.Query().Get("worker"))
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	questions, err := h.Store.GetRecentQuestions(r.Context(), worker, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if questions == nil {
		questions = []store.PublicQuestion{}
	}
	writeJSON(w, http.StatusOK, questions)
}

// HandlePublicAssignments returns scored invitations for scored questions.
// GET /api/v1/invitations?question_id=&miner=&limit=
func (h *PublicHandler) HandlePublicAssignments(w http.ResponseWriter, r *http.Request) {
	var qID int64
	if v := r.URL.Query().Get("question_id"); v != "" {
		var err error
		qID, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid question_id")
			return
		}
	}
	worker := strings.ToLower(r.URL.Query().Get("worker"))
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	assignments, err := h.Store.ListPublicAssignments(r.Context(), store.PublicAssignmentFilter{
		QuestionID: qID,
		Worker:     worker,
		Limit:      limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if assignments == nil {
		assignments = []store.PublicAssignment{}
	}
	writeJSON(w, http.StatusOK, assignments)
}

// HandlePublicEpochs returns the public epoch list.
func (h *PublicHandler) HandlePublicEpochs(w http.ResponseWriter, r *http.Request) {
	epochs, err := h.Store.ListEpochs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type epochView struct {
		EpochDate   string `json:"epoch_date"`
		TotalReward int64  `json:"total_reward"`
		TotalScored int    `json:"total_scored"`
	}
	var result []epochView
	for _, e := range epochs {
		result = append(result, epochView{
			EpochDate:   e.EpochDate.Format("2006-01-02"),
			TotalReward: e.TotalReward,
			TotalScored: e.TotalScored,
		})
	}
	if result == nil {
		result = []epochView{}
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleRecipientRewards returns per-miner reward breakdown for a recipient.
// Public — no auth required. GET /api/v1/rewards/{address}
func (h *PublicHandler) HandleRecipientRewards(w http.ResponseWriter, r *http.Request) {
	addr := strings.ToLower(r.PathValue("address"))
	if addr == "" {
		writeError(w, http.StatusBadRequest, "missing address")
		return
	}

	rewards, err := h.Store.ListRecipientEpochRewards(r.Context(), addr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type minerRewardView struct {
		EpochDate      string  `json:"epoch_date"`
		WorkerAddress   string  `json:"miner_address"`
		ScoredAsks     int     `json:"scored_asks"`
		ScoredAnswers  int     `json:"scored_answers"`
		CompositeScore float64 `json:"composite_score"`
		FinalReward    int64   `json:"final_reward"`
	}

	result := make([]minerRewardView, 0, len(rewards))
	for _, rw := range rewards {
		result = append(result, minerRewardView{
			EpochDate:      rw.EpochDate.Format("2006-01-02"),
			WorkerAddress:   rw.WorkerAddress,
			ScoredAsks:     rw.ScoredAsks,
			ScoredAnswers:  rw.ScoredAnswers,
			CompositeScore: rw.CompositeScore,
			FinalReward:    rw.FinalReward,
		})
	}
	writeJSON(w, http.StatusOK, result)
}
