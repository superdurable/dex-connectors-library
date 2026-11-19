// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListOpportunitiesInput selects one page from the Lever opportunities list.
type ListOpportunitiesInput struct {
	// Offset is blank for the first page or the previous NextOffset value.
	Offset string `json:"offset,omitempty"`
	// Email filters by a contact email; blank includes all contacts.
	Email string `json:"email,omitempty"`
	// StageID filters by the current stage UID; blank includes all stages.
	StageID string `json:"stageId,omitempty"`
	// Limit caps a page at 100; zero uses Lever's default page size.
	Limit int `json:"limit,omitempty"`
}

// OpportunitiesPage contains selected non-secret opportunity fields and a cursor.
type OpportunitiesPage struct {
	// Opportunities excludes contact details, resumes, and notes.
	Opportunities []OpportunityStage `json:"opportunities"`
	// NextOffset is blank at the final page.
	NextOffset string `json:"nextOffset"`
}

// ListOpportunitiesOperation reads one filtered opportunity page.
type ListOpportunitiesOperation struct{ client *Client }

// ListOpportunities binds the Query to this client.
func (client *Client) ListOpportunities() ListOpportunitiesOperation {
	return ListOpportunitiesOperation{client: client}
}

// Definition returns the generated Query contract.
func (ListOpportunitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListOpportunitiesDefinition
}

// Invoke requests and validates one opportunity page.
func (operation ListOpportunitiesOperation) Invoke(call sdkgo.Call, input ListOpportunitiesInput) sdkgo.QueryAttempt[OpportunitiesPage] {
	if input.Limit < 0 || input.Limit > 100 || !validCursor(input.Offset) ||
		(input.Email != "" && !validEmail(input.Email)) ||
		(input.StageID != "" && !validOpportunityID(input.StageID)) {
		return opportunitiesBranch(ListOpportunitiesBranchDefect, sdkgo.FailureValidation, "opportunity filter is invalid")
	}
	query := url.Values{}
	if input.Offset != "" {
		query.Set("offset", input.Offset)
	}
	if input.Email != "" {
		query.Set("email", input.Email)
	}
	if input.StageID != "" {
		query.Set("stage_id", input.StageID)
	}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	path := "/opportunities"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return opportunitiesBranch(ListOpportunitiesBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure(sdkgo.FailureTransport, "Lever request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure(sdkgo.FailureRateLimit, "Lever rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure(sdkgo.FailureAvailability, "Lever is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return opportunitiesBranch(ListOpportunitiesBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return opportunitiesBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lever response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure(sdkgo.FailureTransport, "Lever response was interrupted"), 0)
	}
	page, err := decodeOpportunitiesPage(response.body)
	if err != nil {
		return opportunitiesBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Lever returned invalid opportunities JSON")
	}
	return sdkgo.NewQueryBranch(ListOpportunitiesBranchListed, page, nil, sdkgo.Receipt{})
}

func validCursor(cursor string) bool {
	if len(cursor) > 2048 || !utf8.ValidString(cursor) {
		return false
	}
	for _, character := range cursor {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validEmail(email string) bool {
	if len(email) > 320 || strings.TrimSpace(email) != email {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func decodeOpportunitiesPage(body []byte) (OpportunitiesPage, error) {
	var wire struct {
		Data    []OpportunityStage `json:"data"`
		HasNext *bool              `json:"hasNext"`
		Next    string             `json:"next"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OpportunitiesPage{}, err
	}
	if wire.Data == nil || wire.HasNext == nil {
		return OpportunitiesPage{}, fmt.Errorf("opportunity page fields missing")
	}
	for _, opportunity := range wire.Data {
		if !validOpportunityID(opportunity.ID) || strings.TrimSpace(opportunity.Name) == "" || !validOpportunityID(opportunity.Stage) {
			return OpportunitiesPage{}, fmt.Errorf("opportunity identity, name, or stage missing")
		}
	}
	if *wire.HasNext && !validCursor(wire.Next) || *wire.HasNext && wire.Next == "" || !*wire.HasNext && wire.Next != "" {
		return OpportunitiesPage{}, fmt.Errorf("opportunity page cursor invalid")
	}
	return OpportunitiesPage{Opportunities: wire.Data, NextOffset: wire.Next}, nil
}

func opportunitiesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: "listOpportunities", Message: message}
}

func opportunitiesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OpportunitiesPage] {
	failure := opportunitiesFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, OpportunitiesPage{}, &failure, sdkgo.Receipt{})
}
