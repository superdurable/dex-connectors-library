// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const getDisputeGraphQL = "query GetDispute($id: ID!) { node(id: $id) { __typename ... on Dispute { id status replyByDate evidence { id category ... on DisputeTextEvidence { content } } } } }"
const searchDisputesGraphQL = "query SearchDisputes($input: DisputeSearchInput!, $first: Int!, $after: String) { disputes(input: $input, first: $first, after: $after) { edges { node { id status replyByDate } } pageInfo { hasNextPage endCursor } } }"

// DisputeEvidenceProfile identifies one evidence record without file contents.
type DisputeEvidenceProfile struct {
	// ID is the GraphQL evidence ID.
	ID string `json:"id"`
	// Category is the provider evidence category.
	Category string `json:"category,omitempty"`
	// Content is text evidence; file evidence does not expose content.
	Content string `json:"content,omitempty"`
}

// DisputeProfile shows a dispute's status and evidence identifiers.
type DisputeProfile struct {
	// ID is the GraphQL dispute ID.
	ID string `json:"id"`
	// Status is the current dispute status.
	Status string `json:"status"`
	// ReplyByDate is the provider deadline, when supplied.
	ReplyByDate string `json:"replyByDate,omitempty"`
	// Evidence lists attached evidence; text content may contain personal data.
	Evidence []DisputeEvidenceProfile `json:"evidence,omitempty"`
}

// GetDisputeInput identifies one GraphQL dispute.
type GetDisputeInput struct {
	// DisputeID is the GraphQL ID, not the Control Panel case number.
	DisputeID string `json:"disputeId"`
}

// SearchDisputesInput filters by exact dispute ID or status.
type SearchDisputesInput struct {
	// DisputeID is an optional exact GraphQL ID.
	DisputeID string `json:"disputeId,omitempty"`
	// Status is an optional DisputeStatus enum such as OPEN.
	Status string `json:"status,omitempty"`
	// First is 1 through 100; zero uses 50.
	First int `json:"first,omitempty"`
	// After is the previous page's end cursor.
	After string `json:"after,omitempty"`
}

// DisputesPage contains one provider search page.
type DisputesPage struct {
	// Disputes are the matching dispute profiles.
	Disputes []DisputeProfile `json:"disputes"`
	// HasNextPage indicates another page.
	HasNextPage bool `json:"hasNextPage"`
	// EndCursor is used as After on the next request.
	EndCursor string `json:"endCursor,omitempty"`
}

// GetDisputeOperation reads one dispute through the node Query.
type GetDisputeOperation struct{ client *Client }

// GetDispute binds the Query.
func (client *Client) GetDispute() GetDisputeOperation { return GetDisputeOperation{client} }

// Definition returns the generated Query contract.
func (GetDisputeOperation) Definition() sdkgo.QueryDefinition { return GetDisputeDefinition }

// Invoke reads the selected dispute and its evidence.
func (operation GetDisputeOperation) Invoke(call sdkgo.Call, input GetDisputeInput) sdkgo.QueryAttempt[DisputeProfile] {
	if !validGraphQLID(input.DisputeID) {
		return nodeBranch[DisputeProfile]("getDispute", GetDisputeBranchDefect, sdkgo.FailureValidation, "disputeId is invalid")
	}
	response := operation.client.getNode(call, getDisputeGraphQL, input.DisputeID)
	attempt := nodeQueryAttempt[DisputeProfile](response, "getDispute", "Dispute", input.DisputeID, GetDisputeBranchFound, GetDisputeBranchNotFound, GetDisputeBranchProviderRejected, GetDisputeBranchInvalidResponse, GetDisputeBranchDefect)
	return attempt
}

// SearchDisputesOperation reads one dispute search page.
type SearchDisputesOperation struct{ client *Client }

// SearchDisputes binds the Query.
func (client *Client) SearchDisputes() SearchDisputesOperation {
	return SearchDisputesOperation{client}
}

// Definition returns the generated Query contract.
func (SearchDisputesOperation) Definition() sdkgo.QueryDefinition { return SearchDisputesDefinition }

// Invoke searches by exact dispute ID or status.
func (operation SearchDisputesOperation) Invoke(call sdkgo.Call, input SearchDisputesInput) sdkgo.QueryAttempt[DisputesPage] {
	if (input.DisputeID == "" && input.Status == "") || input.DisputeID != "" && !validGraphQLID(input.DisputeID) || input.Status != "" && !validDisputeStatus(input.Status) || input.First < 0 || input.First > 100 || len(input.After) > 500 {
		return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchDefect, sdkgo.FailureValidation, "dispute search input is invalid")
	}
	first := input.First
	if first == 0 {
		first = 50
	}
	filter := map[string]any{}
	if input.DisputeID != "" {
		filter["id"] = map[string]any{"is": input.DisputeID}
	}
	if input.Status != "" {
		filter["status"] = map[string]any{"is": input.Status}
	}
	variables := map[string]any{"input": filter, "first": first}
	if input.After != "" {
		variables["after"] = input.After
	}
	response := operation.client.graphQL(call, call.Context, searchDisputesGraphQL, variables)
	if errors.Is(response.err, errCredential) {
		return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchDefect, sdkgo.FailureAuthentication, "Braintree credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return nodeRetry[DisputesPage]("searchDisputes", sdkgo.FailureTransport, "Braintree dispute search failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return nodeRetry[DisputesPage]("searchDisputes", sdkgo.FailureRateLimit, "Braintree rate limited dispute search", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return nodeRetry[DisputesPage]("searchDisputes", sdkgo.FailureAvailability, "Braintree dispute search is unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchProviderRejected, sdkgo.FailureProviderRejection, "Braintree rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Braintree dispute search response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			Disputes *struct {
				Edges []struct {
					Node DisputeProfile `json:"node"`
				} `json:"edges"`
				PageInfo *struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"disputes"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree returned invalid dispute search JSON")
	}
	if len(wire.Errors) > 0 {
		switch classifyGraphQLErrors(wire.Errors) {
		case graphQLErrorRateLimited:
			return nodeRetry[DisputesPage]("searchDisputes", sdkgo.FailureRateLimit, "Braintree reported a resource limit", response.retryAfter)
		case graphQLErrorUnavailable:
			return nodeRetry[DisputesPage]("searchDisputes", sdkgo.FailureAvailability, "Braintree reported a temporary failure", response.retryAfter)
		case graphQLErrorNotFound:
			return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchNotFound, sdkgo.FailureNotFound, "Braintree did not find the search resource")
		default:
			return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchProviderRejected, sdkgo.FailureProviderRejection, "Braintree rejected dispute search")
		}
	}
	if wire.Data == nil || wire.Data.Disputes == nil || wire.Data.Disputes.PageInfo == nil {
		return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree dispute page is missing")
	}
	page := DisputesPage{Disputes: make([]DisputeProfile, 0, len(wire.Data.Disputes.Edges)), HasNextPage: wire.Data.Disputes.PageInfo.HasNextPage, EndCursor: wire.Data.Disputes.PageInfo.EndCursor}
	for _, edge := range wire.Data.Disputes.Edges {
		if !validGraphQLID(edge.Node.ID) || !validDisputeStatus(edge.Node.Status) {
			return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree dispute result is invalid")
		}
		page.Disputes = append(page.Disputes, edge.Node)
	}
	if page.HasNextPage && page.EndCursor == "" {
		return nodeBranch[DisputesPage]("searchDisputes", SearchDisputesBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree dispute cursor is missing")
	}
	return sdkgo.NewQueryBranch(SearchDisputesBranchSearched, page, nil, sdkgo.Receipt{})
}

func validDisputeStatus(value string) bool {
	switch strings.ToUpper(value) {
	case "ACCEPTED", "AUTO_ACCEPTED", "DISPUTED", "EXPIRED", "LOST", "OPEN", "UNDER_REVIEW", "WON":
		return value == strings.ToUpper(value)
	}
	return false
}
