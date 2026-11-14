// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weiban

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTagsInput has no user-supplied parameters.
type ListTagsInput struct{}

// TagMetadata contains an authorized customer tag's identity and group, without customer data.
type TagMetadata struct {
	// ID is the provider tag ID.
	ID string `json:"id"`
	// Name is the human-readable tag name.
	Name string `json:"name"`
	// Group is the human-readable tag group.
	Group string `json:"group"`
}

// TagsResult contains the first ten tags and the provider's total tag count.
type TagsResult struct {
	// Total is the number of tags across all pages.
	Total int64 `json:"total"`
	// Tags contains up to ten tags from the first page.
	Tags []TagMetadata `json:"tags"`
}

// ListTagsOperation reads a bounded tag-metadata page.
type ListTagsOperation struct{ client *Client }

// Definition returns the generated tag-list Query contract.
func (ListTagsOperation) Definition() sdkgo.QueryDefinition { return ListTagsDefinition }

// Invoke obtains a short-lived access token and reads the first tag page.
func (operation ListTagsOperation) Invoke(call sdkgo.Call, _ ListTagsInput) sdkgo.QueryAttempt[TagsResult] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validCredential(credentials.CorpID) || !validCredential(credentials.Secret.Reveal()) {
		return tagsBranch(ListTagsBranchDefect, sdkgo.FailureAuthentication, "Weiban credentials are unavailable or invalid")
	}
	tokenBody, err := json.Marshal(struct {
		CorpID string `json:"corp_id"`
		Secret string `json:"secret"`
	}{credentials.CorpID, credentials.Secret.Reveal()})
	if err != nil {
		return tagsBranch(ListTagsBranchDefect, sdkgo.FailureValidation, "Weiban token request is invalid")
	}
	tokenRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/open-api/access_token/get", bytes.NewReader(tokenBody))
	if err != nil {
		return tagsBranch(ListTagsBranchDefect, sdkgo.FailureValidation, "Weiban token request is invalid")
	}
	tokenRequest.Header.Set("Content-Type", "application/json")
	tokenRequest.Header.Set("Accept", "application/json")
	tokenResponse, err := operation.client.httpClient.Do(tokenRequest)
	if err != nil {
		return tagsRetry(sdkgo.FailureAvailability, "Weiban token request failed", 0)
	}
	tokenBytes, tokenAttempt := operation.readResponse(tokenResponse)
	if tokenAttempt != nil {
		return *tokenAttempt
	}
	token, isRejected, isRetry, err := decodeAccessToken(tokenBytes)
	if err != nil {
		return tagsBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "Weiban returned an invalid token response")
	}
	if isRetry {
		return tagsRetry(sdkgo.FailureAvailability, "Weiban token service is busy", 0)
	}
	if isRejected {
		return tagsBranch(ListTagsBranchProviderRejected, sdkgo.FailureAuthentication, "Weiban rejected the application credentials")
	}
	query := url.Values{"access_token": {token}, "limit": {"10"}, "offset": {"0"}}
	tagRequest, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/open-api/tag/list?"+query.Encode(), nil)
	if err != nil {
		return tagsBranch(ListTagsBranchDefect, sdkgo.FailureValidation, "Weiban tag request is invalid")
	}
	tagRequest.Header.Set("Accept", "application/json")
	tagResponse, err := operation.client.httpClient.Do(tagRequest)
	if err != nil {
		return tagsRetry(sdkgo.FailureAvailability, "Weiban tag request failed", 0)
	}
	tagBytes, tagAttempt := operation.readResponse(tagResponse)
	if tagAttempt != nil {
		return *tagAttempt
	}
	result, isRejected, isRetry, err := decodeTags(tagBytes)
	if err != nil {
		return tagsBranch(ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, "Weiban returned an invalid tag response")
	}
	if isRetry {
		return tagsRetry(sdkgo.FailureAvailability, "Weiban tag service is busy", 0)
	}
	if isRejected {
		return tagsBranch(ListTagsBranchProviderRejected, sdkgo.FailureProviderRejection, "Weiban rejected the tag query")
	}
	return sdkgo.NewQueryBranch(ListTagsBranchListed, result, nil, sdkgo.Receipt{})
}

func (operation ListTagsOperation) readResponse(response *http.Response) ([]byte, *sdkgo.QueryAttempt[TagsResult]) {
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		attempt := tagsRetry(sdkgo.FailureRateLimit, "Weiban rate limited request", retryAfter)
		return nil, &attempt
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		attempt := tagsRetry(sdkgo.FailureAvailability, "Weiban API is temporarily unavailable", retryAfter)
		return nil, &attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := tagsBranch(ListTagsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Weiban response exceeded the configured limit")
		return nil, &attempt
	}
	if err != nil {
		attempt := tagsRetry(sdkgo.FailureAvailability, "Weiban response was interrupted", 0)
		return nil, &attempt
	}
	if response.StatusCode != http.StatusOK {
		attempt := tagsBranch(ListTagsBranchProviderRejected, classifyTagsHTTP(response.StatusCode), "Weiban rejected request with HTTP "+strconv.Itoa(response.StatusCode))
		return nil, &attempt
	}
	return body, nil
}
func decodeAccessToken(body []byte) (string, bool, bool, error) {
	var envelope struct {
		ErrorCode   *int   `json:"errcode"`
		AccessToken string `json:"access_token"`
		ExpiresIn   *int   `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ErrorCode == nil {
		return "", false, false, errors.New("invalid Weiban token response")
	}
	if *envelope.ErrorCode != 0 {
		return "", true, isBusyCode(*envelope.ErrorCode), nil
	}
	if !validCredential(envelope.AccessToken) || envelope.ExpiresIn == nil || *envelope.ExpiresIn <= 0 {
		return "", false, false, errors.New("invalid Weiban access token")
	}
	return envelope.AccessToken, false, false, nil
}
func decodeTags(body []byte) (TagsResult, bool, bool, error) {
	var envelope struct {
		ErrorCode *int   `json:"errcode"`
		Total     *int64 `json:"total"`
		Tags      []struct {
			ID    string `json:"tag_id"`
			Name  string `json:"tag_name"`
			Group string `json:"tag_group"`
		} `json:"tag_list"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ErrorCode == nil {
		return TagsResult{}, false, false, errors.New("invalid Weiban tag response")
	}
	if *envelope.ErrorCode != 0 {
		return TagsResult{}, true, isBusyCode(*envelope.ErrorCode), nil
	}
	if envelope.Total == nil || *envelope.Total < 0 || len(envelope.Tags) > 100 || int64(len(envelope.Tags)) > *envelope.Total || envelope.Tags == nil {
		return TagsResult{}, false, false, errors.New("invalid Weiban tags")
	}
	result := TagsResult{Total: *envelope.Total, Tags: make([]TagMetadata, 0, len(envelope.Tags))}
	for _, tag := range envelope.Tags {
		if len(result.Tags) == 10 {
			break
		}
		if tag.ID == "" || len(tag.ID) > 128 || len(tag.Name) > 512 || len(tag.Group) > 512 {
			return TagsResult{}, false, false, errors.New("invalid Weiban tag")
		}
		result.Tags = append(result.Tags, TagMetadata{ID: tag.ID, Name: tag.Name, Group: tag.Group})
	}
	return result, false, false, nil
}
func isBusyCode(code int) bool { return code == 500 || code == 10051 || code == 10052 || code == 10053 }
func validCredential(value string) bool {
	return value != "" && len(value) <= 2048 && strings.TrimSpace(value) == value
}
func classifyTagsHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func tagsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TagsResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "weiban", Operation: "listTags", Message: message}
	return sdkgo.NewQueryBranch(branch, TagsResult{}, &failure, sdkgo.Receipt{})
}
func tagsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[TagsResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "weiban", Operation: "listTags", Message: message}
	return sdkgo.NewQueryRetry[TagsResult](failure, delay)
}
