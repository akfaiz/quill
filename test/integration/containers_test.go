package integration

import (
	"context"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

// Set GOQUERY_SKIP_CONTAINERS=1 to skip the PostgreSQL and MySQL suites (no Docker).
const skipEnv = "GOQUERY_SKIP_CONTAINERS"

var (
	shutdownMu sync.Mutex
	shutdowns  []func(context.Context)
)

// onShutdown registers cleanup to run after the last test, newest first.
func onShutdown(fn func(context.Context)) {
	shutdownMu.Lock()
	defer shutdownMu.Unlock()
	shutdowns = append(shutdowns, fn)
}

func TestMain(m *testing.M) {
	code := m.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	shutdownMu.Lock()
	for _, shutdown := range slices.Backward(shutdowns) {
		shutdown(ctx)
	}
	shutdownMu.Unlock()
	cancel()
	os.Exit(code)
}

// requireContainers skips the test when containers are switched off or Docker is unreachable.
func requireContainers(t *testing.T) {
	t.Helper()
	if os.Getenv(skipEnv) != "" {
		t.Skipf("%s is set", skipEnv)
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
}

// terminate stops and removes a container.
func terminate(c testcontainers.Container) func(context.Context) {
	return func(ctx context.Context) {
		_ = testcontainers.TerminateContainer(c, testcontainers.StopContext(ctx))
	}
}
