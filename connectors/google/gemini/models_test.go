// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
)

// liveDefaultModel is the model TestLiveGenerateContent calls when GEMINI_CONNECTOR_TEST_MODEL is unset.
const liveDefaultModel = "gemini-3.5-flash-lite"

// TestDocumentedModelsAreOpenToNewProjects keeps the live test and READMEs off Gemini 2.x models, which new projects cannot call.
func TestDocumentedModelsAreOpenToNewProjects(t *testing.T) {
	restricted := []string{"gemini-2.0-", "gemini-2.5-"}
	require.Equal(t, gemini.DefaultConfig().Model, liveDefaultModel, "the live test verifies the model that a blank connection calls")
	for _, prefix := range restricted {
		require.NotContains(t, liveDefaultModel, prefix)
	}
	for _, path := range []string{
		"README.md", filepath.Join("examples", "generate-summary", "README.md"), filepath.Join("examples", "summarize-text", "README.md"),
	} {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, prefix := range restricted {
			require.Falsef(t, strings.Contains(string(contents), prefix), "%s shows a %s* model that new projects cannot call", path, prefix)
		}
	}
}

// TestREADMEGoSnippetsComeFromTheRunnableExample keeps documentation snippets copied from runnable example code.
func TestREADMEGoSnippetsComeFromTheRunnableExample(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	require.Contains(t, string(readme), "| `model` | `"+gemini.DefaultConfig().Model+"` |", "the README names the manifest's default model")
	var exampleSources []string
	for _, path := range []string{
		filepath.Join("examples", "generate-summary", "main.go"),
		filepath.Join("examples", "generate-summary", "flow", "workflow.go"),
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
