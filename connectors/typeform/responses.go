// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// maximumResponsePageSize is the largest page_size the Responses API accepts.
	maximumResponsePageSize = 1000
	// defaultResponsePageSize is the page size Typeform uses when page_size is absent.
	defaultResponsePageSize = 25
)

// ListResponsesInput selects one page of a form's completed responses, newest submission first.
type ListResponsesInput struct {
	// FormID is the form ID, such as the formPicker unit's formId.
	FormID string `json:"formId"`
	// PageSize is 1 to 1000 responses; zero uses Typeform's default of 25.
	PageSize int `json:"pageSize,omitempty"`
	// Since keeps responses submitted at or after this time, to the second; zero has no lower bound.
	Since time.Time `json:"since,omitzero"`
	// Until keeps responses submitted at or before this time, to the second; zero has no upper bound.
	Until time.Time `json:"until,omitzero"`
	// Before keeps responses submitted before the response with this token, such as an earlier page's
	// NextBeforeToken; blank starts at the newest response.
	Before string `json:"before,omitempty"`
	// After keeps responses submitted after the response with this token, such as the newest token a Flow
	// already processed; blank has no lower cursor. Set Before or After, not both.
	After string `json:"after,omitempty"`
}

// ResponsePage is one page of a form's completed responses.
type ResponsePage struct {
	// FormID is the form the responses answer.
	FormID string `json:"formId"`
	// Responses are newest submission first.
	Responses []FormResponse `json:"responses"`
	// TotalItems is how many responses match the filters.
	TotalItems int `json:"totalItems"`
	// PageCount is how many pages of this size match the filters.
	PageCount int `json:"pageCount"`
	// NextBeforeToken is the oldest response's token when older responses remain; pass it as Before to
	// read the next page. It is blank on the last page and for a request that sets After.
	NextBeforeToken string `json:"nextBeforeToken,omitempty"`
}

// ListResponsesOperation implements the listResponses Query. Build it with Client.ListResponses.
type ListResponsesOperation struct{ client *Client }

type typeformResponseList struct {
	TotalItems *int                   `json:"total_items"`
	PageCount  *int                   `json:"page_count"`
	Items      []typeformResponseItem `json:"items"`
}

type typeformResponseItem struct {
	typeformResponseFields
	ResponseID string `json:"response_id"`
}

// Definition returns the immutable listResponses operation definition.
func (ListResponsesOperation) Definition() sdkgo.QueryDefinition {
	return ListResponsesDefinition
}

// Invoke reads GET /forms/{form_id}/responses for one page of completed responses. Typeform may omit a
// response submitted in roughly the last 30 minutes; the responseSubmitted Trigger receives those.
func (operation ListResponsesOperation) Invoke(call sdkgo.Call, input ListResponsesInput) sdkgo.QueryAttempt[ResponsePage] {
	operationID := ListResponsesDefinition.Operation.OperationID
	client := operation.client
	query, err := input.query()
	if err != nil {
		return queryAttempt[ResponsePage](client, validationOutcome(operationID, err), "")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return queryAttempt[ResponsePage](client, credentialOutcome(operationID, err), "")
	}
	response, err := client.sendTypeformRequest(call.Context, credentials, http.MethodGet, "/forms/"+input.FormID+"/responses", query, nil)
	branches := failureBranches{
		notFound: ListResponsesBranchNotFound, providerRejected: ListResponsesBranchProviderRejected,
		invalidResponse: ListResponsesBranchInvalidResponse,
	}
	if outcome, isTerminal := classifyExchange(client, operationID, response, err, branches); isTerminal {
		return queryAttempt[ResponsePage](client, outcome, input.FormID)
	}
	var decoded typeformResponseList
	if err := decodeTypeformJSON(response.body, &decoded); err != nil {
		return queryAttempt[ResponsePage](client, malformedOutcome(operationID, ListResponsesBranchInvalidResponse), input.FormID)
	}
	page, err := decoded.convert(input)
	if err != nil {
		return queryAttempt[ResponsePage](client, malformedOutcome(operationID, ListResponsesBranchInvalidResponse), input.FormID)
	}
	return sdkgo.NewQueryBranch(ListResponsesBranchListed, page, nil, client.typeformReceipt(input.FormID))
}

// query validates the input; since and until are sent in UTC to the second, as Typeform documents.
func (input ListResponsesInput) query() (url.Values, error) {
	if err := validateFormID(input.FormID); err != nil {
		return nil, err
	}
	switch {
	case input.PageSize < 0 || input.PageSize > maximumResponsePageSize:
		return nil, fmt.Errorf("pageSize must be from 1 through %d, or 0 for Typeform's default of %d", maximumResponsePageSize, defaultResponsePageSize)
	case !input.Since.IsZero() && !input.Until.IsZero() && input.Since.After(input.Until):
		return nil, fmt.Errorf("since must not be later than until")
	case input.Before != "" && input.After != "":
		return nil, fmt.Errorf("set before or after, not both")
	case input.Before != "" && !typeformIDPattern.MatchString(input.Before):
		return nil, fmt.Errorf("before must be the token of a Typeform response")
	case input.After != "" && !typeformIDPattern.MatchString(input.After):
		return nil, fmt.Errorf("after must be the token of a Typeform response")
	}
	query := url.Values{}
	if input.PageSize > 0 {
		query.Set("page_size", strconv.Itoa(input.PageSize))
	}
	if !input.Since.IsZero() {
		query.Set("since", formatTypeformTimestamp(input.Since))
	}
	if !input.Until.IsZero() {
		query.Set("until", formatTypeformTimestamp(input.Until))
	}
	if input.Before != "" {
		query.Set("before", input.Before)
	}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return query, nil
}

func (list typeformResponseList) convert(input ListResponsesInput) (ResponsePage, error) {
	if list.TotalItems == nil || list.PageCount == nil || *list.TotalItems < 0 || *list.PageCount < 0 {
		return ResponsePage{}, errTypeformResponseMalformed
	}
	page := ResponsePage{
		FormID: input.FormID, Responses: make([]FormResponse, 0, len(list.Items)),
		TotalItems: *list.TotalItems, PageCount: *list.PageCount,
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = defaultResponsePageSize
	}
	if len(list.Items) > pageSize {
		return ResponsePage{}, errTypeformResponseMalformed
	}
	for _, item := range list.Items {
		if item.Token == "" {
			item.Token = item.ResponseID
		}
		response, err := item.convert(nil)
		if err != nil {
			return ResponsePage{}, err
		}
		page.Responses = append(page.Responses, response)
	}
	if input.After == "" && len(page.Responses) == pageSize && page.PageCount > 1 {
		page.NextBeforeToken = page.Responses[len(page.Responses)-1].Token
	}
	return page, nil
}
