// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
)

func readTextFile(t *testing.T, path string) string {
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

// TestREADMESettingsSampleIsAValidConnection decodes the sample strictly and opens a client with it.
func TestREADMESettingsSampleIsAValidConnection(t *testing.T) {
	samples := fencedBlocks(readTextFile(t, "README.md"), "json")
	require.Len(t, samples, 1)
	decoder := json.NewDecoder(strings.NewReader(samples[0]))
	decoder.DisallowUnknownFields()
	var config oauth2http.Config
	require.NoError(t, decoder.Decode(&config))

	_, err := oauth2http.New(config, staticCredentials())

	require.NoError(t, err)
}

// TestREADMEGoSnippetIsTheExampleCode keeps the README's Go snippet copied from the runnable example.
func TestREADMEGoSnippetIsTheExampleCode(t *testing.T) {
	snippets := fencedBlocks(readTextFile(t, "README.md"), "go")
	require.Len(t, snippets, 1)
	require.Contains(t, readTextFile(t, filepath.Join("examples", "api-request", "flow", "workflow.go")), snippets[0])
}

// TestREADMEDescribesEveryFieldBranchAndMock keeps the README tables equal to the manifest.
func TestREADMEDescribesEveryFieldBranchAndMock(t *testing.T) {
	readme := readTextFile(t, "README.md")
	manifest := readOAuth2Manifest(t)
	for _, field := range append(append([]manifestField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...) {
		require.Contains(t, readme, "| `"+field.Name+"` |", "the README documents %s", field.Name)
	}
	for _, branch := range []string{"completed", "rejected", "uncertain", "defect"} {
		require.Contains(t, readme, "| `"+branch+"`", "the README documents the %s branch", branch)
	}
	definition := oauth2http.SendRequestDefinition
	require.Len(t, definition.Branches, 4)
	for _, mock := range regexp.MustCompile(`(?m)^        - name: (\w+)$`).FindAllStringSubmatch(readTextFile(t, "connector.yaml"), -1) {
		require.Contains(t, readme, "`"+mock[1]+"`", "the README names the %s mock", mock[1])
	}
}

// TestRelativeLinksResolve keeps the READMEs' links to files and directories valid.
func TestRelativeLinksResolve(t *testing.T) {
	for _, readme := range []string{"README.md", filepath.Join("examples", "api-request", "README.md")} {
		for _, link := range regexp.MustCompile(`\]\(([^)#\s]+)(?:#[^)\s]*)?\)`).FindAllStringSubmatch(readTextFile(t, readme), -1) {
			if strings.Contains(link[1], "://") {
				continue
			}
			_, err := os.Stat(filepath.Join(filepath.Dir(readme), link[1]))
			require.NoError(t, err, "%s links to %s", readme, link[1])
		}
	}
}
