// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claude_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
)

// TestREADMEGoSnippetsComeFromTheRunnableExample keeps documentation snippets copied from runnable example code.
func TestREADMEGoSnippetsComeFromTheRunnableExample(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	require.Contains(t, string(readme), "| `model` | `"+claude.DefaultConfig().Model+"` |", "the README names the manifest's default model")
	defaultMaxOutputTokens := strconv.FormatInt(claude.DefaultConfig().DefaultMaxOutputTokens, 10)
	require.Contains(t, string(readme), "| `defaultMaxOutputTokens` | `"+defaultMaxOutputTokens+"` |", "the README names the manifest's max_tokens default")
	require.Contains(t, string(readme), "The `"+defaultMaxOutputTokens+"` default is sized", "the README explains the manifest's max_tokens default")
	var exampleSources []string
	for _, path := range []string{
		filepath.Join("examples", "summarize-text", "main.go"),
		filepath.Join("examples", "summarize-text", "flow", "workflow.go"),
	} {
		source, err := os.ReadFile(path)
		require.NoError(t, err)
		exampleSources = append(exampleSources, string(source))
	}
	snippets := readmeGoSnippets(string(readme))
	require.NotEmpty(t, snippets)
	for _, snippet := range snippets {
		require.Truef(t, isIndentedSnippetInSources(snippet, exampleSources), "README Go snippet is not copied from the example:\n%s", snippet)
	}
}

func readmeGoSnippets(readme string) []string {
	var snippets []string
	var snippetLines []string
	isInGoBlock := false
	for _, line := range strings.Split(readme, "\n") {
		switch {
		case !isInGoBlock && line == "```go":
			isInGoBlock, snippetLines = true, nil
		case isInGoBlock && line == "```":
			isInGoBlock = false
			snippets = append(snippets, strings.Join(snippetLines, "\n"))
		case isInGoBlock:
			snippetLines = append(snippetLines, line)
		}
	}
	return snippets
}

// isIndentedSnippetInSources reports whether snippet appears in a source at an indentation depth of up to three tabs.
func isIndentedSnippetInSources(snippet string, sources []string) bool {
	for depth := 0; depth <= 3; depth++ {
		indentation := strings.Repeat("\t", depth)
		lines := strings.Split(snippet, "\n")
		for index, line := range lines {
			if line != "" {
				lines[index] = indentation + line
			}
		}
		indented := strings.Join(lines, "\n")
		for _, source := range sources {
			if strings.Contains(source, "\n"+indented+"\n") {
				return true
			}
		}
	}
	return false
}
