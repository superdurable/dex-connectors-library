// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bilibili

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetAuthorizedUserInput has no fields because the access token identifies the user.
type GetAuthorizedUserInput struct{}

// AuthorizedUser is the user's public identity scoped to the application.
type AuthorizedUser struct {
	// OpenID is the stable application-scoped user identifier.
	OpenID string `json:"openID"`
	// Name is the user's public nickname.
	Name string `json:"name"`
}

// GetAuthorizedUserOperation reads the authorized user's public identity.
type GetAuthorizedUserOperation struct{ client *Client }

// GetAuthorizedUser binds the Query to this client.
func (client *Client) GetAuthorizedUser() GetAuthorizedUserOperation {
	return GetAuthorizedUserOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetAuthorizedUserOperation) Definition() sdkgo.QueryDefinition {
	return GetAuthorizedUserDefinition
}

// Invoke queries Bilibili and classifies the provider outcome.
func (operation GetAuthorizedUserOperation) Invoke(call sdkgo.Call, _ GetAuthorizedUserInput) sdkgo.QueryAttempt[AuthorizedUser] {
	response := operation.client.getAuthorizedUser(call)
	if errors.Is(response.err, errCredential) {
		return userBranch(GetAuthorizedUserBranchDefect, sdkgo.FailureAuthentication, "Bilibili credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[AuthorizedUser](userFailure(sdkgo.FailureTransport, "Bilibili request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AuthorizedUser](userFailure(sdkgo.FailureRateLimit, "Bilibili rate limited the query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AuthorizedUser](userFailure(sdkgo.FailureAvailability, "Bilibili is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return userBranch(GetAuthorizedUserBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Bilibili response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AuthorizedUser](userFailure(sdkgo.FailureTransport, "Bilibili response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return userBranch(GetAuthorizedUserBranchProviderRejected, sdkgo.FailureProviderRejection, "Bilibili rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return userBranch(GetAuthorizedUserBranchInvalidResponse, sdkgo.FailureProtocol, "Bilibili returned invalid UTF-8")
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			OpenID string `json:"openid"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return userBranch(GetAuthorizedUserBranchInvalidResponse, sdkgo.FailureProtocol, "Bilibili returned invalid account JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return userBranch(GetAuthorizedUserBranchInvalidResponse, sdkgo.FailureProtocol, "Bilibili returned trailing JSON")
	}
	if wire.Code == nil {
		return userBranch(GetAuthorizedUserBranchInvalidResponse, sdkgo.FailureProtocol, "Bilibili returned no status code")
	}
	switch *wire.Code {
	case 0:
		if wire.Data == nil || wire.Data.OpenID == "" || wire.Data.Name == "" {
			return userBranch(GetAuthorizedUserBranchInvalidResponse, sdkgo.FailureProtocol, "Bilibili returned incomplete identity")
		}
		return sdkgo.NewQueryBranch(GetAuthorizedUserBranchFound, AuthorizedUser{OpenID: wire.Data.OpenID, Name: wire.Data.Name}, nil, sdkgo.Receipt{})
	case 127009, 127306:
		return sdkgo.NewQueryRetry[AuthorizedUser](userFailure(sdkgo.FailureRateLimit, "Bilibili rate limited the query"), response.retryAfter)
	case 4010, 4011, 122009, 122010, 123002, 127039:
		return sdkgo.NewQueryRetry[AuthorizedUser](userFailure(sdkgo.FailureAvailability, "Bilibili is temporarily unavailable"), response.retryAfter)
	default:
		return userBranch(GetAuthorizedUserBranchProviderRejected, sdkgo.FailureProviderRejection, "Bilibili rejected code "+strconv.Itoa(*wire.Code))
	}
}
func userFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "bilibili", Operation: "getAuthorizedUser", Message: message}
}
func userBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AuthorizedUser] {
	failure := userFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AuthorizedUser{}, &failure, sdkgo.Receipt{})
}
