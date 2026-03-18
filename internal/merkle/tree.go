package merkle

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Leaf represents a single entry in the Merkle tree.
type Leaf struct {
	Address common.Address
	Amount  *big.Int
}

// Tree is an OpenZeppelin-compatible Merkle tree.
// Uses double-hashing: leaf = keccak256(keccak256(abi.encode(address, amount)))
// Sorted pair hashing for internal nodes (matching OZ MerkleProof.sol).
type Tree struct {
	Leaves []Leaf
	Hashes [][]common.Hash // hashes[0] = leaf hashes, hashes[len-1] = [root]
}

// NewTree builds a Merkle tree from the given leaves.
func NewTree(leaves []Leaf) (*Tree, error) {
	if len(leaves) == 0 {
		return nil, fmt.Errorf("empty leaves")
	}

	t := &Tree{Leaves: leaves}

	// Compute leaf hashes
	leafHashes := make([]common.Hash, len(leaves))
	for i, leaf := range leaves {
		leafHashes[i] = HashLeaf(leaf)
	}

	// Sort leaf hashes for deterministic ordering
	sort.Slice(leafHashes, func(i, j int) bool {
		return compareHashes(leafHashes[i], leafHashes[j]) < 0
	})

	t.Hashes = [][]common.Hash{leafHashes}

	// Build tree bottom-up
	current := leafHashes
	for len(current) > 1 {
		next := make([]common.Hash, 0, (len(current)+1)/2)
		for i := 0; i < len(current); i += 2 {
			if i+1 < len(current) {
				next = append(next, hashPair(current[i], current[i+1]))
			} else {
				next = append(next, current[i]) // Odd node promoted
			}
		}
		t.Hashes = append(t.Hashes, next)
		current = next
	}

	return t, nil
}

// Root returns the Merkle root.
func (t *Tree) Root() common.Hash {
	if len(t.Hashes) == 0 {
		return common.Hash{}
	}
	top := t.Hashes[len(t.Hashes)-1]
	if len(top) == 0 {
		return common.Hash{}
	}
	return top[0]
}

// Proof returns the Merkle proof for the given leaf.
func (t *Tree) Proof(leaf Leaf) ([]common.Hash, error) {
	target := HashLeaf(leaf)

	// Find leaf index in sorted hashes
	idx := -1
	for i, h := range t.Hashes[0] {
		if h == target {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("leaf not found in tree")
	}

	var proof []common.Hash
	for level := 0; level < len(t.Hashes)-1; level++ {
		layer := t.Hashes[level]
		if idx%2 == 0 {
			if idx+1 < len(layer) {
				proof = append(proof, layer[idx+1])
			}
		} else {
			proof = append(proof, layer[idx-1])
		}
		idx /= 2
	}

	return proof, nil
}

// Verify checks a Merkle proof against a root.
func Verify(proof []common.Hash, root common.Hash, leaf common.Hash) bool {
	computed := leaf
	for _, p := range proof {
		computed = hashPair(computed, p)
	}
	return computed == root
}

// HashLeaf computes the double-hashed leaf: keccak256(keccak256(abi.encode(address, amount)))
// This matches OpenZeppelin's StandardMerkleTree leaf encoding.
func HashLeaf(leaf Leaf) common.Hash {
	// abi.encode(address, uint256) = 32 bytes (address padded) + 32 bytes (amount)
	encoded := make([]byte, 64)
	copy(encoded[12:32], leaf.Address.Bytes()) // address left-padded to 32 bytes
	amountBytes := leaf.Amount.Bytes()
	copy(encoded[64-len(amountBytes):64], amountBytes) // uint256 left-padded

	// Double hash
	inner := crypto.Keccak256(encoded)
	return common.BytesToHash(crypto.Keccak256(inner))
}

// hashPair computes the hash of a sorted pair (matching OZ MerkleProof.sol).
func hashPair(a, b common.Hash) common.Hash {
	if compareHashes(a, b) > 0 {
		a, b = b, a
	}
	combined := make([]byte, 64)
	copy(combined[:32], a.Bytes())
	copy(combined[32:], b.Bytes())
	return common.BytesToHash(crypto.Keccak256(combined))
}

func compareHashes(a, b common.Hash) int {
	for i := 0; i < 32; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// HashToHex returns the hex string of a hash (with 0x prefix).
func HashToHex(h common.Hash) string {
	return "0x" + hex.EncodeToString(h[:])
}

// ProofToHex converts a proof to a slice of hex strings.
func ProofToHex(proof []common.Hash) []string {
	result := make([]string, len(proof))
	for i, h := range proof {
		result[i] = HashToHex(h)
	}
	return result
}

// HexToHash parses a hex string (with or without 0x) into a Hash.
func HexToHash(s string) (common.Hash, error) {
	s = strings.TrimPrefix(s, "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(b), nil
}
