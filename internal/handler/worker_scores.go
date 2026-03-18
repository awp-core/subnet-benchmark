package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type WorkerScoresHandler struct {
	Store *store.Store
}

type minerStatusView struct {
	Address          string  `json:"address"`
	Suspended        bool    `json:"suspended"`
	SuspendedUntil   *string `json:"suspended_until,omitempty"`
	EpochViolations  int     `json:"epoch_violations"`
	LastPollAt       *string `json:"last_poll_at,omitempty"`
	CreatedAt        string  `json:"created_at"`
	TotalQuestions   int     `json:"total_questions"`
	ScoredQuestions  int     `json:"scored_questions"`
	TotalAssignments int     `json:"total_assignments"`
	ScoredAssignments int    `json:"scored_assignments"`
	TotalReward      int64   `json:"total_reward"`
}

func (h *WorkerScoresHandler) HandleMyStatus(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	wk, err := h.Store.GetWorker(r.Context(), worker)
	if err != nil || wk == nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	stats, err := h.Store.GetWorkerAggregateStats(r.Context(), worker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	v := minerStatusView{
		Address:           wk.Address,
		Suspended:         wk.IsSuspended(),
		EpochViolations:   wk.EpochViolations,
		CreatedAt:         wk.CreatedAt.Format(time.RFC3339),
		TotalQuestions:    stats.TotalQuestions,
		ScoredQuestions:   stats.ScoredQuestions,
		TotalAssignments:  stats.TotalAssignments,
		ScoredAssignments: stats.ScoredAssignments,
		TotalReward:       stats.TotalReward,
	}
	if wk.SuspendedUntil != nil {
		s := wk.SuspendedUntil.Format(time.RFC3339)
		v.SuspendedUntil = &s
	}
	if wk.LastPollAt != nil {
		s := wk.LastPollAt.Format(time.RFC3339)
		v.LastPollAt = &s
	}
	writeJSON(w, http.StatusOK, v)
}

type questionScoreView struct {
	QuestionID int64   `json:"question_id"`
	BSID       string  `json:"bs_id"`
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

type assignmentScoreView struct {
	AssignmentID int64   `json:"assignment_id"`
	QuestionID   int64   `json:"question_id"`
	Question     string  `json:"question"`
	Status       string  `json:"status"`
	Score        int     `json:"score"`
	Share        float64 `json:"share"`
	ReplyDDL     string  `json:"reply_ddl"`
	ReplyValid   *bool   `json:"reply_valid,omitempty"`
	ReplyAnswer  *string `json:"reply_answer,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func (h *WorkerScoresHandler) HandleMyQuestions(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	filter := store.QuestionFilter{
		Questioner: worker,
		Status:     r.URL.Query().Get("status"),
	}
	if from := r.URL.Query().Get("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid 'from' time format")
			return
		}
		filter.From = &t
	}
	if to := r.URL.Query().Get("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid 'to' time format")
			return
		}
		filter.To = &t
	}

	questions, err := h.Store.ListQuestionsByFilter(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]questionScoreView, len(questions))
	for i, q := range questions {
		views[i] = toQuestionScoreView(q)
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *WorkerScoresHandler) HandleMyQuestion(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	qID, err := strconv.ParseInt(r.PathValue("question_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid question_id")
		return
	}
	q, err := h.Store.GetQuestion(r.Context(), qID)
	if err != nil || q == nil || q.Questioner != worker {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toQuestionScoreView(*q))
}

// HandleMyAssignments handles GET /api/v1/my/assignments
func (h *WorkerScoresHandler) HandleMyAssignments(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	filter := store.AssignmentFilter{
		Worker: worker,
		Status: r.URL.Query().Get("status"),
	}
	if from := r.URL.Query().Get("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid 'from' time format")
			return
		}
		filter.From = &t
	}
	if to := r.URL.Query().Get("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid 'to' time format")
			return
		}
		filter.To = &t
	}

	assignments, err := h.Store.ListAssignmentsByFilter(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	views, err := h.buildAssignmentViews(r, assignments)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// HandleMyAssignment handles GET /api/v1/my/assignments/{assignment_id}
func (h *WorkerScoresHandler) HandleMyAssignment(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	aID, err := strconv.ParseInt(r.PathValue("assignment_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid assignment_id")
		return
	}
	a, err := h.Store.GetAssignment(r.Context(), aID)
	if err != nil || a == nil || a.Worker != worker {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	q, _ := h.Store.GetQuestion(r.Context(), a.QuestionID)
	qText := ""
	if q != nil {
		qText = q.Question
	}
	writeJSON(w, http.StatusOK, toAssignmentScoreView(*a, qText))
}

func (h *WorkerScoresHandler) buildAssignmentViews(r *http.Request, assignments []model.Assignment) ([]assignmentScoreView, error) {
	qIDs := make(map[int64]bool)
	for _, a := range assignments {
		qIDs[a.QuestionID] = true
	}
	qMap := make(map[int64]string)
	for qID := range qIDs {
		q, err := h.Store.GetQuestion(r.Context(), qID)
		if err != nil {
			return nil, err
		}
		if q != nil {
			qMap[qID] = q.Question
		}
	}
	views := make([]assignmentScoreView, len(assignments))
	for i, a := range assignments {
		views[i] = toAssignmentScoreView(a, qMap[a.QuestionID])
	}
	return views, nil
}

type epochRewardView struct {
	EpochDate       string  `json:"epoch_date"`
	ScoredAsks      int     `json:"scored_asks"`
	ScoredAnswers   int     `json:"scored_answers"`
	TimedOutAnswers int     `json:"timedout_answers"`
	AskScoreSum     int     `json:"ask_score_sum"`
	AnswerScoreSum  int     `json:"answer_score_sum"`
	RawReward       int64   `json:"raw_reward"`
	CompositeScore  float64 `json:"composite_score"`
	FinalReward     int64   `json:"final_reward"`
}

func (h *WorkerScoresHandler) HandleMyEpochs(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	rewards, err := h.Store.ListWorkerEpochRewards(r.Context(), worker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]epochRewardView, len(rewards))
	for i, r := range rewards {
		views[i] = toEpochRewardView(r)
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *WorkerScoresHandler) HandleMyEpoch(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	dateStr := r.PathValue("epoch_date")
	epochDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid epoch_date")
		return
	}
	reward, err := h.Store.GetWorkerEpochReward(r.Context(), worker, epochDate)
	if err != nil || reward == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toEpochRewardView(*reward))
}

func toEpochRewardView(r model.WorkerEpochReward) epochRewardView {
	return epochRewardView{
		EpochDate:       r.EpochDate.Format("2006-01-02"),
		ScoredAsks:      r.ScoredAsks,
		ScoredAnswers:   r.ScoredAnswers,
		TimedOutAnswers: r.TimedOutAnswers,
		AskScoreSum:     r.AskScoreSum,
		AnswerScoreSum:  r.AnswerScoreSum,
		RawReward:       r.RawReward,
		CompositeScore:  r.CompositeScore,
		FinalReward:     r.FinalReward,
	}
}

func toQuestionScoreView(q model.Question) questionScoreView {
	v := questionScoreView{
		QuestionID: q.QuestionID,
		BSID:       q.BSID,
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

func toAssignmentScoreView(a model.Assignment, questionText string) assignmentScoreView {
	return assignmentScoreView{
		AssignmentID: a.AssignmentID,
		QuestionID:   a.QuestionID,
		Question:     questionText,
		Status:       a.Status,
		Score:        a.Score,
		Share:        a.Share,
		ReplyDDL:     a.ReplyDDL.Format(time.RFC3339),
		ReplyValid:   a.ReplyValid,
		ReplyAnswer:  a.ReplyAnswer,
		CreatedAt:    a.CreatedAt.Format(time.RFC3339),
	}
}
