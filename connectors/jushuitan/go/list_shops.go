// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jushuitan

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListShopsInput selects one page of enabled shops.
type ListShopsInput struct {
	// PageIndex starts at one.
	PageIndex int `json:"pageIndex"`
	// PageSize is the number of shops to request, from one through one hundred.
	PageSize int `json:"pageSize"`
}

// Shop contains non-secret shop metadata.
type Shop struct {
	// ShopID identifies the ERP shop.
	ShopID int64 `json:"shopId"`
	// Name is the shop's displayed name.
	Name string `json:"name"`
	// Site is the platform name reported by Jushuitan.
	Site string `json:"site"`
}

// ListShopsOutput contains one page of enabled ERP shops.
type ListShopsOutput struct {
	// PageIndex is the provider's one-based page number.
	PageIndex int `json:"pageIndex"`
	// PageSize is the provider's page size.
	PageSize int `json:"pageSize"`
	// HasNext indicates another page is available.
	HasNext bool `json:"hasNext"`
	// Shops contains the shops in this page.
	Shops []Shop `json:"shops"`
}

// ListShopsOperation queries the fixed HTTPS shop-list endpoint.
type ListShopsOperation struct{ client *Client }

// Definition returns the generated shop-list contract.
func (ListShopsOperation) Definition() sdkgo.QueryDefinition { return ListShopsDefinition }

// Invoke lists one page without persisting credentials, signatures, or raw provider errors.
func (operation ListShopsOperation) Invoke(call sdkgo.Call, input ListShopsInput) sdkgo.QueryAttempt[ListShopsOutput] {
	if input.PageIndex < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return shopBranch(ListShopsBranchDefect, sdkgo.FailureValidation, "pageIndex must be positive and pageSize must be between 1 and 100")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppKey.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) || !isValidCredential(credentials.AccessToken.Reveal()) {
		return shopBranch(ListShopsBranchDefect, sdkgo.FailureAuthentication, "Jushuitan credentials are unavailable or invalid")
	}
	business, err := json.Marshal(struct {
		PageIndex int `json:"page_index"`
		PageSize  int `json:"page_size"`
	}{input.PageIndex, input.PageSize})
	if err != nil {
		return shopBranch(ListShopsBranchDefect, sdkgo.FailureValidation, "shop page is invalid")
	}
	fields := url.Values{
		"access_token": {credentials.AccessToken.Reveal()},
		"app_key":      {credentials.AppKey.Reveal()},
		"biz":          {string(business)},
		"charset":      {"utf-8"},
		"timestamp":    {strconv.FormatInt(time.Now().Unix(), 10)},
		"version":      {"2"},
	}
	fields.Set("sign", signShopRequest(fields, credentials.AppSecret.Reveal()))
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, shopsEndpoint, strings.NewReader(fields.Encode()))
	if err != nil {
		return shopBranch(ListShopsBranchDefect, sdkgo.FailureValidation, "shop-list request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "Jushuitan shop-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return shopRetry(sdkgo.FailureRateLimit, "Jushuitan rate limited shop listing", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return shopRetry(sdkgo.FailureAvailability, "Jushuitan shop listing is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyShopStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Jushuitan response exceeded the configured limit")
	}
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "Jushuitan shop-list response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			PageIndex *int  `json:"page_index"`
			PageSize  *int  `json:"page_size"`
			HasNext   *bool `json:"has_next"`
			Datas     *[]struct {
				ShopID *int64 `json:"shop_id"`
				Name   string `json:"shop_name"`
				Site   string `json:"shop_site"`
			} `json:"datas"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, "Jushuitan returned invalid shop data")
	}
	if *wire.Code == 199 || *wire.Code == 200 {
		return shopRetry(sdkgo.FailureRateLimit, "Jushuitan shop listing reached its call limit", 0)
	}
	if *wire.Code == 150 {
		return shopRetry(sdkgo.FailureAvailability, "Jushuitan shop listing failed temporarily", 0)
	}
	if *wire.Code != 0 {
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 100 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.Code == 110 || *wire.Code == 190 {
			kind = sdkgo.FailureAuthorization
		}
		return shopBranch(ListShopsBranchProviderRejected, kind, "Jushuitan rejected shop listing with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.PageIndex == nil || wire.Data.PageSize == nil || wire.Data.HasNext == nil || wire.Data.Datas == nil || *wire.Data.PageIndex != input.PageIndex || *wire.Data.PageSize < 1 || *wire.Data.PageSize > 100 {
		return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, "Jushuitan returned invalid shop page metadata")
	}
	output := ListShopsOutput{PageIndex: *wire.Data.PageIndex, PageSize: *wire.Data.PageSize, HasNext: *wire.Data.HasNext, Shops: make([]Shop, 0, len(*wire.Data.Datas))}
	for _, item := range *wire.Data.Datas {
		if item.ShopID == nil || *item.ShopID <= 0 || item.Name == "" {
			return shopBranch(ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, "Jushuitan returned invalid shop metadata")
		}
		output.Shops = append(output.Shops, Shop{ShopID: *item.ShopID, Name: item.Name, Site: item.Site})
	}
	return sdkgo.NewQueryBranch(ListShopsBranchFound, output, nil, sdkgo.Receipt{})
}

func signShopRequest(fields url.Values, appSecret string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if key != "sign" && fields.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var source strings.Builder
	source.WriteString(appSecret)
	for _, key := range keys {
		source.WriteString(key)
		source.WriteString(fields.Get(key))
	}
	digest := md5.Sum([]byte(source.String()))
	return hex.EncodeToString(digest[:])
}
func classifyShopStatus(status int) sdkgo.QueryAttempt[ListShopsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return shopBranch(ListShopsBranchProviderRejected, kind, "Jushuitan rejected shop listing with HTTP "+strconv.Itoa(status))
}
func shopBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListShopsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jushuitan", Operation: "listShops", Message: message}
	return sdkgo.NewQueryBranch(branch, ListShopsOutput{}, &failure, sdkgo.Receipt{})
}
func shopRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListShopsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jushuitan", Operation: "listShops", Message: message}
	return sdkgo.NewQueryRetry[ListShopsOutput](failure, delay)
}
