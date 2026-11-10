// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package callstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// FormatVersion is the record schemaVersion and the FORMAT content; frozen within v1.
const FormatVersion = "dex-connectors-mcp/call-state/v1"

// MaximumIdempotencyKeyBytes is the longest idempotency key a call may carry.
const MaximumIdempotencyKeyBytes = 1024

// Status says whether a keyed call has a final result.
type Status string

const (
	// StatusRunning means an attempt may be in flight or was interrupted.
	StatusRunning Status = "running"
	// StatusCompleted means Result holds the final result, which every later
	// call with the key replays.
	StatusCompleted Status = "completed"
)

// Request identifies a key's call; it omits the tool name, which renames change.
type Request struct {
	// ConnectorID is the manifest connector ID.
	ConnectorID string `json:"connectorId"`
	// OperationID is the manifest operation ID.
	OperationID string `json:"operationId"`
	// ConnectionName is the Dex Web connection name.
	ConnectionName string `json:"connectionName"`
	// ProjectIdentity names the project configuration, such as
	// file:<realpath of connections.json>.
	ProjectIdentity string `json:"projectIdentity"`
	// ArgumentsDigest is the lowercase hexadecimal SHA-256 of the canonical
	// JSON arguments; absent or null arguments are {}.
	ArgumentsDigest string `json:"argumentsDigest"`
}

// Record is the state of one keyed write call.
type Record struct {
	// SchemaVersion equals FormatVersion.
	SchemaVersion string `json:"schemaVersion"`
	// StateKey is StateKey of the idempotency key; it also names the file.
	StateKey string `json:"stateKey"`
	// Request identifies the call.
	Request Request `json:"request"`
	// RequestDigest is Request.Digest.
	RequestDigest string `json:"requestDigest"`
	// Effect is the effect class frozen at the first attempt.
	Effect sdkgo.AgentToolEffect `json:"effect"`
	// CallID is the connector Call ID every attempt derives.
	CallID string `json:"callId"`
	// ConnectionTarget fingerprints the connection's non-secret target at the
	// first attempt; no attempt runs against another target.
	ConnectionTarget string `json:"connectionTarget"`
	// FirstAttemptAt is when attempt one started.
	FirstAttemptAt time.Time `json:"firstAttemptAt"`
	// RetryWindowEndsAt is fixed at the first attempt and never extended; no
	// attempt starts at or after it.
	RetryWindowEndsAt time.Time `json:"retryWindowEndsAt"`
	// Attempt is the last attempt started, written before that attempt runs.
	Attempt int32 `json:"attempt"`
	// Checkpoint is the last heartbeat recorded, as JSON or null, for the next attempt.
	Checkpoint json.RawMessage `json:"checkpoint"`
	// PossiblyApplied, once set, is never cleared and forbids a rejected outcome.
	PossiblyApplied bool `json:"possiblyApplied"`
	// Status says whether Result is final.
	Status Status `json:"status"`
	// Result is the MCP tools/call result returned, or null while running.
	Result json.RawMessage `json:"result"`
	// ServerVersion is the server version that last wrote the record.
	ServerVersion string `json:"serverVersion"`
	// UpdatedAt is when the record was last written; retention counts from it.
	UpdatedAt time.Time `json:"updatedAt"`
}

var connectionTargetPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// errRecordUnreadable is wrapped by KeyLock.Read for a record that exists but
// cannot be trusted: corrupt, of another format, or inconsistent.
var errRecordUnreadable = errors.New("call state record is unreadable")

// IsRecordUnreadable reports an existing but untrustworthy record, not a file error.
func IsRecordUnreadable(err error) bool {
	return errors.Is(err, errRecordUnreadable)
}

// StateKey hashes the format name, NUL, and the key; raw keys never name files.
func StateKey(idempotencyKey string) string {
	payload := make([]byte, 0, len(FormatVersion)+1+len(idempotencyKey))
	payload = append(payload, FormatVersion...)
	payload = append(payload, 0)
	payload = append(payload, idempotencyKey...)
	return canonicaljson.SHA256Hex(payload)
}

// ValidateIdempotencyKey returns an error unless key is a non-empty, valid
// UTF-8 string of at most MaximumIdempotencyKeyBytes bytes.
func ValidateIdempotencyKey(key string) error {
	if key == "" || len(key) > MaximumIdempotencyKeyBytes || !utf8.ValidString(key) {
		return fmt.Errorf("the idempotency key must be a non-empty string of at most %d bytes", MaximumIdempotencyKeyBytes)
	}
	return nil
}

// Digest returns the lowercase hexadecimal SHA-256 of the canonical JSON of
// the request object.
func (request Request) Digest() string {
	canonical, err := canonicaljson.Marshal(map[string]string{
		"argumentsDigest": request.ArgumentsDigest,
		"connectionName":  request.ConnectionName,
		"connectorId":     request.ConnectorID,
		"operationId":     request.OperationID,
		"projectIdentity": request.ProjectIdentity,
	})
	if err != nil {
		panic(fmt.Sprintf("encode call state request: %v", err))
	}
	return canonicaljson.SHA256Hex(canonical)
}

// validate returns an error naming the first inconsistency of a record.
func (record Record) validate(stateKey string) error {
	switch {
	case record.SchemaVersion != FormatVersion:
		return fmt.Errorf("schema version %q is not %q", record.SchemaVersion, FormatVersion)
	case record.StateKey != stateKey:
		return errors.New("the state key does not match the file")
	case record.RequestDigest != record.Request.Digest():
		return errors.New("the request digest does not match the request")
	case record.Effect != sdkgo.AgentToolEffectIdempotentWrite && record.Effect != sdkgo.AgentToolEffectNonIdempotentWrite:
		return fmt.Errorf("effect %q is not a write", record.Effect)
	case record.CallID == "":
		return errors.New("the call ID is empty")
	case !connectionTargetPattern.MatchString(record.ConnectionTarget):
		return errors.New("the connection target is not a sha256 digest")
	case record.Attempt < 1:
		return fmt.Errorf("attempt %d is below one", record.Attempt)
	case record.FirstAttemptAt.IsZero() || record.RetryWindowEndsAt.Before(record.FirstAttemptAt):
		return errors.New("the attempt times are inconsistent")
	case len(record.Checkpoint) > 0 && !json.Valid(record.Checkpoint):
		return errors.New("the checkpoint is not JSON")
	}
	switch record.Status {
	case StatusRunning:
		if hasJSONValue(record.Result) {
			return errors.New("a running record holds a result")
		}
	case StatusCompleted:
		if !bytes.HasPrefix(bytes.TrimSpace(record.Result), []byte("{")) || !json.Valid(record.Result) {
			return errors.New("a completed record holds no result object")
		}
	default:
		return fmt.Errorf("status %q is unknown", record.Status)
	}
	return nil
}

// hasJSONValue reports whether raw holds a value other than null.
func hasJSONValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
