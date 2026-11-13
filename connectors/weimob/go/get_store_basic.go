// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weimob

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

// GetStoreBasicInput has no parameters; application credentials identify the authorized store.
type GetStoreBasicInput struct{}

// StoreBasic contains the WOS store's public profile.
type StoreBasic struct {
	// StoreID is the Weimob BOS ID.
	StoreID int64 `json:"storeID"`
	// Name is the store's display name.
	Name string `json:"name"`
	// Logo is the store's logo URL, when available.
	Logo string `json:"logo,omitempty"`
	// Description is the store's introduction, when available.
	Description string `json:"description,omitempty"`
}

// GetStoreBasicOperation reads the authorized WOS store.
type GetStoreBasicOperation struct{ client *Client }

// GetStoreBasic binds the Query to this client.
func (client *Client) GetStoreBasic() GetStoreBasicOperation { return GetStoreBasicOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetStoreBasicOperation) Definition() sdkgo.QueryDefinition { return GetStoreBasicDefinition }

// Invoke queries Weimob and classifies the provider outcome.
func (operation GetStoreBasicOperation) Invoke(call sdkgo.Call, _ GetStoreBasicInput) sdkgo.QueryAttempt[StoreBasic] {
	response := operation.client.getStoreBasic(call)
	if errors.Is(response.err, errCredential) {
		return storeBranch(GetStoreBasicBranchDefect, sdkgo.FailureAuthentication, "Weimob credentials are missing or invalid")
	}
	if errors.Is(response.err, errTokenRejected) {
		return storeBranch(GetStoreBasicBranchProviderRejected, sdkgo.FailureProviderRejection, "Weimob rejected token acquisition")
	}
	if errors.Is(response.err, errTokenProtocol) {
		return storeBranch(GetStoreBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Weimob returned invalid token JSON")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[StoreBasic](storeFailure(sdkgo.FailureTransport, "Weimob request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[StoreBasic](storeFailure(sdkgo.FailureRateLimit, "Weimob rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[StoreBasic](storeFailure(sdkgo.FailureAvailability, "Weimob is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return storeBranch(GetStoreBasicBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Weimob store response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[StoreBasic](storeFailure(sdkgo.FailureTransport, "Weimob response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return storeBranch(GetStoreBasicBranchProviderRejected, sdkgo.FailureProviderRejection, "Weimob rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return storeBranch(GetStoreBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Weimob returned invalid UTF-8")
	}
	var wire struct {
		Code *struct {
			ErrorCode    string `json:"errcode"`
			ErrorMessage string `json:"errmsg"`
		} `json:"code"`
		Data *struct {
			StoreID     int64  `json:"bosId"`
			Name        string `json:"bosName"`
			Logo        string `json:"bosLogo"`
			Description string `json:"desc"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return storeBranch(GetStoreBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Weimob returned invalid store JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return storeBranch(GetStoreBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Weimob returned trailing JSON")
	}
	if wire.Code == nil || wire.Code.ErrorCode == "" {
		return storeBranch(GetStoreBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Weimob omitted status code")
	}
	if wire.Code.ErrorCode != "0" {
		return storeBranch(GetStoreBasicBranchProviderRejected, sdkgo.FailureProviderRejection, "Weimob rejected the store query")
	}
	if wire.Data == nil || wire.Data.StoreID <= 0 || wire.Data.Name == "" {
		return storeBranch(GetStoreBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Weimob returned incomplete store information")
	}
	store := StoreBasic{StoreID: wire.Data.StoreID, Name: wire.Data.Name, Logo: wire.Data.Logo, Description: wire.Data.Description}
	return sdkgo.NewQueryBranch(GetStoreBasicBranchFound, store, nil, sdkgo.Receipt{})
}

func storeFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "weimob", Operation: "getStoreBasic", Message: message}
}
func storeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[StoreBasic] {
	failure := storeFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, StoreBasic{}, &failure, sdkgo.Receipt{})
}
