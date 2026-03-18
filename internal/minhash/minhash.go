package minhash

import (
	"encoding/binary"
	"math"
)

const (
	NumHashes     = 128
	NGramSize     = 3
	SignatureSize = NumHashes * 8 // 8 bytes per hash value (uint64)
)

// Signature is a MinHash signature consisting of NumHashes uint64 values.
type Signature [NumHashes]uint64

// Generate produces a MinHash signature for the given text.
func Generate(text string) Signature {
	ngrams := extractNGrams(text, NGramSize)
	var sig Signature
	for i := range sig {
		sig[i] = math.MaxUint64
	}

	if len(ngrams) == 0 {
		return sig
	}

	for _, ng := range ngrams {
		ngBytes := []byte(ng)
		for i := 0; i < NumHashes; i++ {
			val := hashWithSeed(ngBytes, uint64(i))
			if val < sig[i] {
				sig[i] = val
			}
		}
	}
	return sig
}

// hashWithSeed computes an FNV-1a variant hash using seed as the initial value.
func hashWithSeed(data []byte, seed uint64) uint64 {
	// FNV-1a offset basis XOR seed
	h := uint64(14695981039346656037) ^ seed
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211 // FNV prime
	}
	return h
}

// Jaccard estimates the Jaccard similarity between two MinHash signatures (0.0 ~ 1.0).
func Jaccard(a, b Signature) float64 {
	matches := 0
	for i := 0; i < NumHashes; i++ {
		if a[i] == b[i] {
			matches++
		}
	}
	return float64(matches) / float64(NumHashes)
}

// ToBytes serializes the signature to a byte array.
func (s Signature) ToBytes() []byte {
	buf := make([]byte, SignatureSize)
	for i, v := range s {
		binary.LittleEndian.PutUint64(buf[i*8:], v)
	}
	return buf
}

// FromBytes deserializes a signature from a byte array.
func FromBytes(data []byte) Signature {
	var sig Signature
	if len(data) < SignatureSize {
		return sig
	}
	for i := range sig {
		sig[i] = binary.LittleEndian.Uint64(data[i*8:])
	}
	return sig
}

// extractNGrams splits text into character-level n-grams.
func extractNGrams(text string, n int) []string {
	runes := []rune(text)
	if len(runes) < n {
		if len(runes) > 0 {
			return []string{string(runes)}
		}
		return nil
	}
	result := make([]string, 0, len(runes)-n+1)
	for i := 0; i <= len(runes)-n; i++ {
		result = append(result, string(runes[i:i+n]))
	}
	return result
}
