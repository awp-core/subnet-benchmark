package handler

import (
	"testing"

	"github.com/awp-core/subnet-benchmark/internal/store"
	"github.com/awp-core/subnet-benchmark/internal/testutil"
)

func testStoreAndDB(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testutil.NewTestDB(t))
}
