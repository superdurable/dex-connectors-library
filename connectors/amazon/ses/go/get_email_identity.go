// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var emailIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9._+@-]{1,254}$`)

// GetEmailIdentityInput identifies one SES email identity in the configured Region.
type GetEmailIdentityInput struct {
	EmailIdentity string `json:"emailIdentity"`
}

// EmailIdentityDetails returns the name and verification status of an SES email identity.
type EmailIdentityDetails struct {
	Name               string `json:"name"`
	VerificationStatus string `json:"verificationStatus"`
}

// GetEmailIdentityOperation reads one email identity verification status.
type GetEmailIdentityOperation struct{ client *Client }

// GetEmailIdentity binds the Query to this client.
func (client *Client) GetEmailIdentity() GetEmailIdentityOperation {
	return GetEmailIdentityOperation{client}
}

// Definition returns the generated Query contract.
func (GetEmailIdentityOperation) Definition() sdkgo.QueryDefinition {
	return GetEmailIdentityDefinition
}

// Invoke executes one signed GetEmailIdentity request and classifies its outcome.
func (operation GetEmailIdentityOperation) Invoke(call sdkgo.Call, input GetEmailIdentityInput) sdkgo.QueryAttempt[EmailIdentityDetails] {
	if !emailIdentityPattern.MatchString(input.EmailIdentity) {
		return identityBranch(GetEmailIdentityBranchDefect, sdkgo.FailureValidation, "SES email identity name is invalid")
	}
	response := operation.client.getEmailIdentity(call, input.EmailIdentity)
	if errors.Is(response.err, errCredential) {
		return identityBranch(GetEmailIdentityBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[EmailIdentityDetails](identityFailure(sdkgo.FailureTransport, "SES request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[EmailIdentityDetails](identityFailure(sdkgo.FailureRateLimit, "SES rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[EmailIdentityDetails](identityFailure(sdkgo.FailureAvailability, "SES is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetEmailIdentityBranchMissing, EmailIdentityDetails{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return identityBranch(GetEmailIdentityBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return identityBranch(GetEmailIdentityBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SES response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[EmailIdentityDetails](identityFailure(sdkgo.FailureTransport, "SES response was interrupted"), 0)
	}
	var wire struct {
		VerificationStatus string `json:"VerificationStatus"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !isVerificationStatus(wire.VerificationStatus) {
		return identityBranch(GetEmailIdentityBranchInvalidResponse, sdkgo.FailureProtocol, "SES returned invalid identity JSON")
	}
	return sdkgo.NewQueryBranch(GetEmailIdentityBranchFound, EmailIdentityDetails{Name: input.EmailIdentity, VerificationStatus: wire.VerificationStatus}, nil, sdkgo.Receipt{})
}
func identityFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-ses", Operation: "getEmailIdentity", Message: message}
}
func identityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[EmailIdentityDetails] {
	failure := identityFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, EmailIdentityDetails{}, &failure, sdkgo.Receipt{})
}

func isVerificationStatus(status string) bool {
	switch status {
	case "PENDING", "SUCCESS", "FAILED", "TEMPORARY_FAILURE", "NOT_STARTED":
		return true
	default:
		return false
	}
}
