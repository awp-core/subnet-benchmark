package minhash

import (
	"testing"
)

func TestIdenticalTexts(t *testing.T) {
	a := Generate("What is 2^10 + 3^7?")
	b := Generate("What is 2^10 + 3^7?")
	sim := Jaccard(a, b)
	if sim != 1.0 {
		t.Errorf("identical texts: similarity = %f, want 1.0", sim)
	}
}

func TestSimilarTexts(t *testing.T) {
	a := Generate("What is 2^10 + 3^7?")
	b := Generate("What is 2^10 + 3^8?")
	sim := Jaccard(a, b)
	// Only one character changed, should be highly similar
	if sim < 0.5 {
		t.Errorf("similar texts: similarity = %f, expected > 0.5", sim)
	}
}

func TestDifferentTexts(t *testing.T) {
	a := Generate("What is the capital of France?")
	b := Generate("Solve the quadratic equation")
	sim := Jaccard(a, b)
	if sim > 0.3 {
		t.Errorf("different texts: similarity = %f, expected < 0.3", sim)
	}
}

func TestSerializeRoundtrip(t *testing.T) {
	orig := Generate("test serialization")
	data := orig.ToBytes()
	if len(data) != SignatureSize {
		t.Fatalf("bytes length = %d, want %d", len(data), SignatureSize)
	}
	restored := FromBytes(data)
	if orig != restored {
		t.Error("round-trip serialization failed")
	}
}

func TestEmptyText(t *testing.T) {
	a := Generate("")
	b := Generate("")
	sim := Jaccard(a, b)
	// Empty texts, signatures are all MaxUint64, should be identical
	if sim != 1.0 {
		t.Errorf("empty texts: similarity = %f, want 1.0", sim)
	}
}
