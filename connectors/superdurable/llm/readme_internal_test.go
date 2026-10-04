// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
		"```go named-model":   "\t\tModel:        cmp.Or(flow.summaryModel.Model, \"claude-opus-5-5\"),",
		"```go per-run-model": "\t\tModel:        cmp.Or(request.Model, flow.summaryModel.Model),",
	} {
		require.Equal(t, []string{expected}, readmeCodeBlocks(readme, fence), fence)
	}
	_, err := providerAPIs[ProviderAnthropic].newWireFormat(&Config{Provider: ProviderAnthropic})
	require.NoError(t, err)
	_, err = providerAPIs[ProviderAnthropic].baseURLForRegion(ProviderAnthropic, RegionGlobal)
	require.NoError(t, err, "the README's named model belongs to a served provider")
}

// TestREADMEDescribesEveryServedProviderAndRegion keeps the provider and region tables equal to providerAPIs.
func TestREADMEDescribesEveryServedProviderAndRegion(t *testing.T) {
	readme := readREADME(t)
	for _, provider := range providerOrder {
		api := providerAPIs[provider]
		row := readmeProviderTableRow(t, readme, provider)
		require.Contains(t, row, "`"+api.defaultModel+"`", "the provider table names %s's default model", provider)
		for region, baseURL := range api.regionBaseURLs {
			require.Contains(t, row, "`"+string(region)+"`", "the provider table names %s region %s", provider, region)
			require.Contains(t, readme, baseURL, "the README names %s's %s API", provider, region)
		}
		require.Contains(t, readme, " (`"+string(provider)+"`)\n\n", "the provider reference has a section for %s", provider)
	}
}

// TestREADMEInstallsTheManifestVersion keeps the go get command on the version this module releases.
func TestREADMEInstallsTheManifestVersion(t *testing.T) {
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest struct {
		Metadata struct {
			Version string `yaml:"version"`
		} `yaml:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.Contains(t, readREADME(t),
		"go get github.com/superdurable/dex-connectors-library/connectors/superdurable/llm@"+manifest.Metadata.Version+"\n")
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

// readmeProviderTableRow returns the provider table's one six-column row for provider.
func readmeProviderTableRow(t *testing.T, readme string, provider Provider) string {
	t.Helper()
	var rows []string
	for _, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Contains(line, " | `"+string(provider)+"` | ") && strings.Count(line, "|")-strings.Count(line, "\\|") == 7 {
			rows = append(rows, line)
		}
	}
	require.Len(t, rows, 1, "the provider table has one row for %s", provider)
	return rows[0]
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
