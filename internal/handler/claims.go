package handler

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/store"
)

type ClaimsHandler struct {
	Store *store.Store
}

type claimView struct {
	EpochDate  string   `json:"epoch_date"`
	Recipient  string   `json:"recipient"`
	Amount     int64    `json:"amount"`
	LeafHash   string   `json:"leaf_hash"`
	Proof      []string `json:"proof"`
	MerkleRoot string   `json:"merkle_root"`
	Claimed    bool     `json:"claimed"`
}

// HandleClaims handles GET /api/v1/claims/{address}
// Public — no authentication required.
func (h *ClaimsHandler) HandleClaims(w http.ResponseWriter, r *http.Request) {
	address := strings.ToLower(r.PathValue("address"))
	if address == "" {
		writeError(w, http.StatusBadRequest, "missing address")
		return
	}

	proofs, err := h.Store.ListMerkleProofsByRecipient(r.Context(), address)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]claimView, 0, len(proofs))
	for _, p := range proofs {
		root, err := h.Store.GetEpochMerkleRoot(r.Context(), p.EpochDate)
		if err != nil {
			log.Printf("get merkle root for %s: %v", p.EpochDate.Format("2006-01-02"), err)
			continue
		}
		if root == "" {
			continue
		}
		views = append(views, claimView{
			EpochDate:  p.EpochDate.Format("2006-01-02"),
			Recipient:    p.Recipient,
			Amount:     p.Amount,
			LeafHash:   p.LeafHash,
			Proof:      p.Proof,
			MerkleRoot: root,
			Claimed:    p.Claimed,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

// HandleClaim handles GET /api/v1/claims/{address}/{epoch_date}
// Public — no authentication required.
func (h *ClaimsHandler) HandleClaim(w http.ResponseWriter, r *http.Request) {
	address := strings.ToLower(r.PathValue("address"))
	if address == "" {
		writeError(w, http.StatusBadRequest, "missing address")
		return
	}

	dateStr := r.PathValue("epoch_date")
	epochDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid epoch_date, use YYYY-MM-DD")
		return
	}

	proof, err := h.Store.GetMerkleProof(r.Context(), epochDate, address)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if proof == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	root, err := h.Store.GetEpochMerkleRoot(r.Context(), epochDate)
	if err != nil {
		log.Printf("get merkle root for %s: %v", dateStr, err)
	}

	writeJSON(w, http.StatusOK, claimView{
		EpochDate:  proof.EpochDate.Format("2006-01-02"),
		Recipient:    proof.Recipient,
		Amount:     proof.Amount,
		LeafHash:   proof.LeafHash,
		Proof:      proof.Proof,
		MerkleRoot: root,
		Claimed:    proof.Claimed,
	})
}
