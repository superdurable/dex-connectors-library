// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wangdian

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const shopsPath = "/openapi/shop.php"

// ListShopsInput selects a zero-based shop page. Zero page size uses 30.
type ListShopsInput struct {
	// Page is the zero-based page number.
	Page int `json:"page"`
	// PageSize is the number of shops per page, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// ShopSummary excludes contacts, account identifiers, and provider tokens.
type ShopSummary struct {
	// ID is the provider's shop ID, preserved as text for JSON precision.
	ID string `json:"id"`
	// Number is the shop's ERP code.
	Number string `json:"number"`
	// Name is the ERP shop name.
	Name string `json:"name"`
	// PlatformID identifies the shop platform.
	PlatformID int `json:"platformID"`
	// AuthorizationState is 0 unapproved, 1 approved, 2 expired, or 3 disabled.
	AuthorizationState int `json:"authorizationState"`
}

// ListShopsOutput contains a bounded shop page and next page number.
type ListShopsOutput struct {
	// Shops are returned in provider order.
	Shops []ShopSummary `json:"shops"`
	// Total is the provider-reported number of enabled shops.
	Total int `json:"total"`
	// NextPage is the next zero-based page, or zero when terminal.
	NextPage int `json:"nextPage"`
}

// ListShopsOperation reads Wangdian ERP shop metadata.
type ListShopsOperation struct{ client *Client }

// Definition returns the generated shop-list Query contract.
func (ListShopsOperation) Definition() sdkgo.QueryDefinition { return ListShopsDefinition }

// Invoke signs a form request and returns one page of enabled shop metadata.
func (operation ListShopsOperation) Invoke(call sdkgo.Call, input ListShopsInput) sdkgo.QueryAttempt[ListShopsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.SID.Reveal()) || !isValidCredential(credentials.AppKey.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return shopBranch(ListShopsBranchDefect, sdkgo.FailureAuthentication, "Wangdian credentials are unavailable or invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 30
	}
	if input.Page < 0 || input.Page > 1000000 || pageSize < 1 || pageSize > 100 {
		return shopBranch(ListShopsBranchDefect, sdkgo.FailureValidation, "page or pageSize is outside the supported range")
	}
	params := map[string]string{"sid": credentials.SID.Reveal(), "appkey": credentials.AppKey.Reveal(), "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "page_no": strconv.Itoa(input.Page), "page_size": strconv.Itoa(pageSize)}
	signature := signShopParams(params, credentials.AppSecret.Reveal())
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	form.Set("sign", signature)
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+shopsPath, strings.NewReader(form.Encode()))
	if err != nil {
		return shopBranch(ListShopsBranchDefect, sdkgo.FailureValidation, "shop request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "Wangdian shop transport failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == 429 {
		return shopRetry(sdkgo.FailureRateLimit, "Wangdian shop request rate limited", retryAfter)
	}
	if response.StatusCode == 408 || response.StatusCode >= 500 {
		return shopRetry(sdkgo.FailureAvailability, "Wangdian shop service temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Wangdian shop response exceeded the configured limit")
	}
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "Wangdian shop response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = sdkgo.FailureAuthentication
		}
		return shopBranch(ListShopsBranchProviderRejected, kind, "Wangdian rejected shop query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code     *int `json:"code"`
		ShopList []struct {
			ID                 json.RawMessage `json:"shop_id"`
			Number             string          `json:"shop_no"`
			Name               string          `json:"shop_name"`
			PlatformID         int             `json:"platform_id"`
			AuthorizationState json.RawMessage `json:"auth_state"`
		} `json:"shoplist"`
		TotalCount *int `json:"total_count"`
	}
	if !decodeOne(body, &wire) || wire.Code == nil {
		return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, "Wangdian returned invalid shop JSON")
	}
	if *wire.Code != 0 {
		return shopProviderFailure(*wire.Code)
	}
	if wire.ShopList == nil || wire.TotalCount == nil || *wire.TotalCount < 0 || len(wire.ShopList) > pageSize {
		return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, "Wangdian returned invalid shop page")
	}
	shops := make([]ShopSummary, 0, len(wire.ShopList))
	for _, item := range wire.ShopList {
		shopID, hasID := parseIntegerText(item.ID)
		stateText, hasState := parseIntegerText(item.AuthorizationState)
		state, conversionError := strconv.Atoi(stateText)
		if !hasID || !hasState || conversionError != nil || shopID == "" || len(shopID) > 30 || item.Number == "" || len(item.Number) > 50 || item.Name == "" || len(item.Name) > 128 || item.PlatformID < 0 || state < 0 || state > 3 {
			return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, "Wangdian returned invalid shop metadata")
		}
		shops = append(shops, ShopSummary{ID: shopID, Number: item.Number, Name: item.Name, PlatformID: item.PlatformID, AuthorizationState: state})
	}
	nextPage := 0
	if (input.Page+1)*pageSize < *wire.TotalCount {
		if len(shops) == 0 || input.Page == 1000000 {
			return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, "Wangdian pagination cannot advance")
		}
		nextPage = input.Page + 1
	}
	return sdkgo.NewQueryBranch(ListShopsBranchListed, ListShopsOutput{Shops: shops, Total: *wire.TotalCount, NextPage: nextPage}, nil, sdkgo.Receipt{})
}
func signShopParams(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := params[key]
		parts = append(parts, fmt.Sprintf("%02d-%s:%04d-%s", len([]byte(key)), key, len([]byte(value)), value))
	}
	digest := md5.Sum([]byte(strings.Join(parts, ";") + secret))
	return hex.EncodeToString(digest[:])
}
func parseIntegerText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var textValue string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &textValue) != nil {
			return "", false
		}
	} else {
		textValue = string(raw)
	}
	if textValue == "" || len(textValue) > 30 {
		return "", false
	}
	for _, character := range textValue {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	return textValue, true
}
func shopProviderFailure(code int) sdkgo.QueryAttempt[ListShopsOutput] {
	switch code {
	case 1012, 1013:
		return shopRetry(sdkgo.FailureRateLimit, "Wangdian shop query rate limited", 0)
	case 1020, 1030, 1040, 1060, 1200, 1510, 1520, 1530:
		return shopRetry(sdkgo.FailureAvailability, "Wangdian shop service temporarily unavailable", 0)
	}
	kind := sdkgo.FailureProviderRejection
	if code == 1090 || code == 1500 || code == 1600 || code == 1610 || code == 2080 || code == 2090 || code == 2100 {
		kind = sdkgo.FailureAuthentication
	}
	return shopBranch(ListShopsBranchProviderRejected, kind, "Wangdian rejected shop query with code "+strconv.Itoa(code))
}
func decodeOne(body []byte, target any) bool {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(target) != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}
func isValidCredential(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func shopBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListShopsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wangdian", Operation: "listShops", Message: message}
	return sdkgo.NewQueryBranch(branch, ListShopsOutput{}, &failure, sdkgo.Receipt{})
}
func shopRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListShopsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wangdian", Operation: "listShops", Message: message}
	return sdkgo.NewQueryRetry[ListShopsOutput](failure, delay)
}
