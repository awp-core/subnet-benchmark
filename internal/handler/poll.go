package handler

import (
	"net/http"

	"github.com/awp-core/subnet-benchmark/internal/service"
)

type PollHandler struct {
	Service *service.PollService
}

// HandlePoll handles GET /api/v1/poll
func (h *PollHandler) HandlePoll(w http.ResponseWriter, r *http.Request) {
	worker := WorkerAddressFromContext(r.Context())
	if worker == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	result, err := h.Service.PollOnline(r.Context(), worker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
