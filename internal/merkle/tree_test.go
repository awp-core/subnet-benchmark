package merkle

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestTreeSingleLeaf(t *testing.T) {
	leaves := []Leaf{
		{Address: common.HexToAddress("0x1111111111111111111111111111111111111111"), Amount: big.NewInt(1000)},
	}
	tree, err := NewTree(leaves)
	if err != nil {
		t.Fatalf("new tree: %v", err)
	}
	root := tree.Root()
	if root == (common.Hash{}) {
		t.Fatal("root should not be zero")
	}

	proof, err := tree.Proof(leaves[0])
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	if len(proof) != 0 {
		t.Errorf("single leaf proof should be empty, got %d", len(proof))
	}

	leafHash := HashLeaf(leaves[0])
	if !Verify(proof, root, leafHash) {
		t.Error("verification failed")
	}
}

func TestTreeMultipleLeaves(t *testing.T) {
	leaves := []Leaf{
		{Address: common.HexToAddress("0xaaaa"), Amount: big.NewInt(100)},
		{Address: common.HexToAddress("0xbbbb"), Amount: big.NewInt(200)},
		{Address: common.HexToAddress("0xcccc"), Amount: big.NewInt(300)},
		{Address: common.HexToAddress("0xdddd"), Amount: big.NewInt(400)},
		{Address: common.HexToAddress("0xeeee"), Amount: big.NewInt(500)},
	}

	tree, err := NewTree(leaves)
	if err != nil {
		t.Fatalf("new tree: %v", err)
	}

	root := tree.Root()
	if root == (common.Hash{}) {
		t.Fatal("root should not be zero")
	}

	// Verify every leaf
	for _, leaf := range leaves {
		proof, err := tree.Proof(leaf)
		if err != nil {
			t.Fatalf("proof for %s: %v", leaf.Address.Hex(), err)
		}
		leafHash := HashLeaf(leaf)
		if !Verify(proof, root, leafHash) {
			t.Errorf("verification failed for %s", leaf.Address.Hex())
		}
	}
}

func TestTreeDeterministic(t *testing.T) {
	leaves := []Leaf{
		{Address: common.HexToAddress("0xaaaa"), Amount: big.NewInt(100)},
		{Address: common.HexToAddress("0xbbbb"), Amount: big.NewInt(200)},
	}

	tree1, _ := NewTree(leaves)
	tree2, _ := NewTree(leaves)

	if tree1.Root() != tree2.Root() {
		t.Error("same leaves should produce same root")
	}
}

func TestTreeDifferentOrder(t *testing.T) {
	leaves1 := []Leaf{
		{Address: common.HexToAddress("0xaaaa"), Amount: big.NewInt(100)},
		{Address: common.HexToAddress("0xbbbb"), Amount: big.NewInt(200)},
	}
	leaves2 := []Leaf{
		{Address: common.HexToAddress("0xbbbb"), Amount: big.NewInt(200)},
		{Address: common.HexToAddress("0xaaaa"), Amount: big.NewInt(100)},
	}

	tree1, _ := NewTree(leaves1)
	tree2, _ := NewTree(leaves2)

	// Sorted internally, so order doesn't matter
	if tree1.Root() != tree2.Root() {
		t.Error("different input order should produce same root")
	}
}

func TestWrongProofFails(t *testing.T) {
	leaves := []Leaf{
		{Address: common.HexToAddress("0xaaaa"), Amount: big.NewInt(100)},
		{Address: common.HexToAddress("0xbbbb"), Amount: big.NewInt(200)},
	}
	tree, _ := NewTree(leaves)
	root := tree.Root()

	// Forge a leaf with wrong amount
	fakeLeaf := Leaf{Address: common.HexToAddress("0xaaaa"), Amount: big.NewInt(999)}
	fakeHash := HashLeaf(fakeLeaf)

	// Use real proof for the real leaf
	proof, _ := tree.Proof(leaves[0])

	if Verify(proof, root, fakeHash) {
		t.Error("forged leaf should not verify")
	}
}

func TestProofToHex(t *testing.T) {
	leaves := []Leaf{
		{Address: common.HexToAddress("0xaaaa"), Amount: big.NewInt(100)},
		{Address: common.HexToAddress("0xbbbb"), Amount: big.NewInt(200)},
	}
	tree, _ := NewTree(leaves)

	proof, _ := tree.Proof(leaves[0])
	hexProof := ProofToHex(proof)
	for _, h := range hexProof {
		if len(h) != 66 { // "0x" + 64 hex chars
			t.Errorf("hex proof element length = %d, want 66", len(h))
		}
	}

	rootHex := HashToHex(tree.Root())
	if len(rootHex) != 66 {
		t.Errorf("root hex length = %d, want 66", len(rootHex))
	}
}

func TestEmptyLeaves(t *testing.T) {
	_, err := NewTree(nil)
	if err == nil {
		t.Error("expected error for empty leaves")
	}
}
