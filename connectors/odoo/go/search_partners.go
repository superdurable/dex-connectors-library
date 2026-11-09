// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchPartnersInput selects an Odoo ilike name pattern and result bound.
type SearchPartnersInput struct {
	// NamePattern is an Odoo ilike pattern such as %Acme%.
	NamePattern string `json:"namePattern"`
	// Limit is one through one hundred; zero uses twenty.
	Limit int `json:"limit,omitempty"`
}

// Partner is one matching company partner's public identity.
type Partner struct {
	// ID is Odoo's numeric partner ID.
	ID int64 `json:"id"`
	// Name is the company partner's display name.
	Name string `json:"name"`
}

// PartnerSearchPage contains a bounded set of company partners.
type PartnerSearchPage struct {
	// Partners are matching companies in Odoo's default search order.
	Partners []Partner `json:"partners"`
}

// SearchPartnersOperation reads company partners with a single JSON-2 call.
type SearchPartnersOperation struct{ client *Client }

// SearchPartners binds the Query to this client.
func (client *Client) SearchPartners() SearchPartnersOperation {
	return SearchPartnersOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (SearchPartnersOperation) Definition() sdkgo.QueryDefinition { return SearchPartnersDefinition }

// Invoke searches Odoo once and classifies retryable and terminal outcomes.
func (operation SearchPartnersOperation) Invoke(call sdkgo.Call, input SearchPartnersInput) sdkgo.QueryAttempt[PartnerSearchPage] {
	if !validNamePattern(input.NamePattern) || input.Limit < 0 || input.Limit > 100 {
		return partnerBranch(SearchPartnersBranchDefect, sdkgo.FailureValidation, "name pattern or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	requestBody, err := json.Marshal(struct {
		Domain [][]any  `json:"domain"`
		Fields []string `json:"fields"`
		Limit  int      `json:"limit"`
	}{Domain: [][]any{{"is_company", "=", true}, {"name", "ilike", input.NamePattern}}, Fields: []string{"id", "name"}, Limit: limit})
	if err != nil {
		return partnerBranch(SearchPartnersBranchDefect, sdkgo.FailureLocalDefect, "Odoo search request could not be encoded")
	}
	response := operation.client.search(call, requestBody)
	if errors.Is(response.err, errCredential) {
		return partnerBranch(SearchPartnersBranchDefect, sdkgo.FailureAuthentication, "Odoo API key is unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PartnerSearchPage](partnerFailure(sdkgo.FailureTransport, "Odoo request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PartnerSearchPage](partnerFailure(sdkgo.FailureRateLimit, "Odoo rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PartnerSearchPage](partnerFailure(sdkgo.FailureAvailability, "Odoo is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return partnerBranch(SearchPartnersBranchProviderRejected, sdkgo.FailureProviderRejection, rejectedStatus(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return partnerBranch(SearchPartnersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Odoo response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PartnerSearchPage](partnerFailure(sdkgo.FailureTransport, "Odoo response was interrupted"), 0)
	}
	page, err := decodePartners(response.body, limit)
	if err != nil {
		return partnerBranch(SearchPartnersBranchInvalidResponse, sdkgo.FailureProtocol, "Odoo returned invalid partner data")
	}
	return sdkgo.NewQueryBranch(SearchPartnersBranchFound, page, nil, sdkgo.Receipt{})
}

func decodePartners(body []byte, limit int) (PartnerSearchPage, error) {
	var partners []Partner
	if err := json.Unmarshal(body, &partners); err != nil {
		return PartnerSearchPage{}, err
	}
	if partners == nil || len(partners) > limit {
		return PartnerSearchPage{}, errors.New("partner list is missing or exceeds the limit")
	}
	for _, partner := range partners {
		if partner.ID <= 0 || partner.Name == "" {
			return PartnerSearchPage{}, errors.New("partner ID or name is invalid")
		}
	}
	return PartnerSearchPage{Partners: partners}, nil
}
