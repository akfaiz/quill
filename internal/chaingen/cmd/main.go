// Command chaingen writes db/chain_gen.go. Run it through `go generate ./db`.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/akfaiz/quill/internal/chaingen"
)

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	src, err := chaingen.Generate(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "chaingen:", err)
		os.Exit(1)
	}
	out := filepath.Join(dir, chaingen.OutFile)
	//nolint:gosec // dir comes from the go:generate directive
	if err := os.WriteFile(out, src, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "chaingen:", err)
		os.Exit(1)
	}
}
