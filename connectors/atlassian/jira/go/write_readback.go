// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const jiraCallPropertyKey = "dex.connector.call"

type jiraCallProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type jiraCallPropertyResponse struct {
	Value string `json:"value"`
}

type writeReadbackStatus uint8

const (
	writeReadbackMissing writeReadbackStatus = iota + 1
	writeReadbackFound
	writeReadbackRetry
)

type issueReadbackPage struct {
	Issues []issueReadbackCandidate `json:"issues"`
}

type issueReadbackCandidate struct {
	ID         string                     `json:"id"`
	Key        string                     `json:"key"`
	Properties map[string]json.RawMessage `json:"properties"`
	Fields     struct {
		Summary string `json:"summary"`
	} `json:"fields"`
}

type commentReadbackPage struct {
	Comments []commentResource `json:"comments"`
}

func jiraCallPropertyForCall(call sdkgo.Call) jiraCallProperty {
	digest := sha256.Sum256([]byte(call.ID))
	return jiraCallProperty{Key: jiraCallPropertyKey, Value: hex.EncodeToString(digest[:])}
}

func readbackExchangeStatus(result exchangeResult) writeReadbackStatus {
	if result.err != nil || result.response.hasBodyReadFailed || isRetryableReadStatus(result.response.statusCode) {
		return writeReadbackRetry
	}
	if result.response.statusCode != http.StatusOK || result.response.isBodyTooLarge || result.response.reflectsSecret {
		return writeReadbackMissing
	}
	return writeReadbackFound
}

// sendCapPolicy holds one fenced attempt's send-cap facts from its Fence decision.
type sendCapPolicy struct {
	// canResendUnknownOutcome is true while another send stays within the cap.
	canResendUnknownOutcome bool
	// hasPossibleEarlierSend is true when an earlier send of this call may have taken effect.
	hasPossibleEarlierSend bool
}

func newSendCapPolicy(call sdkgo.Call, decision singledispatch.Decision) sendCapPolicy {
	return sendCapPolicy{
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		hasPossibleEarlierSend:  decision.MaximumPriorSends > 0,
	}
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func resendOrSelectUncertain[T any](policy sendCapPolicy, value T, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[T] {
	if policy.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[T](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(value, failure, receipt)
}

// rejectOrSelectUncertain selects a refusal branch only when no earlier send may have written.
func rejectOrSelectUncertain[T any](fence *singledispatch.Fence, policy sendCapPolicy, operationID string, branch sdkgo.BranchID, value T, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[T] {
	if policy.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(value, failure, receipt)
	}
	if err := fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[T](newFailure(operationID, sdkgo.FailureAvailability, "Jira rejection checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(branch, value, &failure, receipt)
}

// defectOrSelectUncertain selects defect only when no earlier send may have written.
func defectOrSelectUncertain[T any](policy sendCapPolicy, defect sdkgo.BranchID, value T, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[T] {
	if policy.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(value, failure, receipt)
	}
	return sdkgo.NewMutationBranch(defect, value, &failure, receipt)
}

func matchesJiraCallProperty(raw json.RawMessage, expected string) bool {
	var propertyValue string
	if json.Unmarshal(raw, &propertyValue) == nil {
		return strings.EqualFold(propertyValue, expected)
	}
	var property jiraCallPropertyResponse
	return json.Unmarshal(raw, &property) == nil && strings.EqualFold(property.Value, expected)
}
