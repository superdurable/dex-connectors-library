// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jingdigital

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const authPath = "/v1/oauth2/token"
const tagsPath = "/v1/tag/select/list"

// ListTagsInput selects a one-based page and page size. Zero values use page 1 and 20 tags.
type ListTagsInput struct {
	// Page is a one-based page number.
	Page int `json:"page"`
	// PageSize is the requested tags per page, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// TagSummary exposes only ordinary tag metadata.
type TagSummary struct {
	// ID is the provider's numeric tag ID.
	ID int64 `json:"id"`
	// Name is the marketing tag name.
	Name string `json:"name"`
	// Level is the provider's tag hierarchy level.
	Level int `json:"level"`
}

// ListTagsOutput contains a bounded page and pagination metadata.
type ListTagsOutput struct {
	// Tags are returned in provider order.
	Tags []TagSummary `json:"tags"`
	// Total is the number of matching tags reported by JINGdigital.
	Total int `json:"total"`
	// NextPage is the next one-based page or zero when terminal.
	NextPage int `json:"nextPage"`
}

// ListTagsOperation reads ordinary marketing tags.
type ListTagsOperation struct{ client *Client }

// Definition returns the generated tag-list Query contract.
func (ListTagsOperation) Definition() sdkgo.QueryDefinition { return ListTagsDefinition }

// Invoke obtains an ephemeral token and reads one bounded ordinary-tag page.
func (operation ListTagsOperation) Invoke(call sdkgo.Call, input ListTagsInput) sdkgo.QueryAttempt[ListTagsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return tagBranch(ListTagsBranchDefect, sdkgo.FailureAuthentication, "JINGdigital credentials are unavailable or invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 {
		return tagBranch(ListTagsBranchDefect, sdkgo.FailureValidation, "page or pageSize is outside the supported range")
	}
	authBody, _ := json.Marshal(struct {
		AppID     string `json:"app_id"`
		AppSecret string `json:"app_secret"`
	}{credentials.AppID.Reveal(), credentials.AppSecret.Reveal()})
	authRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+authPath, strings.NewReader(string(authBody)))
	if err != nil {
		return tagBranch(ListTagsBranchDefect, sdkgo.FailureValidation, "authorization request is invalid")
	}
	authRequest.Header.Set("Content-Type", "application/json")
	authRequest.Header.Set("Accept", "application/json")
	authResponse, err := operation.client.httpClient.Do(authRequest)
	if err != nil {
		return tagRetry(sdkgo.FailureAvailability, "JINGdigital authorization transport failed", 0)
	}
	authBytes, attempt := operation.readResponse(authResponse, "authorization")
	if attempt != nil {
		return *attempt
	}
	var authWire struct {
		Code *int `json:"code"`
		Data *struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if !decodeOne(authBytes, &authWire) || authWire.Code == nil {
		return tagBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JINGdigital returned invalid authorization JSON")
	}
	if *authWire.Code != 0 {
		return tagProviderFailure(*authWire.Code, "authorization")
	}
	if authWire.Data == nil || !isValidCredential(authWire.Data.AccessToken) {
		return tagBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JINGdigital returned invalid authorization data")
	}
	requestBody, _ := json.Marshal(struct {
		TagIDs  []int64 `json:"tag_ids"`
		Level   int     `json:"level"`
		Page    int     `json:"page"`
		PerPage int     `json:"perPage"`
	}{[]int64{}, 1, page, pageSize})
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+tagsPath, strings.NewReader(string(requestBody)))
	if err != nil {
		return tagBranch(ListTagsBranchDefect, sdkgo.FailureValidation, "tag request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("app_id", credentials.AppID.Reveal())
	request.Header.Set("access_token", authWire.Data.AccessToken)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return tagRetry(sdkgo.FailureAvailability, "JINGdigital tag transport failed", 0)
	}
	body, attempt := operation.readResponse(response, "tag")
	if attempt != nil {
		return *attempt
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Items []struct {
				ID    int64  `json:"id"`
				Name  string `json:"name"`
				Level int    `json:"level"`
			} `json:"items"`
			Meta *struct {
				TotalCount  int `json:"totalCount"`
				CurrentPage int `json:"currentPage"`
				PerPage     int `json:"perPage"`
			} `json:"_meta"`
		} `json:"data"`
	}
	if !decodeOne(body, &wire) || wire.Code == nil {
		return tagBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JINGdigital returned invalid tag JSON")
	}
	if *wire.Code != 0 {
		return tagProviderFailure(*wire.Code, "tag")
	}
	if wire.Data == nil || wire.Data.Items == nil || wire.Data.Meta == nil || wire.Data.Meta.TotalCount < 0 || wire.Data.Meta.CurrentPage != page || wire.Data.Meta.PerPage < 1 || len(wire.Data.Items) > pageSize {
		return tagBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JINGdigital returned invalid tag page")
	}
	tags := make([]TagSummary, 0, len(wire.Data.Items))
	for _, item := range wire.Data.Items {
		if item.ID <= 0 || item.Name == "" || len(item.Name) > 512 || item.Level < 0 || item.Level > 10 {
			return tagBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JINGdigital returned invalid tag metadata")
		}
		tags = append(tags, TagSummary{ID: item.ID, Name: item.Name, Level: item.Level})
	}
	nextPage := 0
	if page*wire.Data.Meta.PerPage < wire.Data.Meta.TotalCount {
		if len(tags) == 0 || page == 1000000 {
			return tagBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JINGdigital pagination cannot advance")
		}
		nextPage = page + 1
	}
	return sdkgo.NewQueryBranch(ListTagsBranchListed, ListTagsOutput{Tags: tags, Total: wire.Data.Meta.TotalCount, NextPage: nextPage}, nil, sdkgo.Receipt{})
}
func (operation ListTagsOperation) readResponse(response *http.Response, stage string) ([]byte, *sdkgo.QueryAttempt[ListTagsOutput]) {
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == 429 {
		attempt := tagRetry(sdkgo.FailureRateLimit, "JINGdigital "+stage+" request rate limited", retryAfter)
		return nil, &attempt
	}
	if response.StatusCode == 408 || response.StatusCode >= 500 {
		attempt := tagRetry(sdkgo.FailureAvailability, "JINGdigital "+stage+" temporarily unavailable", retryAfter)
		return nil, &attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := tagBranch(ListTagsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "JINGdigital "+stage+" response exceeded the configured limit")
		return nil, &attempt
	}
	if err != nil {
		attempt := tagRetry(sdkgo.FailureAvailability, "JINGdigital "+stage+" response was interrupted", 0)
		return nil, &attempt
	}
	if response.StatusCode != 200 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = sdkgo.FailureAuthentication
		}
		attempt := tagBranch(ListTagsBranchProviderRejected, kind, "JINGdigital rejected "+stage+" with HTTP "+strconv.Itoa(response.StatusCode))
		return nil, &attempt
	}
	return body, nil
}
func tagProviderFailure(code int, stage string) sdkgo.QueryAttempt[ListTagsOutput] {
	if code == 20000 || code == 40005 {
		return tagRetry(sdkgo.FailureAvailability, "JINGdigital "+stage+" temporarily unavailable", 0)
	}
	if code == 20002 {
		return tagRetry(sdkgo.FailureRateLimit, "JINGdigital "+stage+" request rate limited", 0)
	}
	kind := sdkgo.FailureProviderRejection
	if code == 20001 || code == 40006 {
		kind = sdkgo.FailureAuthentication
	}
	return tagBranch(ListTagsBranchProviderRejected, kind, "JINGdigital rejected "+stage+" with code "+strconv.Itoa(code))
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
func tagBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListTagsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jingdigital", Operation: "listTags", Message: message}
	return sdkgo.NewQueryBranch(branch, ListTagsOutput{}, &failure, sdkgo.Receipt{})
}
func tagRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListTagsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jingdigital", Operation: "listTags", Message: message}
	return sdkgo.NewQueryRetry[ListTagsOutput](failure, delay)
}
