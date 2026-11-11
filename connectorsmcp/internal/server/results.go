// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// truncatedNoteFormat matches dex-agents CONNECTOR_RESULT_TRUNCATED_NOTE.
const truncatedNoteFormat = "[Dex] truncated, %d bytes"

// toolResult is stored exactly as returned, so a replay differs only in replayed.
type toolResult struct {
	Content           []textContent   `json:"content"`
	IsError           bool            `json:"isError,omitempty"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	Meta              map[string]any  `json:"_meta,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// resultOutcome is what a result tells Dex about the call.
type resultOutcome string

const (
	// resultSucceeded has no outcome key: Dex records succeeded.
	resultSucceeded resultOutcome = "succeeded"
	// resultRejected sets outcome rejected: nothing took effect, Dex records failed.
	resultRejected resultOutcome = "rejected"
	// resultFailed is an isError result without the outcome key: a read
	// failed, Dex records failed. Dex reserves other outcome values.
	resultFailed resultOutcome = "failed"
	// resultUnknown sets outcome unknown: Dex records outcome_unknown and never re-issues.
	resultUnknown resultOutcome = "unknown"
)

// newErrorResult returns an isError result with text and, for rejected and
// unknown, the Dex outcome key.
func newErrorResult(outcome resultOutcome, text string, branch sdkgo.BranchID) toolResult {
	meta := map[string]any{}
	switch outcome {
	case resultRejected:
		meta[metaKeyOutcome] = outcomeRejected
	case resultUnknown:
		meta[metaKeyOutcome] = outcomeUnknown
	}
	if branch != "" {
		meta[metaKeyBranch] = string(branch)
	}
	if len(meta) == 0 {
		meta = nil
	}
	return toolResult{Content: []textContent{{Type: "text", Text: text}}, IsError: true, Meta: meta}
}

func rejectedResult(text string) toolResult { return newErrorResult(resultRejected, text, "") }

func unknownResult(text string) toolResult { return newErrorResult(resultUnknown, text, "") }

// newSuccessResult returns canonical JSON text, structured when an object, truncated if long.
func newSuccessResult(value json.RawMessage, branch sdkgo.BranchID, maxResultBytes int) toolResult {
	canonical, err := canonicaljson.Canonicalize(value)
	if err != nil {
		canonical = []byte("null")
	}
	result := toolResult{Content: []textContent{{Type: "text", Text: string(canonical)}}, Meta: map[string]any{metaKeyBranch: string(branch)}}
	if len(canonical) > maxResultBytes {
		result.Content[0].Text = truncateUTF8(string(canonical), maxResultBytes) + "\n" + fmt.Sprintf(truncatedNoteFormat, len(canonical))
		result.Meta[metaKeyTruncatedBytes] = len(canonical)
		return result
	}
	if bytes.HasPrefix(canonical, []byte("{")) {
		result.StructuredContent = canonical
	}
	return result
}

// branchResult maps a classified attempt that selected a branch.
func branchResult(binding *toolBinding, attempt sdkgo.AgentToolAttempt, maxResultBytes int) toolResult {
	detail := string(attempt.Branch)
	if attempt.Failure != nil {
		detail += ": " + string(attempt.Failure.Kind) + ": " + attempt.Failure.Message
	}
	switch attempt.Outcome {
	case sdkgo.AgentToolAttemptSucceeded:
		return newSuccessResult(attempt.Value, attempt.Branch, maxResultBytes)
	case sdkgo.AgentToolAttemptRejected:
		return newErrorResult(resultRejected, binding.operationLabel()+" returned "+detail, attempt.Branch)
	case sdkgo.AgentToolAttemptFailed:
		return newErrorResult(resultFailed, binding.operationLabel()+" returned "+detail, attempt.Branch)
	default:
		return newErrorResult(resultUnknown, binding.operationLabel()+" may have taken effect ("+detail+"); do not repeat it", attempt.Branch)
	}
}

// truncateUTF8 cuts text to at most limit bytes on a character boundary.
func truncateUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// toMCP converts the result for the go-sdk, adding the replayed flag.
func (result toolResult) toMCP(isReplayed bool) *mcp.CallToolResult {
	converted := &mcp.CallToolResult{IsError: result.IsError, Content: make([]mcp.Content, 0, len(result.Content))}
	for _, content := range result.Content {
		converted.Content = append(converted.Content, &mcp.TextContent{Text: content.Text})
	}
	if len(result.StructuredContent) > 0 {
		converted.StructuredContent = result.StructuredContent
	}
	meta := mcp.Meta{}
	for key, value := range result.Meta {
		meta[key] = value
	}
	if isReplayed {
		meta[metaKeyReplayed] = true
	}
	if len(meta) > 0 {
		converted.Meta = meta
	}
	return converted
}

// encode returns the stored JSON form.
func (result toolResult) encode() json.RawMessage {
	encoded, err := json.Marshal(result)
	if err != nil {
		panic(fmt.Sprintf("encode tool result: %v", err))
	}
	return encoded
}

func decodeStoredResult(stored json.RawMessage) (toolResult, error) {
	var result toolResult
	if err := json.Unmarshal(stored, &result); err != nil {
		return toolResult{}, err
	}
	return result, nil
}

// unknownToolError answers a name the tool set does not offer when the key has no state.
func unknownToolError(name string) error {
	data, err := json.Marshal(map[string]any{"_meta": map[string]any{metaKeyOutcome: outcomeRejected}})
	if err != nil {
		panic(fmt.Sprintf("encode error data: %v", err))
	}
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q; nothing was sent", name), Data: data}
}

// interruptedError is a bare -32603, which Dex re-issues; never used for
// non_idempotent_write.
func interruptedError(message string) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: message}
}
