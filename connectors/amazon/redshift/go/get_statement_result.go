// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package redshift

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var statementIDPattern = regexp.MustCompile(`^[a-z0-9]{8}(?:-[a-z0-9]{4}){3}-[a-z0-9]{12}(?::[0-9]{1,2})?$`)

// GetStatementResultInput identifies a JSON result page by statement UUID and optional token.
type GetStatementResultInput struct {
	StatementID string `json:"statementId"`
	NextToken   string `json:"nextToken"`
}

// StatementResultPage holds one page of Redshift Field rows and a token for the next page.
type StatementResultPage struct {
	Records      []json.RawMessage `json:"records"`
	NextToken    string            `json:"nextToken"`
	TotalNumRows int64             `json:"totalNumRows"`
}

// GetStatementResultOperation fetches one cached Redshift SQL result page.
type GetStatementResultOperation struct{ client *Client }

// GetStatementResult binds the Query to this client.
func (client *Client) GetStatementResult() GetStatementResultOperation {
	return GetStatementResultOperation{client}
}

// Definition returns the generated Query contract.
func (GetStatementResultOperation) Definition() sdkgo.QueryDefinition {
	return GetStatementResultDefinition
}

// Invoke signs a result-page request and classifies its outcome.
func (operation GetStatementResultOperation) Invoke(call sdkgo.Call, input GetStatementResultInput) sdkgo.QueryAttempt[StatementResultPage] {
	if !statementIDPattern.MatchString(input.StatementID) || strings.ContainsAny(input.NextToken, "\x00\r\n") || len(input.NextToken) > 4096 {
		return statementBranch(GetStatementResultBranchDefect, sdkgo.FailureValidation, "Redshift statement ID or page token is invalid")
	}
	payload, err := json.Marshal(struct {
		ID        string `json:"Id"`
		NextToken string `json:"NextToken,omitempty"`
	}{input.StatementID, input.NextToken})
	if err != nil {
		return statementBranch(GetStatementResultBranchDefect, sdkgo.FailureValidation, "Redshift request could not be encoded")
	}
	response := operation.client.getStatementResult(call, payload)
	if errors.Is(response.err, errCredential) {
		return statementBranch(GetStatementResultBranchDefect, sdkgo.FailureAuthentication, "Redshift credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[StatementResultPage](statementFailure(sdkgo.FailureTransport, "Redshift request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status != http.StatusOK && isThrottled(response.body) {
		return sdkgo.NewQueryRetry[StatementResultPage](statementFailure(sdkgo.FailureRateLimit, "Redshift rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[StatementResultPage](statementFailure(sdkgo.FailureAvailability, "Redshift is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound || response.status == http.StatusBadRequest && hasErrorCode(response.body, "ResourceNotFoundException") {
		return sdkgo.NewQueryBranch(GetStatementResultBranchMissing, StatementResultPage{Records: []json.RawMessage{}}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return statementBranch(GetStatementResultBranchProviderRejected, sdkgo.FailureProviderRejection, "Redshift rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return statementBranch(GetStatementResultBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Redshift response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[StatementResultPage](statementFailure(sdkgo.FailureTransport, "Redshift response was interrupted"), 0)
	}
	var wire struct {
		Records      []json.RawMessage `json:"Records"`
		NextToken    string            `json:"NextToken"`
		TotalNumRows int64             `json:"TotalNumRows"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Records == nil || wire.TotalNumRows < 0 {
		return statementBranch(GetStatementResultBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid result JSON")
	}
	for _, row := range wire.Records {
		var fields []map[string]json.RawMessage
		if json.Unmarshal(row, &fields) != nil || fields == nil {
			return statementBranch(GetStatementResultBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid result JSON")
		}
		for _, field := range fields {
			if len(field) != 1 {
				return statementBranch(GetStatementResultBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid result JSON")
			}
		}
	}
	return sdkgo.NewQueryBranch(GetStatementResultBranchFetched, StatementResultPage{Records: wire.Records, NextToken: wire.NextToken, TotalNumRows: wire.TotalNumRows}, nil, sdkgo.Receipt{})
}

func hasErrorCode(body []byte, expected string) bool {
	var response struct {
		Type string `json:"__type"`
	}
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	return strings.TrimPrefix(response.Type, "com.amazonaws.redshiftdata#") == expected || strings.HasSuffix(response.Type, "#"+expected)
}
func isThrottled(body []byte) bool {
	for _, code := range []string{"ThrottlingException", "ActiveStatementsExceededException", "ActiveSessionsExceededException"} {
		if hasErrorCode(body, code) {
			return true
		}
	}
	return false
}
func statementFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-redshift", Operation: "getStatementResult", Message: message}
}
func statementBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[StatementResultPage] {
	failure := statementFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, StatementResultPage{Records: []json.RawMessage{}}, &failure, sdkgo.Receipt{})
}
