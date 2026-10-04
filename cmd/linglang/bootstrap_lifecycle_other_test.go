//go:build !darwin && !linux

package main

import "testing"

func testBootstrapRunLifetime(t *testing.T, _ string, _ []string) {
	t.Helper()
	t.Skip("process-group cancellation regression requires Linux or macOS")
}
