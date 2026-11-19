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

var workgroupNamePattern = regexp.MustCompile(`^[a-z0-9-]{3,63}$`)

// ExecuteStatementInput submits one SQL statement to a serverless workgroup.
type ExecuteStatementInput struct {
	// WorkgroupName is the Redshift Serverless workgroup name in the configured Region.
	WorkgroupName string `json:"workgroupName"`
	// Database is the database selected in Query editor v2.
	Database string `json:"database"`
	// SQL is one statement without credentials or other secrets.
	SQL string `json:"sql"`
}

// StatementIdentity contains the ID assigned by Redshift Data API.
type StatementIdentity struct {
	// ID is the Redshift Data API UUID to describe or fetch results.
	ID string `json:"id"`
}

// DescribeStatementInput identifies a submitted statement.
type DescribeStatementInput struct {
	// StatementID is the UUID returned by ExecuteStatement.
	StatementID string `json:"statementId"`
}

// StatementStatus contains the identity and execution status of a statement.
type StatementStatus struct {
	// ID is the Redshift Data API statement UUID.
	ID string `json:"id"`
	// Status is a Redshift execution state such as FINISHED or FAILED.
	Status string `json:"status"`
	// HasResultSet reports whether a result page can be fetched.
	HasResultSet bool `json:"hasResultSet"`
}

// ListStatementsInput selects a bounded page of recent statements.
type ListStatementsInput struct {
	// MaxResults selects 1 to 100 statements; zero uses the provider page size.
	MaxResults int `json:"maxResults"`
	// NextToken resumes from a prior page; blank starts at the beginning.
	NextToken string `json:"nextToken"`
}

// StatementPage contains statement IDs and statuses with a continuation token.
type StatementPage struct {
	// Statements contains IDs and statuses without SQL text.
	Statements []StatementStatus `json:"statements"`
	// NextToken resumes the following page; blank means the list is complete.
	NextToken string `json:"nextToken"`
}

// CancelStatementInput identifies a running statement to cancel.
type CancelStatementInput struct {
	// StatementID is the UUID of the running statement.
	StatementID string `json:"statementId"`
}

// StatementCancellation reports whether Redshift accepted cancellation.
type StatementCancellation struct {
	// Canceled reports that cancellation was accepted or the statement is aborted.
	Canceled bool `json:"canceled"`
}

// ExecuteStatementOperation submits SQL using a provider idempotency token.
type ExecuteStatementOperation struct{ client *Client }

// DescribeStatementOperation reads one statement status.
type DescribeStatementOperation struct{ client *Client }

// ListStatementsOperation reads a bounded statement page.
type ListStatementsOperation struct{ client *Client }

// CancelStatementOperation requests statement cancellation.
type CancelStatementOperation struct{ client *Client }

// ExecuteStatement binds the Mutation to this client.
func (client *Client) ExecuteStatement() ExecuteStatementOperation {
	return ExecuteStatementOperation{client}
}

// DescribeStatement binds the Query to this client.
func (client *Client) DescribeStatement() DescribeStatementOperation {
	return DescribeStatementOperation{client}
}

// ListStatements binds the Query to this client.
func (client *Client) ListStatements() ListStatementsOperation {
	return ListStatementsOperation{client}
}

// CancelStatement binds the Mutation to this client.
func (client *Client) CancelStatement() CancelStatementOperation {
	return CancelStatementOperation{client}
}

// Definition returns the generated Mutation contract.
func (ExecuteStatementOperation) Definition() sdkgo.MutationDefinition {
	return ExecuteStatementDefinition
}

// Definition returns the generated Query contract.
func (DescribeStatementOperation) Definition() sdkgo.QueryDefinition {
	return DescribeStatementDefinition
}

// Definition returns the generated Query contract.
func (ListStatementsOperation) Definition() sdkgo.QueryDefinition { return ListStatementsDefinition }

// Definition returns the generated Mutation contract.
func (CancelStatementOperation) Definition() sdkgo.MutationDefinition {
	return CancelStatementDefinition
}

// IdempotencyKey returns the Dex call identity for provider deduplication.
func (ExecuteStatementOperation) IdempotencyKey(callID sdkgo.CallID, _ ExecuteStatementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the Dex call identity for cancellation.
func (CancelStatementOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelStatementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke submits one statement with an eight-hour provider deduplication token.
func (operation ExecuteStatementOperation) Invoke(call sdkgo.Call, input ExecuteStatementInput) sdkgo.MutationAttempt[StatementIdentity] {
	if !workgroupNamePattern.MatchString(input.WorkgroupName) || strings.TrimSpace(input.Database) == "" || len(input.Database) > 127 || strings.TrimSpace(input.SQL) == "" || len(input.SQL) > 200*1024 || call.IdempotencyKey == "" {
		return executeBranch(ExecuteStatementBranchDefect, sdkgo.FailureValidation, "Redshift execution input is invalid")
	}
	payload, err := json.Marshal(struct {
		ClientToken   string `json:"ClientToken"`
		WorkgroupName string `json:"WorkgroupName"`
		Database      string `json:"Database"`
		SQL           string `json:"Sql"`
		ResultFormat  string `json:"ResultFormat"`
	}{string(call.IdempotencyKey), input.WorkgroupName, input.Database, input.SQL, "JSON"})
	if err != nil {
		return executeBranch(ExecuteStatementBranchDefect, sdkgo.FailureValidation, "Redshift execution request could not be encoded")
	}
	response := operation.client.send(call, "ExecuteStatement", payload)
	if outcome, ok := classifyExecuteResponse(response); ok {
		return outcome
	}
	var wire struct {
		ID string `json:"Id"`
	}
	if json.Unmarshal(response.body, &wire) != nil || !statementIDPattern.MatchString(wire.ID) {
		return executeBranch(ExecuteStatementBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid statement ID")
	}
	return sdkgo.NewMutationBranch(ExecuteStatementBranchSubmitted, StatementIdentity{ID: wire.ID}, nil, sdkgo.Receipt{})
}

// Invoke reads the current status of a statement without returning SQL text.
func (operation DescribeStatementOperation) Invoke(call sdkgo.Call, input DescribeStatementInput) sdkgo.QueryAttempt[StatementStatus] {
	if !statementIDPattern.MatchString(input.StatementID) {
		return describeBranch(DescribeStatementBranchDefect, sdkgo.FailureValidation, "Redshift statement ID is invalid")
	}
	payload, _ := json.Marshal(struct {
		ID string `json:"Id"`
	}{input.StatementID})
	response := operation.client.send(call, "DescribeStatement", payload)
	if outcome, ok := classifyDescribeResponse(response); ok {
		return outcome
	}
	var wire struct {
		ID           string `json:"Id"`
		Status       string `json:"Status"`
		HasResultSet bool   `json:"HasResultSet"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.ID != input.StatementID || !isStatementStatus(wire.Status) {
		return describeBranch(DescribeStatementBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid statement status")
	}
	return sdkgo.NewQueryBranch(DescribeStatementBranchDescribed, StatementStatus{ID: wire.ID, Status: wire.Status, HasResultSet: wire.HasResultSet}, nil, sdkgo.Receipt{})
}

// Invoke lists one page of statement IDs and statuses without SQL text.
func (operation ListStatementsOperation) Invoke(call sdkgo.Call, input ListStatementsInput) sdkgo.QueryAttempt[StatementPage] {
	if input.MaxResults < 0 || input.MaxResults > 100 || len(input.NextToken) > 4096 || strings.ContainsAny(input.NextToken, "\x00\r\n") {
		return listBranch(ListStatementsBranchDefect, sdkgo.FailureValidation, "Redshift list input is invalid")
	}
	payload, _ := json.Marshal(struct {
		MaxResults int    `json:"MaxResults,omitempty"`
		NextToken  string `json:"NextToken,omitempty"`
	}{input.MaxResults, input.NextToken})
	response := operation.client.send(call, "ListStatements", payload)
	if outcome, ok := classifyListResponse(response); ok {
		return outcome
	}
	var wire struct {
		Statements []struct {
			ID     string `json:"Id"`
			Status string `json:"Status"`
		} `json:"Statements"`
		NextToken string `json:"NextToken"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Statements == nil || len(wire.Statements) > 100 || len(wire.NextToken) > 4096 {
		return listBranch(ListStatementsBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid statement page")
	}
	page := StatementPage{Statements: make([]StatementStatus, 0, len(wire.Statements)), NextToken: wire.NextToken}
	for _, statement := range wire.Statements {
		if !statementIDPattern.MatchString(statement.ID) || !isStatementStatus(statement.Status) {
			return listBranch(ListStatementsBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid statement identity")
		}
		page.Statements = append(page.Statements, StatementStatus{ID: statement.ID, Status: statement.Status})
	}
	return sdkgo.NewQueryBranch(ListStatementsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke requests cancellation of one running statement.
func (operation CancelStatementOperation) Invoke(call sdkgo.Call, input CancelStatementInput) sdkgo.MutationAttempt[StatementCancellation] {
	if !statementIDPattern.MatchString(input.StatementID) {
		return cancelBranch(CancelStatementBranchDefect, sdkgo.FailureValidation, "Redshift statement ID is invalid")
	}
	payload, _ := json.Marshal(struct {
		ID string `json:"Id"`
	}{input.StatementID})
	response := operation.client.send(call, "CancelStatement", payload)
	if outcome, ok := classifyCancelResponse(response); ok {
		return outcome
	}
	var wire struct {
		Status *bool `json:"Status"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Status == nil {
		return cancelBranch(CancelStatementBranchInvalidResponse, sdkgo.FailureProtocol, "Redshift returned invalid cancellation status")
	}
	if !*wire.Status {
		confirmation := operation.client.send(call, "DescribeStatement", payload)
		if confirmation.err != nil || confirmation.status != http.StatusOK {
			return sdkgo.NewMutationRetry[StatementCancellation](cancelFailure(sdkgo.FailureAvailability, "Redshift cancellation could not be confirmed"), confirmation.retryAfter)
		}
		var status struct {
			Status string `json:"Status"`
		}
		if json.Unmarshal(confirmation.body, &status) != nil || !isStatementStatus(status.Status) {
			return sdkgo.NewMutationRetry[StatementCancellation](cancelFailure(sdkgo.FailureProtocol, "Redshift cancellation status is unknown"), 0)
		}
		if status.Status == "ABORTED" {
			return sdkgo.NewMutationBranch(CancelStatementBranchCanceled, StatementCancellation{Canceled: true}, nil, sdkgo.Receipt{})
		}
		if status.Status == "FINISHED" || status.Status == "FAILED" {
			return sdkgo.NewMutationBranch(CancelStatementBranchNotCanceled, StatementCancellation{}, nil, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[StatementCancellation](cancelFailure(sdkgo.FailureAvailability, "Redshift statement is still running"), 0)
	}
	return sdkgo.NewMutationBranch(CancelStatementBranchCanceled, StatementCancellation{Canceled: true}, nil, sdkgo.Receipt{})
}

func isStatementStatus(status string) bool {
	switch status {
	case "SUBMITTED", "PICKED", "STARTED", "FINISHED", "ABORTED", "FAILED":
		return true
	}
	return false
}

func classifyExecuteResponse(response providerResponse) (sdkgo.MutationAttempt[StatementIdentity], bool) {
	if errors.Is(response.err, errCredential) {
		return executeBranch(ExecuteStatementBranchDefect, sdkgo.FailureAuthentication, "Redshift credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[StatementIdentity](executeFailure(sdkgo.FailureTransport, "Redshift request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests || isThrottled(response.body) {
		return sdkgo.NewMutationRetry[StatementIdentity](executeFailure(sdkgo.FailureRateLimit, "Redshift rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[StatementIdentity](executeFailure(sdkgo.FailureAvailability, "Redshift is temporarily unavailable"), response.retryAfter), true
	}
	if response.status != http.StatusOK {
		return executeBranch(ExecuteStatementBranchProviderRejected, sdkgo.FailureProviderRejection, "Redshift rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return executeBranch(ExecuteStatementBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Redshift response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[StatementIdentity](executeFailure(sdkgo.FailureTransport, "Redshift response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[StatementIdentity]{}, false
}

func classifyDescribeResponse(response providerResponse) (sdkgo.QueryAttempt[StatementStatus], bool) {
	if errors.Is(response.err, errCredential) {
		return describeBranch(DescribeStatementBranchDefect, sdkgo.FailureAuthentication, "Redshift credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[StatementStatus](describeFailure(sdkgo.FailureTransport, "Redshift request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests || isThrottled(response.body) {
		return sdkgo.NewQueryRetry[StatementStatus](describeFailure(sdkgo.FailureRateLimit, "Redshift rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[StatementStatus](describeFailure(sdkgo.FailureAvailability, "Redshift is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound || response.status == http.StatusBadRequest && hasErrorCode(response.body, "ResourceNotFoundException") {
		return sdkgo.NewQueryBranch(DescribeStatementBranchMissing, StatementStatus{}, nil, sdkgo.Receipt{}), true
	}
	if response.status != http.StatusOK {
		return describeBranch(DescribeStatementBranchProviderRejected, sdkgo.FailureProviderRejection, "Redshift rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return describeBranch(DescribeStatementBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Redshift response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[StatementStatus](describeFailure(sdkgo.FailureTransport, "Redshift response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[StatementStatus]{}, false
}

func classifyListResponse(response providerResponse) (sdkgo.QueryAttempt[StatementPage], bool) {
	if errors.Is(response.err, errCredential) {
		return listBranch(ListStatementsBranchDefect, sdkgo.FailureAuthentication, "Redshift credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[StatementPage](listFailure(sdkgo.FailureTransport, "Redshift request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests || isThrottled(response.body) {
		return sdkgo.NewQueryRetry[StatementPage](listFailure(sdkgo.FailureRateLimit, "Redshift rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[StatementPage](listFailure(sdkgo.FailureAvailability, "Redshift is temporarily unavailable"), response.retryAfter), true
	}
	if response.status != http.StatusOK {
		return listBranch(ListStatementsBranchProviderRejected, sdkgo.FailureProviderRejection, "Redshift rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listBranch(ListStatementsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Redshift response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[StatementPage](listFailure(sdkgo.FailureTransport, "Redshift response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[StatementPage]{}, false
}

func classifyCancelResponse(response providerResponse) (sdkgo.MutationAttempt[StatementCancellation], bool) {
	if errors.Is(response.err, errCredential) {
		return cancelBranch(CancelStatementBranchDefect, sdkgo.FailureAuthentication, "Redshift credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[StatementCancellation](cancelFailure(sdkgo.FailureTransport, "Redshift request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests || isThrottled(response.body) {
		return sdkgo.NewMutationRetry[StatementCancellation](cancelFailure(sdkgo.FailureRateLimit, "Redshift rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[StatementCancellation](cancelFailure(sdkgo.FailureAvailability, "Redshift is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound || response.status == http.StatusBadRequest && hasErrorCode(response.body, "ResourceNotFoundException") {
		return sdkgo.NewMutationBranch(CancelStatementBranchMissing, StatementCancellation{}, nil, sdkgo.Receipt{}), true
	}
	if response.status != http.StatusOK {
		return cancelBranch(CancelStatementBranchProviderRejected, sdkgo.FailureProviderRejection, "Redshift rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return cancelBranch(CancelStatementBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Redshift response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[StatementCancellation](cancelFailure(sdkgo.FailureTransport, "Redshift response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[StatementCancellation]{}, false
}

func executeFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-redshift", Operation: "executeStatement", Message: message}
}
func executeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[StatementIdentity] {
	failure := executeFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, StatementIdentity{}, &failure, sdkgo.Receipt{})
}
func describeFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-redshift", Operation: "describeStatement", Message: message}
}
func describeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[StatementStatus] {
	failure := describeFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, StatementStatus{}, &failure, sdkgo.Receipt{})
}
func listFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-redshift", Operation: "listStatements", Message: message}
}
func listBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[StatementPage] {
	failure := listFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, StatementPage{}, &failure, sdkgo.Receipt{})
}
func cancelFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-redshift", Operation: "cancelStatement", Message: message}
}
func cancelBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[StatementCancellation] {
	failure := cancelFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, StatementCancellation{}, &failure, sdkgo.Receipt{})
}
