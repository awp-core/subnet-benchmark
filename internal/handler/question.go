package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/awp-core/subnet-benchmark/internal/service"
)

type QuestionHandler struct {
	Service *service.QuestionService
}

type submitQuestionRequest struct {
	BSID     string `json:"bs_id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// HandleSubmit handles POST /api/v1/questions
func (h *QuestionHandler) HandleSubmit(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var req submitQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.BSID == "" || req.Question == "" || req.Answer == "" {
		writeError(w, http.StatusBadRequest, "bs_id, question, and answer are required")
		return
	}

	result, err := h.Service.SubmitQuestion(r.Context(), service.SubmitQuestionRequest{
		BSID:     req.BSID,
		Question: req.Question,
		Answer:   req.Answer,
		Worker:   worker,
	})
	if err != nil {
		var ue *service.UserError
		if errors.As(err, &ue) {
			status := http.StatusBadRequest
			switch ue.Code {
			case "not_enough_miners":
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, ue.Code+": "+ue.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]int64{"question_id": result.QuestionID})
}
