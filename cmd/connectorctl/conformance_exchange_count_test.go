// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedConformanceDecoderAllowsZeroRequestFinalAttempt(t *testing.T) {
	template, err := os.ReadFile("conformance_generated.go.txt")
	require.NoError(t, err)
	_, conditionTail, found := strings.Cut(string(template), "if intermediate.ExchangeCount <= previousExchangeCount")
	require.True(t, found)
	conditionTail, _, found = strings.Cut(conditionTail, " { return nil, errors.New(\"invalid intermediate exchange count\") }")
	require.True(t, found)
	condition := "intermediate.ExchangeCount <= previousExchangeCount" + conditionTail

	testDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(testDirectory, "go.mod"), []byte("module example.com/conformance-count\n\ngo 1.24.0\n"), 0o644))
	testSource := fmt.Sprintf(`package conformancecount
import "testing"
func invalid(previousExchangeCount, exchangeCount, totalExchanges int) bool {
 intermediate := struct{ ExchangeCount int }{exchangeCount}
 scenario := struct{ Exchanges []int }{make([]int, totalExchanges)}
 return %s
}
func TestExchangeCountBoundary(t *testing.T) {
 for _, testCase := range []struct{ previous, current, total int; invalid bool }{
  {0, 1, 2, false},
  {0, 2, 2, false}, // A final retry may finish without another request.
  {0, 3, 2, true},
  {1, 1, 2, true},
 } {
  if actual := invalid(testCase.previous, testCase.current, testCase.total); actual != testCase.invalid {
   t.Errorf("count %%d after %%d of %%d: invalid=%%t, want %%t", testCase.current, testCase.previous, testCase.total, actual, testCase.invalid)
  }
 }
}
`, condition)
	require.NoError(t, os.WriteFile(filepath.Join(testDirectory, "exchange_count_test.go"), []byte(testSource), 0o644))
	command := exec.Command("go", "test", ".")
	command.Dir = testDirectory
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}
