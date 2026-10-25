// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestScaffoldWritesDefaultsAndBusinessOutcomesAndKeepsTheRestOfTheManifest(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeMockFixtureConnector(t, directory)
	source := removeAllMocks(t, manifestPath)
	require.NoError(t, os.WriteFile(manifestPath, []byte(source), 0o600))
	var notes bytes.Buffer

	require.NoError(t, scaffoldMocks(manifestPath, &notes))

	scaffolded, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	manifest, err := load(manifestPath)
	require.NoError(t, err)
	getWidget, createWidget := manifest.Spec.Operations[0], manifest.Spec.Operations[2]
	require.Equal(t, []string{"foundDefaultAnswer", "notFoundFailure"}, mockNames(getWidget))
	require.True(t, getWidget.Mocks[0].Default)
	require.Equal(t, "{}", getWidget.Mocks[0].Output.JSON())
	require.Equal(t, "NOT_FOUND", getWidget.Mocks[1].Failure.Kind)
	require.Equal(t, "The widget does not exist.", getWidget.Mocks[1].Failure.Message)
	require.Equal(t, []string{"listedDefaultAnswer"}, mockNames(manifest.Spec.Operations[1]))
	require.Len(t, manifest.Spec.Operations[1].Mocks[0].Pages, 3)
	require.Equal(t, []string{`{"nextPage":2}`, `{"nextPage":3}`, `{"nextPage":0}`}, pageJSON(manifest.Spec.Operations[1].Mocks[0]),
		"an integer cursor gets integer placeholders")
	require.Equal(t, []string{"createdDefaultAnswer"}, mockNames(createWidget))
	require.True(t, createWidget.Mocks[0].Default)
	require.Contains(t, notes.String(), "getWidget: replace the foundDefaultAnswer default mock placeholder")
	require.Contains(t, notes.String(), "createWidget: replace the createdDefaultAnswer default mock placeholder")
	require.NoError(t, manifest.ValidateMockCoverage())

	notes.Reset()
	require.NoError(t, scaffoldMocks(manifestPath, &notes))
	rescaffolded, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.Equal(t, string(scaffolded), string(rescaffolded), "a second scaffold adds nothing")
	require.Equal(t, source, strings.Join(removeScaffoldedLines(string(scaffolded)), ""), "scaffold edits only the lines it inserts")
}

func TestScaffoldWritesPlaceholdersOfTheDeclaredCursorType(t *testing.T) {
	testCases := map[string]struct {
		source string
		pages  []string
	}{
		"string cursor": {
			source: "type WidgetPage struct {\n\tNextPage string `json:\"nextPage\"`\n}\n",
			pages:  []string{`{"nextPage":"page-2"}`, `{"nextPage":"page-3"}`, `{"nextPage":""}`},
		},
		"pointer to a string cursor": {
			source: "type WidgetPage struct {\n\tNextPage *string `json:\"nextPage,omitempty\"`\n}\n",
			pages:  []string{`{"nextPage":"page-2"}`, `{"nextPage":"page-3"}`, `{"nextPage":""}`},
		},
		"named string cursor": {
			source: "type PageToken string\n\ntype WidgetPage struct {\n\tPageToken PageToken `json:\"nextPage\"`\n}\n",
			pages:  []string{`{"nextPage":"page-2"}`, `{"nextPage":"page-3"}`, `{"nextPage":""}`},
		},
		"integer cursor": {
			source: "type WidgetPage struct {\n\tNextPage int64 `json:\"nextPage\"`\n}\n",
			pages:  []string{`{"nextPage":2}`, `{"nextPage":3}`, `{"nextPage":0}`},
		},
		"untagged cursor": {
			source: "type WidgetPage struct {\n\tNextPage string\n}\n",
			pages:  []string{`{"nextPage":"page-2"}`, `{"nextPage":"page-3"}`, `{"nextPage":""}`},
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			manifestPath := writeMockFixtureConnector(t, t.TempDir())
			writeWidgetPageSource(t, manifestPath, testCase.source)
			require.NoError(t, os.WriteFile(manifestPath, []byte(removeAllMocks(t, manifestPath)), 0o600))

			require.NoError(t, scaffoldMocks(manifestPath, &bytes.Buffer{}))

			manifest, err := load(manifestPath)
			require.NoError(t, err)
			require.Equal(t, testCase.pages, pageJSON(manifest.Spec.Operations[1].Mocks[0]))
			require.NoError(t, manifest.ValidateMockCoverage())
		})
	}
}

func TestScaffoldReportsACursorWhoseTypeItCannotRead(t *testing.T) {
	testCases := map[string]string{
		"no Go source":            "",
		"missing output type":     "type Other struct{}\n",
		"missing cursor field":    "type WidgetPage struct {\n\tNextToken string `json:\"nextToken\"`\n}\n",
		"unsupported cursor type": "type WidgetPage struct {\n\tNextPage []string `json:\"nextPage\"`\n}\n",
	}
	for name, source := range testCases {
		t.Run(name, func(t *testing.T) {
			manifestPath := writeMockFixtureConnector(t, t.TempDir())
			require.NoError(t, os.Remove(filepath.Join(filepath.Dir(manifestPath), "widget_page.go")))
			if source != "" {
				writeWidgetPageSource(t, manifestPath, source)
			}
			stripped := removeAllMocks(t, manifestPath)
			require.NoError(t, os.WriteFile(manifestPath, []byte(stripped), 0o600))

			err := scaffoldMocks(manifestPath, &bytes.Buffer{})

			require.ErrorContains(t, err, "listWidgets: cannot tell whether the nextPage field of WidgetPage is a string or a number cursor")
			current, readErr := os.ReadFile(manifestPath)
			require.NoError(t, readErr)
			require.Equal(t, stripped, string(current), "a failed scaffold leaves the manifest unchanged")
		})
	}
}

func TestScaffoldAppendsToAnExistingMocksSequence(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "mocks.yaml"))
	require.NoError(t, err)
	trimmed := strings.Replace(string(source), "        - name: invalidPage\n          branch: defect\n          description: The page number is negative.\n          failure: {kind: VALIDATION, message: The page must not be negative.}\n", "", 1)
	manifestPath := filepath.Join(t.TempDir(), "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(trimmed), 0o600))

	require.NoError(t, scaffoldMocks(manifestPath, &bytes.Buffer{}))

	manifest, err := load(manifestPath)
	require.NoError(t, err)
	require.Equal(t, []string{"threePages"}, mockNames(manifest.Spec.Operations[1]))
	require.NoError(t, manifest.ValidateMockCoverage())
}

func TestScaffoldRejectsMocksBeforeOtherOperationKeys(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "mocks.yaml"))
	require.NoError(t, err)
	moved := strings.Replace(string(source), "      pagination: {inputField: page, nextField: nextPage}\n      mocks:\n", "      mocks:\n", 1)
	moved = strings.Replace(moved, "    - name: createWidget\n", "      pagination: {inputField: page, nextField: nextPage}\n    - name: createWidget\n", 1)
	manifestPath := filepath.Join(t.TempDir(), "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(moved), 0o600))
	_, err = load(manifestPath)
	require.NoError(t, err)

	require.ErrorContains(t, scaffoldMocks(manifestPath, &bytes.Buffer{}), "operation 2: move mocks to the end of the operation before scaffolding")
}

func TestGenerateWritesAndChecksTheMockPackage(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeMockFixtureConnector(t, directory)

	require.NoError(t, generate([]string{manifestPath}))
	require.NoError(t, generate([]string{"--check", manifestPath}))
	mockSource := filepath.Join(directory, "mockfixturemock", "zz_generated_mock.go")
	require.FileExists(t, mockSource)
	require.FileExists(t, filepath.Join(directory, "mockfixturemock", "zz_generated_mock_test.go"))
	require.NoError(t, os.WriteFile(mockSource, []byte("stale"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale")

	withoutMocks := removeAllMocks(t, manifestPath)
	require.NoError(t, os.WriteFile(manifestPath, []byte(withoutMocks), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated mock package is stale because the manifest declares no mocks")
	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, mockSource)
	require.NoError(t, generate([]string{"--check", manifestPath}))
}

func TestValidateRequiresMockCoverage(t *testing.T) {
	manifestPath := writeMockFixtureConnector(t, t.TempDir())
	require.NoError(t, run([]string{"validate", manifestPath}))
	contents, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	incomplete := strings.Replace(string(contents), "          description: A widget with every field set.\n          default: true\n", "          description: A widget with every field set.\n", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(incomplete), 0o600))

	require.ErrorContains(t, run([]string{"validate", manifestPath}), "getWidget: exactly one default mock is required")
}

func TestCatalogListsMocksAndTheMockPackage(t *testing.T) {
	directory := t.TempDir()
	manifest, err := load(writeMockFixtureConnector(t, directory))
	require.NoError(t, err)
	withoutMocks := manifest
	withoutMocks.Spec.Operations = []schema.Operation{{Name: "getWidget", Kind: "query", Description: "Read one widget."}}

	encoded, err := encodeConnectorCatalog([]connectorDirectoryEntry{
		{Directory: "connectors/example/mock-fixture", ModulePath: "example.com/connectors/example/mock-fixture", Manifest: manifest},
		{Directory: "connectors/example/plain", ModulePath: "example.com/connectors/example/plain", Manifest: withoutMocks},
	})

	require.NoError(t, err)
	catalog := string(encoded)
	require.Contains(t, catalog, "      mockPackage: example.com/connectors/example/mock-fixture/mockfixturemock\n")
	require.Contains(t, catalog, "        - operation: getWidget\n          name: gear\n          branch: found\n          description: A widget with every field set.\n          default: true\n")
	require.Contains(t, catalog, "        - operation: createWidget\n          name: connectionLost\n          branch: uncertain\n")
	require.Contains(t, catalog, "        - operation: getWidget\n          name: throttled\n          description: The provider throttles the read once.\n")
	require.Equal(t, 1, strings.Count(catalog, "mockPackage:"), "a connector without mocks has no mock package")
}

func writeMockFixtureConnector(t *testing.T, directory string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "mocks.yaml"))
	require.NoError(t, err)
	manifestPath := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, source, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/mockfixture\n\ngo 1.24\n"), 0o600))
	writeWidgetPageSource(t, manifestPath, "type WidgetPage struct {\n\tNextPage int `json:\"nextPage\"`\n}\n")
	return manifestPath
}

// writeWidgetPageSource writes the hand-written Go declarations that the fixture's paginated output type needs.
func writeWidgetPageSource(t *testing.T, manifestPath, declarations string) {
	t.Helper()
	source := "package mockfixture\n\n" + declarations
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(manifestPath), "widget_page.go"), []byte(source), 0o600))
}

func pageJSON(mock schema.OperationMock) []string {
	pages := make([]string, 0, len(mock.Pages))
	for _, page := range mock.Pages {
		pages = append(pages, page.JSON())
	}
	return pages
}

func removeAllMocks(t *testing.T, manifestPath string) string {
	t.Helper()
	contents, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var kept []string
	isInMocks := false
	for _, line := range strings.SplitAfter(string(contents), "\n") {
		switch {
		case line == "      mocks:\n":
			isInMocks = true
			continue
		case isInMocks && strings.HasPrefix(line, "        "):
			continue
		}
		isInMocks = false
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

// removeScaffoldedLines drops the mocks blocks scaffold inserted into a manifest that had none.
func removeScaffoldedLines(contents string) []string {
	var kept []string
	isInMocks := false
	for _, line := range strings.SplitAfter(contents, "\n") {
		if line == "      mocks:\n" {
			isInMocks = true
			continue
		}
		if isInMocks && strings.HasPrefix(line, "        ") {
			continue
		}
		isInMocks = false
		kept = append(kept, line)
	}
	return kept
}

func mockNames(operation schema.Operation) []string {
	names := make([]string, 0, len(operation.Mocks))
	for _, mock := range operation.Mocks {
		names = append(names, mock.Name)
	}
	return names
}
