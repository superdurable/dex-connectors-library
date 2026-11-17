// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListDealsInput selects one offset-based page of deals.
type ListDealsInput struct {
	// Offset is the zero-based number of deals to skip.
	Offset int `json:"offset,omitempty"`
	// Limit is the requested page size; zero uses the provider's default.
	Limit int `json:"limit,omitempty"`
}

// GetDealInput selects one numeric ActiveCampaign deal ID.
type GetDealInput struct {
	// DealID is the provider ID from a Deals page or API.
	DealID string `json:"dealId"`
}

// DealMetadata contains selected deal fields without embedded contacts or links.
type DealMetadata struct {
	// ID is the stable provider deal ID.
	ID string `json:"id"`
	// Title is the deal's display title.
	Title string `json:"title"`
	// ContactID is the primary contact's numeric ID, when visible.
	ContactID string `json:"contactId"`
	// StageID is the deal stage's numeric ID, when visible.
	StageID string `json:"stageId"`
	// ValueMinor is the deal value in the currency's minor unit, when visible.
	ValueMinor int64 `json:"valueMinor"`
	// Currency is the ISO currency code, when visible.
	Currency string `json:"currency"`
	// Status is 0 for open, 1 for won, and 2 for lost.
	Status int `json:"status"`
}

// DealsPage is one offset-based page of deal metadata.
type DealsPage struct {
	// Deals contains the selected deal records.
	Deals []DealMetadata `json:"deals"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested page size.
	Limit int `json:"limit"`
	// Total is the provider's matching deal count.
	Total int `json:"total"`
}

// ListDealsOperation retrieves one page of deal metadata.
type ListDealsOperation struct{ client *Client }

// GetDealOperation retrieves one deal by ID.
type GetDealOperation struct{ client *Client }

// ListDeals binds the list Query to this client.
func (client *Client) ListDeals() ListDealsOperation { return ListDealsOperation{client} }

// GetDeal binds the single-deal Query to this client.
func (client *Client) GetDeal() GetDealOperation { return GetDealOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListDealsOperation) Definition() sdkgo.QueryDefinition { return ListDealsDefinition }

// Definition returns the manifest-generated single-deal contract.
func (GetDealOperation) Definition() sdkgo.QueryDefinition { return GetDealDefinition }

// Invoke reads one page of visible deals.
func (operation ListDealsOperation) Invoke(call sdkgo.Call, input ListDealsInput) sdkgo.QueryAttempt[DealsPage] {
	branches := queryBranches{found: ListDealsBranchListed, rejected: ListDealsBranchProviderRejected, invalid: ListDealsBranchInvalidResponse, defect: ListDealsBranchDefect}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listDeals", branches.defect, DealsPage{}, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeQuery(call, operation.client, "listDeals", "/deals?"+query.Encode(), branches, func(body []byte) (DealsPage, error) { return decodeDealsPage(body, input) })
}

// Invoke reads one deal without embedded relationships.
func (operation GetDealOperation) Invoke(call sdkgo.Call, input GetDealInput) sdkgo.QueryAttempt[DealMetadata] {
	branches := queryBranches{found: GetDealBranchFound, notFound: GetDealBranchNotFound, rejected: GetDealBranchProviderRejected, invalid: GetDealBranchInvalidResponse, defect: GetDealBranchDefect}
	if !numericID.MatchString(input.DealID) {
		return queryBranch("getDeal", branches.defect, DealMetadata{}, sdkgo.FailureValidation, "deal ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getDeal", "/deals/"+input.DealID, branches, decodeDeal)
}

type dealWire struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Contact    string          `json:"contact"`
	Stage      string          `json:"stage"`
	Value      json.RawMessage `json:"value"`
	Currency   string          `json:"currency"`
	Status     json.RawMessage `json:"status"`
	IsDisabled json.RawMessage `json:"isDisabled"`
}

func decodeDealsPage(body []byte, input ListDealsInput) (DealsPage, error) {
	var wire struct {
		Deals []dealWire `json:"deals"`
		Meta  struct {
			Total json.RawMessage `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DealsPage{}, err
	}
	if wire.Deals == nil {
		return DealsPage{}, fmt.Errorf("deal collection missing")
	}
	total, err := parseDealInteger(wire.Meta.Total)
	if err != nil || total < 0 {
		return DealsPage{}, fmt.Errorf("deal count missing or invalid")
	}
	page := DealsPage{Deals: make([]DealMetadata, 0, len(wire.Deals)), Offset: input.Offset, Limit: input.Limit, Total: int(total)}
	for _, item := range wire.Deals {
		metadata, err := dealMetadata(item)
		if err != nil {
			return DealsPage{}, err
		}
		page.Deals = append(page.Deals, metadata)
	}
	return page, nil
}

func decodeDeal(body []byte) (DealMetadata, error) {
	var wire struct {
		Deal *dealWire `json:"deal"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DealMetadata{}, err
	}
	if wire.Deal == nil {
		return DealMetadata{}, fmt.Errorf("deal missing")
	}
	return dealMetadata(*wire.Deal)
}

func dealMetadata(item dealWire) (DealMetadata, error) {
	if !numericID.MatchString(item.ID) || item.Title == "" {
		return DealMetadata{}, fmt.Errorf("deal ID or title missing")
	}
	metadata := DealMetadata{ID: item.ID, Title: item.Title, ContactID: item.Contact, StageID: item.Stage, Currency: strings.ToUpper(item.Currency)}
	if len(item.Value) > 0 {
		value, err := parseDealInteger(item.Value)
		if err != nil {
			return DealMetadata{}, fmt.Errorf("deal value invalid")
		}
		metadata.ValueMinor = value
	}
	if len(item.Status) > 0 {
		status, err := parseDealInteger(item.Status)
		if err != nil || status < 0 || status > 2 {
			return DealMetadata{}, fmt.Errorf("deal status invalid")
		}
		metadata.Status = int(status)
	}
	return metadata, nil
}

func parseDealInteger(value json.RawMessage) (int64, error) {
	if len(value) == 0 || string(value) == "null" {
		return 0, fmt.Errorf("number missing")
	}
	return strconv.ParseInt(strings.Trim(string(value), `"`), 10, 64)
}
