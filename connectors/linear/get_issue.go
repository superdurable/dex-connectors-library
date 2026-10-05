// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getIssueOperationID = "getIssue"

	// getIssueDocument filters the issues list, so a missing issue is an empty list, not an undocumented error.
	getIssueDocument = `query LinearGetIssue($filter: IssueFilter!) {
  issues(filter: $filter, first: 1, includeArchived: true) { nodes { ` + issueFields + ` } }
}`
	readIssueSummaryDocument = `query LinearReadIssueSummary($filter: IssueFilter!) {
  issues(filter: $filter, first: 1, includeArchived: true) { nodes { ` + issueSummaryFields + ` } }
}`
)

// GetIssueInput names the issue to read.
type GetIssueInput struct {
	// IssueID is the issue's UUID or its identifier, such as ENG-123. An identifier finds the issue only
	// under its current team key; after a move to another team, use the UUID.
	IssueID string `json:"issueId"`
}

// GetIssueOperation implements the getIssue Query.
type GetIssueOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetIssueOperation) Definition() sdkgo.QueryDefinition { return GetIssueDefinition }

// Invoke reads one issue, including an archived one; a trashed or deleted issue is notFound.
func (operation GetIssueOperation) Invoke(call sdkgo.Call, input GetIssueInput) sdkgo.QueryAttempt[Issue] {
	locator, err := parseIssueLocator(input.IssueID, "issueId")
	if err != nil {
		return sdkgo.NewQueryBranch(GetIssueBranchDefect, Issue{}, linearFailurePointer(sdkgo.FailureValidation, getIssueOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failed := operation.client.openSession(call, getIssueOperationID)
	defer cancel()
	if failed != nil {
		return queryAttemptForExchange(*failed, Issue{}, sdkgo.Receipt{CallID: call.ID, Provider: providerName}, getIssueBranches)
	}
	result := session.exchange(graphQLRequest{
		operationName: "LinearGetIssue", document: getIssueDocument, variables: map[string]any{"filter": locator.issueFilter()},
	})
	receipt := session.receipt(result.response, locator.mutationID())
	if result.outcome != exchangeSucceeded {
		return queryAttemptForExchange(result, Issue{}, receipt, getIssueBranches)
	}
	wires, err := decodeIssueNodes(result.response.data)
	if err != nil {
		return sdkgo.NewQueryBranch(GetIssueBranchInvalidResponse, Issue{}, linearFailurePointer(sdkgo.FailureProtocol, getIssueOperationID, err.Error()), receipt)
	}
	if len(wires) == 0 {
		return sdkgo.NewQueryBranch(GetIssueBranchNotFound, Issue{}, linearFailurePointer(sdkgo.FailureNotFound, getIssueOperationID,
			"Linear has no issue with this ID that the connection can see"), receipt)
	}
	issue, err := decodeIssue(wires[0])
	if err != nil || !locator.matches(issue.IssueSummary) {
		return sdkgo.NewQueryBranch(GetIssueBranchInvalidResponse, Issue{}, linearFailurePointer(sdkgo.FailureProtocol, getIssueOperationID,
			"Linear returned a malformed issue or another issue than requested"), receipt)
	}
	receipt.ProviderObjectID = issue.ID
	return sdkgo.NewQueryBranch(GetIssueBranchFound, issue, nil, receipt)
}

var getIssueBranches = queryBranches{
	providerRejected: GetIssueBranchProviderRejected, invalidResponse: GetIssueBranchInvalidResponse, defect: GetIssueBranchDefect,
}

// issueReadBack is the outcome of reading one issue's summary after a write.
type issueReadBack struct {
	result  linearExchange
	summary IssueSummary
	isFound bool
}

// readIssueSummary reads the located issue; a malformed answer is classified as exchangeInvalid.
func readIssueSummary(session *graphQLSession, locator issueLocator) issueReadBack {
	result := session.exchange(graphQLRequest{
		operationName: "LinearReadIssueSummary", document: readIssueSummaryDocument, variables: map[string]any{"filter": locator.issueFilter()},
	})
	if result.outcome != exchangeSucceeded {
		return issueReadBack{result: result}
	}
	wires, err := decodeIssueNodes(result.response.data)
	if err != nil {
		return issueReadBack{result: linearExchange{outcome: exchangeInvalid, response: result.response,
			failure: linearFailure(sdkgo.FailureProtocol, session.operation, err.Error())}}
	}
	if len(wires) == 0 {
		return issueReadBack{result: result}
	}
	summary, err := decodeIssueSummary(wires[0])
	if err != nil || !locator.matches(summary) {
		return issueReadBack{result: linearExchange{outcome: exchangeInvalid, response: result.response,
			failure: linearFailure(sdkgo.FailureProtocol, session.operation, "Linear returned a malformed issue or another issue than requested")}}
	}
	return issueReadBack{result: result, summary: summary, isFound: true}
}

func decodeIssueNodes(data json.RawMessage) ([]issueWire, error) {
	var document struct {
		Issues *struct {
			Nodes []issueWire `json:"nodes"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Issues == nil || len(document.Issues.Nodes) > 1 {
		return nil, errors.New("Linear returned a malformed issue list")
	}
	return document.Issues.Nodes, nil
}
