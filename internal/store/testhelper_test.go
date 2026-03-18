package store

import (
	"testing"

	"github.com/awp-core/subnet-benchmark/internal/testutil"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return New(testutil.NewTestDB(t))
}
