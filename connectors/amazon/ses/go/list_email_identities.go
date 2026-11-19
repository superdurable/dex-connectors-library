// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const listEmailIdentitiesOperationID = "listEmailIdentities"

// ListEmailIdentitiesInput requests one page of regional identities.
type ListEmailIdentitiesInput struct {
	// NextToken continues a previous page; blank starts at the beginning.
	NextToken string `json:"nextToken,omitempty"`
	// PageSize is 1 to 1000; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
}

// EmailIdentitySummary describes one identity returned by SES.
type EmailIdentitySummary struct {
	// Name is the email address or domain.
	Name string `json:"name"`
	// IdentityType is EMAIL_ADDRESS or DOMAIN.
	IdentityType string `json:"identityType"`
	// VerificationStatus is the current SES verification state.
	VerificationStatus string `json:"verificationStatus"`
	// SendingEnabled reports whether SES permits this identity to send.
	SendingEnabled bool `json:"sendingEnabled"`
}

// EmailIdentityPage is one bounded page and its continuation token.
type EmailIdentityPage struct {
	// Identities holds at most PageSize entries.
	Identities []EmailIdentitySummary `json:"identities"`
	// NextToken continues the listing; blank means the final page.
	NextToken string `json:"nextToken,omitempty"`
}

// ListEmailIdentitiesOperation implements the regional identity listing.
type ListEmailIdentitiesOperation struct{ client *Client }

// ListEmailIdentities binds this Query to the client.
func (client *Client) ListEmailIdentities() ListEmailIdentitiesOperation {
	return ListEmailIdentitiesOperation{client}
}

// Definition returns the generated Query contract.
func (ListEmailIdentitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListEmailIdentitiesDefinition
}

// Invoke sends one signed page request and validates the returned identities.
func (operation ListEmailIdentitiesOperation) Invoke(call sdkgo.Call, input ListEmailIdentitiesInput) sdkgo.QueryAttempt[EmailIdentityPage] {
	if input.PageSize < 0 || input.PageSize > 1000 || len(input.NextToken) > 4096 || strings.ContainsAny(input.NextToken, "\r\n") {
		return listIdentitiesBranch(ListEmailIdentitiesBranchDefect, sdkgo.FailureValidation, "SES identity page size or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	body, err := json.Marshal(struct {
		NextToken string `json:"NextToken,omitempty"`
		PageSize  int    `json:"PageSize"`
	}{input.NextToken, input.PageSize})
	if err != nil {
		return listIdentitiesBranch(ListEmailIdentitiesBranchDefect, sdkgo.FailureLocalDefect, "SES identity page could not be encoded")
	}
	response := operation.client.request(call, http.MethodPost, "/v2/email/list-identities", body)
	if errors.Is(response.err, errCredential) {
		return listIdentitiesBranch(ListEmailIdentitiesBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[EmailIdentityPage](sesFailure(listEmailIdentitiesOperationID, sdkgo.FailureTransport, "SES identity listing failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewQueryRetry[EmailIdentityPage](sesFailure(listEmailIdentitiesOperationID, kind, "SES identity listing is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listIdentitiesBranch(ListEmailIdentitiesBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected identity listing HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listIdentitiesBranch(ListEmailIdentitiesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SES identity page exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[EmailIdentityPage](sesFailure(listEmailIdentitiesOperationID, sdkgo.FailureTransport, "SES identity page was interrupted"), 0)
	}
	var wire struct {
		EmailIdentities []struct {
			Name               string `json:"IdentityName"`
			IdentityType       string `json:"IdentityType"`
			VerificationStatus string `json:"VerificationStatus"`
			SendingEnabled     bool   `json:"SendingEnabled"`
		} `json:"EmailIdentities"`
		NextToken string `json:"NextToken"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.EmailIdentities == nil || len(wire.EmailIdentities) > input.PageSize || len(wire.NextToken) > 4096 {
		return listIdentitiesBranch(ListEmailIdentitiesBranchInvalidResponse, sdkgo.FailureProtocol, "SES returned invalid identity page JSON")
	}
	page := EmailIdentityPage{Identities: make([]EmailIdentitySummary, 0, len(wire.EmailIdentities)), NextToken: wire.NextToken}
	for _, identity := range wire.EmailIdentities {
		if !emailIdentityPattern.MatchString(identity.Name) || (identity.IdentityType != "EMAIL_ADDRESS" && identity.IdentityType != "DOMAIN") || !isVerificationStatus(identity.VerificationStatus) {
			return listIdentitiesBranch(ListEmailIdentitiesBranchInvalidResponse, sdkgo.FailureProtocol, "SES returned invalid identity page JSON")
		}
		page.Identities = append(page.Identities, EmailIdentitySummary{identity.Name, identity.IdentityType, identity.VerificationStatus, identity.SendingEnabled})
	}
	return sdkgo.NewQueryBranch(ListEmailIdentitiesBranchPage, page, nil, sdkgo.Receipt{})
}

func sesFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-ses", Operation: operationID, Message: message}
}

func listIdentitiesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[EmailIdentityPage] {
	failure := sesFailure(listEmailIdentitiesOperationID, kind, message)
	return sdkgo.NewQueryBranch(branch, EmailIdentityPage{}, &failure, sdkgo.Receipt{})
}
