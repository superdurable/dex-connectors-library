// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youzan

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

// GetShopBasicInput has no parameters; the access token identifies the authorized shop.
type GetShopBasicInput struct{}

// ShopBasic contains the authorized shop's stable identity and basic details.
type ShopBasic struct {
	// ShopID is the Youzan kdt_id.
	ShopID int64 `json:"shopID"`
	// Name is the shop's display name.
	Name string `json:"name"`
	// Logo is the shop's logo URL, when available.
	Logo string `json:"logo,omitempty"`
	// URL is the shop's homepage URL.
	URL string `json:"url,omitempty"`
	// Role identifies a single shop, head office, branch, or other organization role.
	Role int `json:"role"`
	// LineOfBusiness identifies the Youzan commerce product line.
	LineOfBusiness int `json:"lineOfBusiness"`
}

// GetShopBasicOperation reads the authorized shop.
type GetShopBasicOperation struct{ client *Client }

// GetShopBasic binds the Query to this client.
func (client *Client) GetShopBasic() GetShopBasicOperation { return GetShopBasicOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetShopBasicOperation) Definition() sdkgo.QueryDefinition { return GetShopBasicDefinition }

// Invoke queries Youzan and classifies the provider outcome.
func (operation GetShopBasicOperation) Invoke(call sdkgo.Call, _ GetShopBasicInput) sdkgo.QueryAttempt[ShopBasic] {
	response := operation.client.getShopBasic(call)
	if errors.Is(response.err, errCredential) {
		return shopBranch(GetShopBasicBranchDefect, sdkgo.FailureAuthentication, "Youzan access token is unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ShopBasic](shopFailure(sdkgo.FailureTransport, "Youzan request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ShopBasic](shopFailure(sdkgo.FailureRateLimit, "Youzan rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ShopBasic](shopFailure(sdkgo.FailureAvailability, "Youzan is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return shopBranch(GetShopBasicBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Youzan response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ShopBasic](shopFailure(sdkgo.FailureTransport, "Youzan response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return shopBranch(GetShopBasicBranchProviderRejected, sdkgo.FailureProviderRejection, "Youzan rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return shopBranch(GetShopBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Youzan returned invalid UTF-8")
	}
	var wire struct {
		Success *bool  `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			ShopID         int64  `json:"sid"`
			Name           string `json:"name"`
			Logo           string `json:"logo"`
			URL            string `json:"url"`
			Role           int    `json:"role"`
			LineOfBusiness int    `json:"line_of_business"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return shopBranch(GetShopBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Youzan returned invalid shop JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return shopBranch(GetShopBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Youzan returned trailing JSON")
	}
	if wire.Success == nil || !*wire.Success {
		if wire.Code == 161400100 {
			return sdkgo.NewQueryRetry[ShopBasic](shopFailure(sdkgo.FailureAvailability, "Youzan reported a system error"), response.retryAfter)
		}
		if wire.Success == nil {
			return shopBranch(GetShopBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Youzan omitted success status")
		}
		return shopBranch(GetShopBasicBranchProviderRejected, sdkgo.FailureProviderRejection, "Youzan rejected code "+strconv.Itoa(wire.Code))
	}
	if wire.Code != 200 || wire.Data == nil || wire.Data.ShopID <= 0 || wire.Data.Name == "" {
		return shopBranch(GetShopBasicBranchInvalidResponse, sdkgo.FailureProtocol, "Youzan returned incomplete shop information")
	}
	shop := ShopBasic{ShopID: wire.Data.ShopID, Name: wire.Data.Name, Logo: wire.Data.Logo, URL: wire.Data.URL, Role: wire.Data.Role, LineOfBusiness: wire.Data.LineOfBusiness}
	return sdkgo.NewQueryBranch(GetShopBasicBranchFound, shop, nil, sdkgo.Receipt{})
}

func shopFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "youzan", Operation: "getShopBasic", Message: message}
}
func shopBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ShopBasic] {
	failure := shopFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ShopBasic{}, &failure, sdkgo.Receipt{})
}
