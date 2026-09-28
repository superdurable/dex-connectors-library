// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
)

// exampleModelLine is the line of MapToGenerateTextRequest that the README's variants replace.
const exampleModelLine = "\t\tModel:        flow.summaryModel.Model,"

// TestREADMEGoSnippetsComeFromTheRunnableExample keeps documentation snippets copied from runnable example code.
func TestREADMEGoSnippetsComeFromTheRunnableExample(t *testing.T) {
	readme := readREADME(t)
	exampleSources := readExampleSources(t)
	snippets := readmeCodeBlocks(readme, "```go")
	require.NotEmpty(t, snippets)
	for _, snippet := range snippets {
		require.Truef(t, isIndentedSnippetInSources(snippet, exampleSources), "README Go snippet is not copied from the example:\n%s", snippet)
	}
}

// TestREADMEVariantsChangeOnlyTheExampleModelLine keeps the named-model and per-run variants one line away from the example.
func TestREADMEVariantsChangeOnlyTheExampleModelLine(t *testing.T) {
	readme := readREADME(t)
	require.Contains(t, readExampleSources(t)[1], "\n"+exampleModelLine+"\n")
	for fence, expected := range map[string]string{
		"```go named-model":   "\t\tModel:        cmp.Or(flow.summaryModel.Model, \"anthropic/claude-opus-5-5\"),",
		"```go per-run-model": "\t\tModel:        cmp.Or(request.Model, flow.summaryModel.Model),",
	} {
		variants := readmeCodeBlocks(readme, fence)
		require.Equal(t, []string{expected}, variants, fence)
	}
	_, err := llmrouter.New(llmrouter.Config{Model: "anthropic/claude-opus-5-5"}, staticCredentials(allTestAPIKeys()))
	require.NoError(t, err, "the README's named model is a valid selection")
	require.Contains(t, readme, "`cmp.Or(flow.summaryModel.Model, \"anthropic\")`")
}

// TestREADMENamesThePinnedProvidersAndTheirDefaults keeps the routing table equal to go.mod and the provider defaults.
func TestREADMENamesThePinnedProvidersAndTheirDefaults(t *testing.T) {
	readme := readREADME(t)
	goMod, err := os.ReadFile("go.mod")
	require.NoError(t, err)
	for _, directory := range []string{"connectors/openai", "connectors/anthropic", "connectors/google/gemini"} {
		version := requiredModuleVersion(t, string(goMod), "github.com/superdurable/dex-connectors-library/"+directory)
		require.Contains(t, readme, "| `"+directory+"/"+version+"` |", "the README names the pinned %s release", directory)
	}
	for _, defaultModel := range []string{openai.DefaultConfig().Model, claude.DefaultConfig().Model, gemini.DefaultConfig().Model} {
		require.Contains(t, readme, "`"+defaultModel+"`", "the README names each provider connector's default model")
	}
	require.Contains(t, readme, "| `maxResponseBytes` | `8388608` (8 MiB) |")
}

func readREADME(t *testing.T) string {
	t.Helper()
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	return string(readme)
}

func readExampleSources(t *testing.T) []string {
	t.Helper()
	var sources []string
	for _, path := range []string{
		filepath.Join("examples", "summarize-text", "main.go"),
		filepath.Join("examples", "summarize-text", "flow", "workflow.go"),
	} {
		source, err := os.ReadFile(path)
		require.NoError(t, err)
		sources = append(sources, string(source))
	}
	return sources
}

// readmeCodeBlocks returns the contents of every fenced block whose opening line is exactly fence.
func readmeCodeBlocks(readme string, fence string) []string {
	var blocks []string
	var blockLines []string
	isInBlock := false
	for _, line := range strings.Split(readme, "\n") {
		switch {
		case !isInBlock && line == fence:
			isInBlock, blockLines = true, nil
		case isInBlock && line == "```":
			isInBlock = false
			blocks = append(blocks, strings.Join(blockLines, "\n"))
		case isInBlock:
			blockLines = append(blockLines, line)
		}
	}
	return blocks
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

func requiredModuleVersion(t *testing.T, goMod string, modulePath string) string {
	t.Helper()
	for _, line := range strings.Split(goMod, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == modulePath {
			return fields[1]
		}
	}
	t.Fatalf("go.mod does not require %s", modulePath)
	return ""
}
