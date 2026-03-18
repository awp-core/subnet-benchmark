package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/awp-core/subnet-benchmark/internal/service"
)

type AnswerHandler struct {
	Service *service.AnswerService
}

type submitAnswerRequest struct {
	QuestionID int64  `json:"question_id"`
	Valid      bool   `json:"valid"`
	Answer     string `json:"answer"`
}

// HandleSubmit handles POST /api/v1/answers
func (h *AnswerHandler) HandleSubmit(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var req submitAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.QuestionID == 0 {
		writeError(w, http.StatusBadRequest, "question_id is required")
		return
	}

	err := h.Service.SubmitAnswer(r.Context(), service.SubmitAnswerRequest{
		QuestionID: req.QuestionID,
		Valid:      req.Valid,
		Answer:     req.Answer,
		Worker:     worker,
	})
	if err != nil {
		var ue *service.UserError
		if errors.As(err, &ue) {
			writeError(w, http.StatusBadRequest, ue.Code+": "+ue.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}
