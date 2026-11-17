// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAgreementsInput selects one cursor page from the caller's agreement list.
type ListAgreementsInput struct {
	// Cursor is the opaque nextCursor from a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// PageSize is the requested item count; zero lets Acrobat Sign select its default.
	PageSize int `json:"pageSize,omitempty"`
}

// AgreementPage contains only agreement identity, name, state, and pagination.
type AgreementPage struct {
	// Agreements contains the returned agreement summaries.
	Agreements []AgreementStatus `json:"agreements"`
	// NextCursor selects the next page; blank means there is no next page.
	NextCursor string `json:"nextCursor"`
}

// ListAgreementsOperation reads one page of agreements.
type ListAgreementsOperation struct{ client *Client }

// ListAgreements binds the Query to this client.
func (client *Client) ListAgreements() ListAgreementsOperation {
	return ListAgreementsOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListAgreementsOperation) Definition() sdkgo.QueryDefinition { return ListAgreementsDefinition }

// Invoke returns agreement summaries without participant or document content.
func (operation ListAgreementsOperation) Invoke(call sdkgo.Call, input ListAgreementsInput) sdkgo.QueryAttempt[AgreementPage] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.Cursor) > 2048 || strings.ContainsAny(input.Cursor, "\r\n") {
		return listAgreementsBranch(ListAgreementsBranchDefect, sdkgo.FailureValidation, "page size or cursor is invalid")
	}
	query := url.Values{}
	if input.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(input.PageSize))
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	path := "/agreements"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return listAgreementsBranch(ListAgreementsBranchDefect, sdkgo.FailureAuthentication, "Acrobat Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[AgreementPage](listAgreementsFailure(sdkgo.FailureTransport, "Acrobat Sign request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AgreementPage](listAgreementsFailure(sdkgo.FailureRateLimit, "Acrobat Sign rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AgreementPage](listAgreementsFailure(sdkgo.FailureAvailability, "Acrobat Sign is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errInvalidAccessPoint) {
		return listAgreementsBranch(ListAgreementsBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned an unsafe API access point")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listAgreementsBranch(ListAgreementsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Acrobat Sign response exceeded the configured limit")
	}
	if response.status != http.StatusOK {
		return listAgreementsBranch(ListAgreementsBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AgreementPage](listAgreementsFailure(sdkgo.FailureTransport, "Acrobat Sign response was interrupted"), 0)
	}
	page, err := decodeAgreementPage(response.body)
	if err != nil {
		return listAgreementsBranch(ListAgreementsBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned invalid agreement list JSON")
	}
	return sdkgo.NewQueryBranch(ListAgreementsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeAgreementPage(body []byte) (AgreementPage, error) {
	var wire struct {
		UserAgreementList []AgreementStatus `json:"userAgreementList"`
		Page              struct {
			NextCursor string `json:"nextCursor"`
		} `json:"page"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AgreementPage{}, err
	}
	if wire.UserAgreementList == nil {
		return AgreementPage{}, fmt.Errorf("agreement list missing")
	}
	for _, agreement := range wire.UserAgreementList {
		if !validAgreementID(agreement.ID) || strings.TrimSpace(agreement.Name) == "" {
			return AgreementPage{}, fmt.Errorf("agreement identity missing")
		}
	}
	return AgreementPage{Agreements: wire.UserAgreementList, NextCursor: wire.Page.NextCursor}, nil
}

func listAgreementsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-acrobat-sign", Operation: "listAgreements", Message: message}
}

func listAgreementsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AgreementPage] {
	failure := listAgreementsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AgreementPage{}, &failure, sdkgo.Receipt{})
}
