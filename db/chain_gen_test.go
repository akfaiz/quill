package db_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/internal/chaingen"
)

// TestChainGenIsUpToDate fails when chain_gen.go no longer matches what the generator
// produces from the SelectBuilder methods.
func TestChainGenIsUpToDate(t *testing.T) {
	want, err := chaingen.Generate(".")
	require.NoError(t, err)
	got, err := os.ReadFile("chain_gen.go")
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "chain_gen.go is stale: run `go generate ./db`")
}
