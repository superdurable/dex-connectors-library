// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestDecodeMocksManifestKeepsOutputsAsJSON(t *testing.T) {
	manifest := decodeMocksFixture(t, nil)

	getWidget := manifest.Spec.Operations[0]
	require.Equal(t, `{"id":"w-1","name":"Gear","createdAt":"2026-01-02T03:04:05Z","tags":["metal","007"]}`, getWidget.Mocks[0].Output.JSON())
	require.True(t, getWidget.Mocks[0].Default)
	require.Equal(t, "Gear", getWidget.Mocks[0].GoName())
	require.False(t, getWidget.Mocks[3].IsBranchMock())
	require.Equal(t, &schema.Pagination{InputField: "page", NextField: "nextPage"}, manifest.Spec.Operations[1].Pagination)
	require.Len(t, manifest.Spec.Operations[1].Mocks[0].Pages, 3)
	require.True(t, manifest.HasMocks())
	require.NoError(t, manifest.ValidateMockCoverage())
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"output":{"id":"w-1","name":"Gear"`)
}

func TestRejectInvalidMocks(t *testing.T) {
	testCases := map[string]struct {
		replacements [][2]string
		problem      string
	}{
		"undeclared branch": {
			replacements: [][2]string{{"branch: notFound\n", "branch: vanished\n"}},
			problem:      "getWidget: mock missing: branch must name a declared branch",
		},
		"unknown failure kind": {
			replacements: [][2]string{{"kind: NOT_FOUND", "kind: MISSING"}},
			problem:      "getWidget: mock missing: failure kind must be a Connector SDK failure kind",
		},
		"empty branch mock": {
			replacements: [][2]string{{"          failure: {kind: NOT_FOUND, message: The widget does not exist.}\n", ""}},
			problem:      "getWidget: mock missing: declares output, pages, or failure",
		},
		"duplicate name": {
			replacements: [][2]string{{"name: invalidID", "name: missing"}},
			problem:      "getWidget: mock names must be unique lower camel case values",
		},
		"name repeats a branch": {
			replacements: [][2]string{{"name: missing\n", "name: notFound\n"}},
			problem:      "getWidget: mock notFound: name must not repeat a branch goName, Retry, or Default",
		},
		"two defaults": {
			replacements: [][2]string{{"          description: The provider reports no such widget.\n", "          description: The provider reports no such widget.\n          default: true\n"}},
			problem:      "getWidget: at most one mock is the default",
		},
		"retry with a branch": {
			replacements: [][2]string{{"          retry: {", "          branch: found\n          retry: {"}},
			problem:      "getWidget: mock throttled: a retry mock declares only retry",
		},
		"negative retry delay": {
			replacements: [][2]string{{"after: 2s", "after: -2s"}},
			problem:      "getWidget: mock throttled: retry after must be a non-negative duration",
		},
		"pages without pagination": {
			replacements: [][2]string{{"      pagination: {inputField: page, nextField: nextPage}\n", ""}},
			problem:      "listWidgets: mock threePages: pages require operation pagination",
		},
		"page without a next cursor": {
			replacements: [][2]string{{"], nextPage: 2}", "]}"}},
			problem:      "listWidgets: mock threePages: page 1 must name the next page in nextPage",
		},
		"last page with a next cursor": {
			replacements: [][2]string{{"{widgets: [], nextPage: 0}", "{widgets: [], nextPage: 4}"}},
			problem:      "listWidgets: mock threePages: the last page must end the listing with an empty nextPage",
		},
		"repeated cursor": {
			replacements: [][2]string{{"nextPage: 3}", "nextPage: 2}"}},
			problem:      "listWidgets: mock threePages: page 2 repeats a nextPage cursor",
		},
		"uncertain on a branch mock": {
			replacements: [][2]string{{"          uncertain: {", "          branch: uncertain\n          output: {id: w-2}\n          uncertain: {"}},
			problem:      "createWidget: mock connectionLost: an uncertain mock declares only uncertain",
		},
		"uncertain without the branch": {
			replacements: [][2]string{{"        - {id: uncertain, goName: Uncertain, description: The create outcome is unknown., optional: true}\n", ""}},
			problem:      "createWidget: mock connectionLost: an uncertain mock requires a mutation that declares uncertain",
		},
		"failing default": {
			replacements: [][2]string{{"          default: true\n          output: {id: w-1", "          default: true\n          failure: {kind: NOT_FOUND, message: Gone.}\n          output: {id: w-1"}},
			problem:      "getWidget: mock gear: a default mock answers without a failure on a branch other than defect",
		},
		"pagination on a mutation": {
			replacements: [][2]string{{"      idempotency: required\n", "      idempotency: required\n      pagination: {inputField: page, nextField: nextPage}\n"}},
			problem:      "createWidget: only a query declares pagination",
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := schema.Decode(strings.NewReader(mocksFixture(t, testCase.replacements)))

			require.ErrorContains(t, err, testCase.problem)
		})
	}
}

func TestRejectMockValuesThatAreNotJSON(t *testing.T) {
	_, err := schema.Decode(strings.NewReader(mocksFixture(t, [][2]string{{"output: {id: w-1, name: Gear", "output: {? [a]: b, id: w-1, name: Gear"}})))

	require.ErrorContains(t, err, "mock object keys must be strings")
}

func TestValidateMockCoverage(t *testing.T) {
	testCases := map[string]struct {
		replacements [][2]string
		problem      string
	}{
		"uncovered branch": {
			replacements: [][2]string{{"        - name: invalidID\n          branch: defect\n          description: The widget ID is empty.\n          failure: {kind: VALIDATION, message: The widget ID is required.}\n", ""}},
			problem:      "getWidget: branch defect needs a mock",
		},
		"query without a default": {
			replacements: [][2]string{{"          description: A widget with every field set.\n          default: true\n", "          description: A widget with every field set.\n"}},
			problem:      "getWidget: a query needs exactly one default mock",
		},
		"paged default with two pages": {
			replacements: [][2]string{{"            - {widgets: [], nextPage: 0}\n", ""}, {"tags: [archived]}], nextPage: 3}", "tags: [archived]}], nextPage: 0}"}},
			problem:      "listWidgets: paginated default mock threePages must serve at least 3 pages",
		},
		"mutation without an uncertain mock": {
			replacements: [][2]string{{"        - name: connectionLost\n          description: The connection closed after the request was sent.\n          uncertain: {failure: {kind: TRANSPORT, message: The connection closed before the response.}}\n", ""}},
			problem:      "createWidget: branch uncertain needs a mock",
		},
		"operation without mocks": {
			replacements: [][2]string{{"        - name: invalidPage\n          branch: defect\n          description: The page number is negative.\n          failure: {kind: VALIDATION, message: The page must not be negative.}\n", ""}},
			problem:      "listWidgets: branch defect needs a mock",
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			manifest := decodeMocksFixture(t, testCase.replacements)

			require.ErrorContains(t, manifest.ValidateMockCoverage(), testCase.problem)
		})
	}
}

func TestValidateMockCoverageSkipsAConnectorWithoutMocks(t *testing.T) {
	file, err := os.Open("testdata/api-key-methods.yaml")
	require.NoError(t, err)
	defer file.Close()
	manifest, err := schema.Decode(file)
	require.NoError(t, err)

	require.False(t, manifest.HasMocks())
	require.NoError(t, manifest.ValidateMockCoverage())
}

func decodeMocksFixture(t *testing.T, replacements [][2]string) schema.Manifest {
	t.Helper()
	manifest, err := schema.Decode(strings.NewReader(mocksFixture(t, replacements)))
	require.NoError(t, err)
	return manifest
}

func mocksFixture(t *testing.T, replacements [][2]string) string {
	t.Helper()
	contents, err := os.ReadFile("testdata/mocks.yaml")
	require.NoError(t, err)
	text := string(contents)
	for _, replacement := range replacements {
		require.Contains(t, text, replacement[0])
		text = strings.Replace(text, replacement[0], replacement[1], 1)
	}
	return text
}
