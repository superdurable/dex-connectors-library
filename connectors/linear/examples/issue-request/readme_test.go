// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	issuerequest "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-request/flow"
)

// TestREADMEStartFlowSampleIsAValidRequest decodes the Start Flow sample strictly and validates it.
func TestREADMEStartFlowSampleIsAValidRequest(t *testing.T) {
	samples := fencedBlocks(readFile(t, "README.md"), "json")
	require.Len(t, samples, 1)
	var input issuerequest.Input
	decoder := json.NewDecoder(strings.NewReader(samples[0]))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&input))
	request, err := issuerequest.BuildIssueRequest(input, "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4")
	require.NoError(t, err)
	require.Equal(t, "In Progress", request.StateName)
}

// TestConnectorREADMESnippetIsTheExampleCode keeps the connector README's Go snippet runnable.
func TestConnectorREADMESnippetIsTheExampleCode(t *testing.T) {
	snippets := fencedBlocks(readFile(t, filepath.Join("..", "..", "README.md")), "go")
	require.Len(t, snippets, 1)
	require.Contains(t, readFile(t, "main.go"), snippets[0])
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}

func fencedBlocks(markdown string, language string) []string {
	var blocks []string
	var lines []string
	isInBlock := false
	for _, line := range strings.Split(markdown, "\n") {
		switch {
		case !isInBlock && line == "```"+language:
			isInBlock, lines = true, nil
		case isInBlock && line == "```":
			isInBlock = false
			blocks = append(blocks, strings.Join(lines, "\n"))
		case isInBlock:
			lines = append(lines, line)
		}
	}
	return blocks
}
