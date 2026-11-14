// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xiaoetech

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListRootGoodsCategoriesInput has no arguments; the operation requests parent_id zero.
type ListRootGoodsCategoriesInput struct{}

// GoodsCategory is a root category without nested or private provider metadata.
type GoodsCategory struct {
	// ID is the provider's category identifier.
	ID int64 `json:"id"`
	// Title is the provider's category title.
	Title string `json:"title"`
	// HasChildren reports whether child categories exist.
	HasChildren bool `json:"hasChildren"`
}

// ListRootGoodsCategoriesOutput holds the store's root goods categories.
type ListRootGoodsCategoriesOutput struct {
	// Categories contains only top-level goods categories.
	Categories []GoodsCategory `json:"categories"`
}

// ListRootGoodsCategoriesOperation reads the store's root goods categories.
type ListRootGoodsCategoriesOperation struct{ client *Client }

// Definition returns the generated root category contract.
func (ListRootGoodsCategoriesOperation) Definition() sdkgo.QueryDefinition {
	return ListRootGoodsCategoriesDefinition
}

// Invoke obtains a cached access token and reads top-level categories in the Dex Step.
func (operation ListRootGoodsCategoriesOperation) Invoke(call sdkgo.Call, _ ListRootGoodsCategoriesInput) sdkgo.QueryAttempt[ListRootGoodsCategoriesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidSecret(credentials.AppID) || !isValidSecret(credentials.ClientID) || !isValidSecret(credentials.SecretKey.Reveal()) {
		return categoryBranch(ListRootGoodsCategoriesBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	token, tokenFailure := operation.client.getAccessToken(call.Context, credentials)
	if tokenFailure != nil {
		return categoryFailure(*tokenFailure)
	}
	payload, err := json.Marshal(struct {
		AccessToken string `json:"access_token"`
		ParentID    int    `json:"parent_id"`
	}{token, 0})
	if err != nil {
		return categoryBranch(ListRootGoodsCategoriesBranchDefect, sdkgo.FailureValidation, "category request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/xe.goods.category.list/2.0.0", bytes.NewReader(payload))
	if err != nil {
		return categoryBranch(ListRootGoodsCategoriesBranchDefect, sdkgo.FailureValidation, "category request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return categoryRetry(sdkgo.FailureAvailability, "XiaoeTech category request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return categoryRetry(sdkgo.FailureRateLimit, "XiaoeTech rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return categoryRetry(sdkgo.FailureAvailability, "XiaoeTech is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		failure := classifyReadError(err)
		return categoryFailure(*failure)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return categoryBranch(ListRootGoodsCategoriesBranchProviderRejected, kind, "XiaoeTech rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code *int `json:"code"`
		Data *[]struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			HasChildren *int   `json:"has_children"`
			ParentID    *int64 `json:"parent_id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return categoryBranch(ListRootGoodsCategoriesBranchInvalidResponse, sdkgo.FailureProtocol, "XiaoeTech returned invalid category data")
	}
	if *wire.Code != 0 {
		if *wire.Code == 2008 {
			operation.client.invalidateAccessToken()
		}
		return categoryFailure(*classifyProviderCode(*wire.Code, true))
	}
	if wire.Data == nil || len(*wire.Data) > 1000 {
		return categoryBranch(ListRootGoodsCategoriesBranchInvalidResponse, sdkgo.FailureProtocol, "XiaoeTech returned invalid category data")
	}
	categories := make([]GoodsCategory, 0, len(*wire.Data))
	for _, item := range *wire.Data {
		if item.ID <= 0 || strings.TrimSpace(item.Title) == "" || len(item.Title) > 512 || item.HasChildren == nil || (*item.HasChildren != 0 && *item.HasChildren != 1) || item.ParentID == nil || *item.ParentID != 0 {
			return categoryBranch(ListRootGoodsCategoriesBranchInvalidResponse, sdkgo.FailureProtocol, "XiaoeTech returned invalid category data")
		}
		categories = append(categories, GoodsCategory{ID: item.ID, Title: item.Title, HasChildren: *item.HasChildren == 1})
	}
	return sdkgo.NewQueryBranch(ListRootGoodsCategoriesBranchListed, ListRootGoodsCategoriesOutput{Categories: categories}, nil, sdkgo.Receipt{})
}

type providerFailure struct {
	branch  sdkgo.BranchID
	kind    sdkgo.FailureKind
	message string
	retry   bool
	delay   time.Duration
}

func classifyReadError(err error) *providerFailure {
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return &providerFailure{branch: ListRootGoodsCategoriesBranchInvalidResponse, kind: sdkgo.FailureResponseTooLarge, message: "XiaoeTech response exceeded the configured limit"}
	}
	return &providerFailure{kind: sdkgo.FailureAvailability, message: "XiaoeTech response was interrupted", retry: true}
}
func classifyProviderCode(code int, isCategory bool) *providerFailure {
	switch code {
	case 1001:
		return &providerFailure{kind: sdkgo.FailureAvailability, message: "XiaoeTech is temporarily unavailable", retry: true}
	case 1002:
		return &providerFailure{branch: ListRootGoodsCategoriesBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "XiaoeTech reported an invalid business response"}
	case 2053, 2054:
		return &providerFailure{kind: sdkgo.FailureRateLimit, message: "XiaoeTech rate limited the request", retry: true}
	case 2008:
		if isCategory {
			return &providerFailure{kind: sdkgo.FailureAuthentication, message: "XiaoeTech access token expired; refreshing", retry: true}
		}
		return &providerFailure{branch: ListRootGoodsCategoriesBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "XiaoeTech rejected token credentials"}
	case 2001, 2002, 2003, 2004, 2005, 2006, 2020:
		return &providerFailure{branch: ListRootGoodsCategoriesBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "XiaoeTech rejected token credentials"}
	case 2017, 2051:
		return &providerFailure{branch: ListRootGoodsCategoriesBranchProviderRejected, kind: sdkgo.FailureAuthorization, message: "XiaoeTech permission or IP allowlist is missing"}
	default:
		return &providerFailure{branch: ListRootGoodsCategoriesBranchProviderRejected, kind: sdkgo.FailureProviderRejection, message: "XiaoeTech rejected the category request with code " + strconv.Itoa(code)}
	}
}
func isValidSecret(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func categoryFailure(failure providerFailure) sdkgo.QueryAttempt[ListRootGoodsCategoriesOutput] {
	if failure.retry {
		return categoryRetry(failure.kind, failure.message, failure.delay)
	}
	return categoryBranch(failure.branch, failure.kind, failure.message)
}
func categoryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListRootGoodsCategoriesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "xiaoetech", Operation: "listRootGoodsCategories", Message: message}
	return sdkgo.NewQueryBranch(branch, ListRootGoodsCategoriesOutput{}, &failure, sdkgo.Receipt{})
}
func categoryRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListRootGoodsCategoriesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "xiaoetech", Operation: "listRootGoodsCategories", Message: message}
	return sdkgo.NewQueryRetry[ListRootGoodsCategoriesOutput](failure, delay)
}
