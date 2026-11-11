// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

// dexConventionsFixtureSource records where testdata/mcp_dex_conventions.json
// was vendored from; refresh it from Dex main when the conventions change.
const dexConventionsFixtureSource = "superdurable/dex@18a5b6a905af42b4a297fb1efc7b11736c8654f0 (main, PR #1518) agents/conformance/mcp_dex_conventions.json"

type dexConventionsFixture struct {
	DefinitionCases []struct {
		Name                  string          `json:"name"`
		Tool                  json.RawMessage `json:"tool"`
		TrustsToolAnnotations bool            `json:"trustsToolAnnotations"`
		SessionPolicy         *dexToolPolicy  `json:"sessionPolicy"`
		Expected              dexDefinition   `json:"expected"`
	} `json:"definitionCases"`
	ResultCases []struct {
		Name     string          `json:"name"`
		Result   json.RawMessage `json:"result"`
		Expected dexRecord       `json:"expected"`
	} `json:"resultCases"`
	ErrorCases []struct {
		Name     string          `json:"name"`
		Error    json.RawMessage `json:"error"`
		Expected dexRecord       `json:"expected"`
	} `json:"errorCases"`
}

type dexToolPolicy struct {
	Effect           string `json:"effect"`
	RequiresApproval *bool  `json:"requiresApproval,omitempty"`
}

type dexDefinition struct {
	FrameworkEffect       *string       `json:"frameworkEffect"`
	DexEffect             *string       `json:"dexEffect"`
	IsApprovalRecommended bool          `json:"isApprovalRecommended"`
	Policy                dexToolPolicy `json:"policy"`
	Ordered               bool          `json:"ordered"`
}

type dexRecord struct {
	Status string  `json:"status"`
	Text   *string `json:"text"`
}

// classifyDexDefinition is a reference implementation of how Dex's
// McpToolSource reads a tool definition, written from the conventions.
func classifyDexDefinition(rawTool json.RawMessage, isTrusted bool, sessionPolicy *dexToolPolicy) dexDefinition {
	var tool struct {
		Annotations map[string]any             `json:"annotations"`
		Meta        map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(rawTool, &tool); err != nil {
		panic(err)
	}
	effectRanks := map[string]int{"read": 0, "idempotent_write": 1, "non_idempotent_write": 2, "unclassified": 3}
	defaultApproval := func(effect string) bool { return effect == "non_idempotent_write" || effect == "unclassified" }
	result := dexDefinition{}
	if !isTrusted {
		result.Policy = dexToolPolicy{Effect: "unclassified", RequiresApproval: pointerTo(true)}
		return applySessionPolicyWithoutDeclaration(result, sessionPolicy, defaultApproval)
	}
	rawEffect, hasEffectKey := tool.Meta[metaKeyEffect]
	if hasEffectKey {
		var effect string
		if json.Unmarshal(rawEffect, &effect) != nil || effectRanks[effect] == 3 || !contains([]string{"read", "idempotent_write", "non_idempotent_write"}, effect) {
			result.Policy = dexToolPolicy{Effect: "unclassified", RequiresApproval: pointerTo(true)}
			return applySessionPolicyWithoutDeclaration(result, sessionPolicy, defaultApproval)
		}
		result.FrameworkEffect = pointerTo(effect)
		result.DexEffect = pointerTo(effect)
		_, result.IsApprovalRecommended = tool.Meta[metaKeyApproval]
		policyEffect := effect
		requiresApproval := result.IsApprovalRecommended
		if sessionPolicy != nil {
			if effectRanks[sessionPolicy.Effect] > effectRanks[effect] {
				policyEffect = sessionPolicy.Effect
			}
			requiresApproval = result.IsApprovalRecommended || defaultApproval(sessionPolicy.Effect)
			if sessionPolicy.RequiresApproval != nil {
				requiresApproval = *sessionPolicy.RequiresApproval
			}
		}
		result.Policy = dexToolPolicy{Effect: policyEffect, RequiresApproval: pointerTo(requiresApproval)}
		result.Ordered = policyEffect != "read"
		return result
	}
	switch {
	case tool.Annotations["readOnlyHint"] == true:
		result.FrameworkEffect = pointerTo("read")
	case tool.Annotations["idempotentHint"] == true:
		result.FrameworkEffect = pointerTo("idempotent_write")
	}
	effect := "unclassified"
	if result.FrameworkEffect != nil {
		effect = *result.FrameworkEffect
	}
	result.Policy = dexToolPolicy{Effect: effect, RequiresApproval: pointerTo(defaultApproval(effect))}
	return applySessionPolicyWithoutDeclaration(result, sessionPolicy, defaultApproval)
}

func applySessionPolicyWithoutDeclaration(result dexDefinition, sessionPolicy *dexToolPolicy, defaultApproval func(string) bool) dexDefinition {
	if sessionPolicy == nil {
		return result
	}
	requiresApproval := defaultApproval(sessionPolicy.Effect)
	if sessionPolicy.RequiresApproval != nil {
		requiresApproval = *sessionPolicy.RequiresApproval
	}
	result.Policy = dexToolPolicy{Effect: sessionPolicy.Effect, RequiresApproval: pointerTo(requiresApproval)}
	return result
}

// classifyDexResult is a reference implementation of how McpToolSource records
// a tools/call result.
func classifyDexResult(rawResult json.RawMessage) dexRecord {
	var result struct {
		Content           []map[string]any `json:"content"`
		IsError           bool             `json:"isError"`
		StructuredContent json.RawMessage  `json:"structuredContent"`
		Meta              map[string]any   `json:"_meta"`
	}
	if err := json.Unmarshal(rawResult, &result); err != nil {
		panic(err)
	}
	var parts []string
	for _, content := range result.Content {
		if content["type"] == "text" {
			parts = append(parts, content["text"].(string))
		} else {
			parts = append(parts, renderPythonJSON(content))
		}
	}
	if len(parts) == 0 && len(result.StructuredContent) > 0 {
		var structured any
		decoder := json.NewDecoder(bytes.NewReader(result.StructuredContent))
		decoder.UseNumber()
		if err := decoder.Decode(&structured); err != nil {
			panic(err)
		}
		parts = append(parts, renderPythonJSON(structured))
	}
	text := strings.Join(parts, "\n")
	withFixedText := func(status string, fixed string) dexRecord {
		if text == "" {
			text = fixed
		}
		return dexRecord{Status: status, Text: &text}
	}
	switch result.Meta[metaKeyOutcome] {
	case outcomeUnknown:
		return withFixedText("outcome_unknown", "the MCP server could not tell whether the tool took effect")
	case outcomeRejected:
		return withFixedText("failed", "the MCP server rejected the call, so it did not take effect")
	}
	if result.IsError {
		return withFixedText("failed", "the MCP tool reported an error")
	}
	return dexRecord{Status: "succeeded", Text: &text}
}

// classifyDexError is a reference implementation of how McpToolSource records
// a JSON-RPC error answer to tools/call.
func classifyDexError(rawError json.RawMessage) dexRecord {
	var rpcError struct {
		Code    int64           `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rawError, &rpcError); err != nil {
		panic(err)
	}
	text := fmt.Sprintf("MCP error %d: %s", rpcError.Code, rpcError.Message)
	var data struct {
		Meta map[string]any `json:"_meta"`
	}
	if json.Unmarshal(rpcError.Data, &data) == nil {
		switch data.Meta[metaKeyOutcome] {
		case outcomeUnknown:
			return dexRecord{Status: "outcome_unknown", Text: &text}
		case outcomeRejected:
			return dexRecord{Status: "failed", Text: &text}
		}
	}
	if rpcError.Code == -32603 {
		return dexRecord{Status: "interrupted"}
	}
	return dexRecord{Status: "failed", Text: &text}
}

// renderPythonJSON renders like json.dumps(value, sort_keys=True) with
// integral numbers as integers, as the agent cores show structured content.
func renderPythonJSON(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, renderPythonJSON(key)+": "+renderPythonJSON(typed[key]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, 0, len(typed))
		for _, element := range typed {
			parts = append(parts, renderPythonJSON(element))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case json.Number:
		number, err := typed.Float64()
		if err == nil && number == math.Trunc(number) && math.Abs(number) < 1e15 {
			return strconv.FormatInt(int64(number), 10)
		}
		return typed.String()
	case float64:
		return renderPythonJSON(json.Number(strconv.FormatFloat(typed, 'g', -1, 64)))
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			panic(err)
		}
		return string(encoded)
	}
}

func readDexConventionsFixture(t *testing.T) dexConventionsFixture {
	t.Helper()
	contents, err := os.ReadFile("testdata/mcp_dex_conventions.json")
	require.NoError(t, err, "vendored from %s", dexConventionsFixtureSource)
	var fixture dexConventionsFixture
	require.NoError(t, json.Unmarshal(contents, &fixture))
	require.NotEmpty(t, fixture.DefinitionCases)
	require.NotEmpty(t, fixture.ResultCases)
	require.NotEmpty(t, fixture.ErrorCases)
	return fixture
}

func TestReferenceClassifierPassesEveryDexConventionsCase(t *testing.T) {
	fixture := readDexConventionsFixture(t)
	for _, testCase := range fixture.DefinitionCases {
		got := classifyDexDefinition(testCase.Tool, testCase.TrustsToolAnnotations, testCase.SessionPolicy)
		require.Equal(t, testCase.Expected, got, testCase.Name)
	}
	for _, testCase := range fixture.ResultCases {
		require.Equal(t, testCase.Expected, classifyDexResult(testCase.Result), testCase.Name)
	}
	for _, testCase := range fixture.ErrorCases {
		require.Equal(t, testCase.Expected, classifyDexError(testCase.Error), testCase.Name)
	}
}

func TestEveryListedToolDeclaresItsEffectToDex(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	expected := map[string]dexDefinition{
		"example-notes_get_note": {FrameworkEffect: pointerTo("read"), DexEffect: pointerTo("read"),
			Policy: dexToolPolicy{Effect: "read", RequiresApproval: pointerTo(false)}},
		"example-notes_post_note": {FrameworkEffect: pointerTo("non_idempotent_write"), DexEffect: pointerTo("non_idempotent_write"),
			IsApprovalRecommended: true, Policy: dexToolPolicy{Effect: "non_idempotent_write", RequiresApproval: pointerTo(true)}, Ordered: true},
		"example-notes_save_record": {FrameworkEffect: pointerTo("idempotent_write"), DexEffect: pointerTo("idempotent_write"),
			Policy: dexToolPolicy{Effect: "idempotent_write", RequiresApproval: pointerTo(false)}, Ordered: true},
	}
	tools := harness.listTools()
	require.Len(t, tools, len(expected))
	for _, tool := range tools {
		encoded, err := json.Marshal(tool)
		require.NoError(t, err)
		name := tool["name"].(string)
		require.Equal(t, expected[name], classifyDexDefinition(encoded, true, nil), name)
		require.Equal(t, expected[name].DexEffect, classifyDexDefinition(encoded, true, &dexToolPolicy{Effect: "read"}).DexEffect,
			"%s: a weaker session policy never lowers the declared effect", name)
	}
}

// TestServerAnswersRecordTheMappedDexStatus classifies the answers of the
// mapping table's rows with the reference classifier.
func TestServerAnswersRecordTheMappedDexStatus(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	rows := []struct {
		name           string
		call           func() rpcResponse
		expectedStatus string
	}{
		{"read succeeds", func() rpcResponse {
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
		}, "succeeded"},
		{"read listed rejected", func() rpcResponse {
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "missing"}, "")
		}, "failed"},
		{"read unlisted branch", func() rpcResponse {
			harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusConflict})
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
		}, "failed"},
		{"read budget spent", func() rpcResponse {
			harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
		}, "interrupted"},
		{"unknown tool", func() rpcResponse { return harness.callTool("example-notes_nothing", map[string]any{}, "flow/turn/x") }, "failed"},
		{"invalid arguments", func() rpcResponse {
			return harness.callTool("example-notes_get_note", map[string]any{"wrong": 1}, "")
		}, "failed"},
		{"write without key", func() rpcResponse {
			return harness.callTool("example-notes_post_note", map[string]any{"text": "a"}, "")
		}, "failed"},
		{"non-idempotent write succeeds", func() rpcResponse {
			return harness.callTool("example-notes_post_note", map[string]any{"text": "a"}, "flow/turn/c1")
		}, "succeeded"},
		{"replay", func() rpcResponse {
			return harness.callTool("example-notes_post_note", map[string]any{"text": "a"}, "flow/turn/c1")
		}, "succeeded"},
		{"key reused for another call", func() rpcResponse {
			return harness.callTool("example-notes_post_note", map[string]any{"text": "b"}, "flow/turn/c1")
		}, "failed"},
		{"non-idempotent retry after send", func() rpcResponse {
			harness.provider.Script("postNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
			return harness.callTool("example-notes_post_note", map[string]any{"text": "a"}, "flow/turn/c2")
		}, "outcome_unknown"},
		{"idempotent write listed rejected", func() rpcResponse {
			return harness.callTool("example-notes_save_record", map[string]any{"name": "forbidden", "value": "v"}, "flow/turn/c3")
		}, "failed"},
		{"idempotent write budget spent", func() rpcResponse {
			harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
			return harness.callTool("example-notes_save_record", map[string]any{"name": "n", "value": "v"}, "flow/turn/c4")
		}, "interrupted"},
		{"idempotent write re-issue succeeds", func() rpcResponse {
			return harness.callTool("example-notes_save_record", map[string]any{"name": "n", "value": "v"}, "flow/turn/c4")
		}, "succeeded"},
	}
	for _, row := range rows {
		response := row.call()
		var record dexRecord
		if response.Error != nil {
			encoded, err := json.Marshal(response.Error)
			require.NoError(t, err)
			record = classifyDexError(encoded)
		} else {
			record = classifyDexResult(response.Result)
		}
		require.Equal(t, row.expectedStatus, record.Status, row.name)
	}
	require.Equal(t, 1, harness.provider.MaximumAppliedRecordWrites())
}

func pointerTo[T any](value T) *T { return &value }

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
