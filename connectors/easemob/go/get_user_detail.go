// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package easemob

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

// GetUserDetailInput selects one IM user by ID.
type GetUserDetailInput struct {
	// UserID is the registered IM user ID, with lowercase letters, digits, underscore, hyphen, or period.
	UserID string `json:"userId"`
}

// UserDetail contains selected nonsecret Easemob account fields.
type UserDetail struct {
	// UserID is the registered IM user ID.
	UserID string `json:"userId"`
	// UserUUID is Easemob's internal identifier for this user.
	UserUUID string `json:"userUuid"`
	// CreatedAtMilliseconds is account creation time in Unix milliseconds.
	CreatedAtMilliseconds int64 `json:"createdAtMilliseconds"`
	// ModifiedAtMilliseconds is the last account modification time in Unix milliseconds.
	ModifiedAtMilliseconds int64 `json:"modifiedAtMilliseconds"`
	// Activated reports whether the account can be used.
	Activated bool `json:"activated"`
	// Nickname is the optional push notification nickname.
	Nickname string `json:"nickname"`
}

// GetUserDetailOperation reads one IM user's account metadata.
type GetUserDetailOperation struct{ client *Client }

// GetUserDetail binds the Query to this client.
func (client *Client) GetUserDetail() GetUserDetailOperation { return GetUserDetailOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetUserDetailOperation) Definition() sdkgo.QueryDefinition { return GetUserDetailDefinition }

// Invoke queries Easemob and classifies the provider outcome.
func (operation GetUserDetailOperation) Invoke(call sdkgo.Call, input GetUserDetailInput) sdkgo.QueryAttempt[UserDetail] {
	response := operation.client.getUser(call, input)
	if errors.Is(response.err, errCredential) {
		return userBranch(GetUserDetailBranchDefect, sdkgo.FailureAuthentication, "Easemob credentials or user ID are unavailable or invalid")
	}
	if errors.Is(response.err, errInvalidTokenResponse) {
		return userBranch(GetUserDetailBranchInvalidResponse, sdkgo.FailureProtocol, "Easemob returned an invalid App Token response")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[UserDetail](userFailure(sdkgo.FailureTransport, "Easemob request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UserDetail](userFailure(sdkgo.FailureRateLimit, "Easemob rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[UserDetail](userFailure(sdkgo.FailureAvailability, "Easemob is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return userBranch(GetUserDetailBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Easemob response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[UserDetail](userFailure(sdkgo.FailureTransport, "Easemob response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return userBranch(GetUserDetailBranchProviderRejected, sdkgo.FailureProviderRejection, "Easemob rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return userBranch(GetUserDetailBranchInvalidResponse, sdkgo.FailureProtocol, "Easemob returned invalid UTF-8")
	}
	var wire struct {
		Entities []struct {
			UUID      string `json:"uuid"`
			Created   int64  `json:"created"`
			Modified  int64  `json:"modified"`
			Username  string `json:"username"`
			Activated *bool  `json:"activated"`
			Nickname  string `json:"nickname"`
		} `json:"entities"`
		Count *int `json:"count"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return userBranch(GetUserDetailBranchInvalidResponse, sdkgo.FailureProtocol, "Easemob returned invalid user JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return userBranch(GetUserDetailBranchInvalidResponse, sdkgo.FailureProtocol, "Easemob returned trailing JSON")
	}
	if wire.Count == nil || *wire.Count != 1 || len(wire.Entities) != 1 {
		return userBranch(GetUserDetailBranchInvalidResponse, sdkgo.FailureProtocol, "Easemob returned an invalid user count")
	}
	entity := wire.Entities[0]
	if entity.Username != input.UserID || entity.UUID == "" || entity.Created <= 0 || entity.Modified <= 0 || entity.Activated == nil {
		return userBranch(GetUserDetailBranchInvalidResponse, sdkgo.FailureProtocol, "Easemob returned incomplete user detail")
	}
	output := UserDetail{UserID: entity.Username, UserUUID: entity.UUID, CreatedAtMilliseconds: entity.Created, ModifiedAtMilliseconds: entity.Modified, Activated: *entity.Activated, Nickname: entity.Nickname}
	return sdkgo.NewQueryBranch(GetUserDetailBranchFound, output, nil, sdkgo.Receipt{})
}
func userFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "easemob", Operation: "getUserDetail", Message: message}
}
func userBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserDetail] {
	failure := userFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserDetail{}, &failure, sdkgo.Receipt{})
}
