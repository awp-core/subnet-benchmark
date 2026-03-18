package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// MerkleProofRow represents a stored merkle proof record.
type MerkleProofRow struct {
	EpochDate    time.Time
	Recipient string
	Amount       int64
	LeafHash     string
	Proof        []string
	Claimed      bool
}

// SetEpochMerkleRoot sets the merkle root for an epoch.
func (s *Store) SetEpochMerkleRoot(ctx context.Context, epochDate time.Time, root string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE epochs SET merkle_root = $1 WHERE epoch_date = $2`, root, epochDate)
	if err != nil {
		return fmt.Errorf("set merkle root: %w", err)
	}
	return nil
}

// SetEpochPublished marks an epoch's merkle root as published on-chain.
func (s *Store) SetEpochPublished(ctx context.Context, epochDate time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE epochs SET published_at = now() WHERE epoch_date = $1`, epochDate)
	if err != nil {
		return fmt.Errorf("set epoch published: %w", err)
	}
	return nil
}

// SaveMerkleProof stores a merkle proof for a miner in an epoch.
func (s *Store) SaveMerkleProof(ctx context.Context, p MerkleProofRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO merkle_proofs (epoch_date, recipient, amount, leaf_hash, proof)
		VALUES ($1, $2, $3, $4, $5)`,
		p.EpochDate, p.Recipient, p.Amount, p.LeafHash, pq.Array(p.Proof))
	if err != nil {
		return fmt.Errorf("save merkle proof: %w", err)
	}
	return nil
}

// GetMerkleProof returns the merkle proof for a miner in an epoch.
func (s *Store) GetMerkleProof(ctx context.Context, epochDate time.Time, recipientAddr string) (*MerkleProofRow, error) {
	var p MerkleProofRow
	err := s.db.QueryRowContext(ctx, `
		SELECT epoch_date, recipient, amount, leaf_hash, proof, claimed
		FROM merkle_proofs WHERE epoch_date = $1 AND recipient = $2`,
		epochDate, recipientAddr,
	).Scan(&p.EpochDate, &p.Recipient, &p.Amount, &p.LeafHash, pq.Array(&p.Proof), &p.Claimed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get merkle proof: %w", err)
	}
	return &p, nil
}

// ListMerkleProofsByRecipient returns all merkle proofs for a miner, newest first.
func (s *Store) ListMerkleProofsByRecipient(ctx context.Context, recipientAddr string) ([]MerkleProofRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT epoch_date, recipient, amount, leaf_hash, proof, claimed
		FROM merkle_proofs WHERE recipient = $1
		ORDER BY epoch_date DESC`, recipientAddr)
	if err != nil {
		return nil, fmt.Errorf("list merkle proofs: %w", err)
	}
	defer rows.Close()

	var result []MerkleProofRow
	for rows.Next() {
		var p MerkleProofRow
		if err := rows.Scan(&p.EpochDate, &p.Recipient, &p.Amount, &p.LeafHash, pq.Array(&p.Proof), &p.Claimed); err != nil {
			return nil, fmt.Errorf("scan merkle proof: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// GetEpochMerkleRoot returns the merkle root for an epoch.
func (s *Store) GetEpochMerkleRoot(ctx context.Context, epochDate time.Time) (string, error) {
	var root sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT merkle_root FROM epochs WHERE epoch_date = $1`, epochDate).Scan(&root)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get epoch merkle root: %w", err)
	}
	if !root.Valid {
		return "", nil
	}
	return root.String, nil
}
