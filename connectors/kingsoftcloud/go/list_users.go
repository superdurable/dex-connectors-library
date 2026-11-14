// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kingsoftcloud

import (
	"crypto/hmac"
	"crypto/sha256"
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

// ListUsersInput selects one IAM user page. MaxItems zero uses the provider default of 100.
type ListUsersInput struct {
	// Marker is the opaque next-page token from a previous response; empty starts at the first page.
	Marker string `json:"marker,omitempty"`
	// MaxItems is the requested page size from 1 to 100; zero uses 100.
	MaxItems int `json:"maxItems,omitempty"`
}

// IAMUser contains only stable IAM identity fields, excluding contact and profile data.
type IAMUser struct {
	// UserID is the provider's stable IAM user ID.
	UserID string `json:"userId"`
	// UserName is the provider's IAM user name.
	UserName string `json:"userName"`
}

// ListUsersOutput contains one page and its optional continuation marker.
type ListUsersOutput struct {
	// Users contains the returned IAM identities.
	Users []IAMUser `json:"users"`
	// HasMore reports whether another page is available.
	HasMore bool `json:"hasMore"`
	// NextMarker is passed as the next call's Marker when HasMore is true.
	NextMarker string `json:"nextMarker"`
}

// ListUsersOperation reads one bounded IAM user page.
type ListUsersOperation struct{ client *Client }

// Definition returns the generated ListUsers contract.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition { return ListUsersDefinition }

// Invoke signs and sends one IAM ListUsers request and classifies provider and transport outcomes.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[ListUsersOutput] {
	if input.MaxItems < 0 || input.MaxItems > 100 || len(input.Marker) > 320 || strings.TrimSpace(input.Marker) != input.Marker {
		return listUsersBranch(ListUsersBranchDefect, sdkgo.FailureValidation, "IAM page size or marker is invalid")
	}
	maxItems := input.MaxItems
	if maxItems == 0 {
		maxItems = 100
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AccessKeyID.Reveal()) || !isValidCredential(credentials.SecretAccessKey.Reveal()) {
		return listUsersBranch(ListUsersBranchDefect, sdkgo.FailureAuthentication, "IAM credentials are unavailable or invalid")
	}
	query := url.Values{
		"Accesskey":        {credentials.AccessKeyID.Reveal()},
		"Action":           {"ListUsers"},
		"Service":          {operation.client.service},
		"Region":           {operation.client.region},
		"Version":          {"2015-11-01"},
		"Timestamp":        {operation.client.now().UTC().Format(time.RFC3339)},
		"SignatureVersion": {"1.0"},
		"SignatureMethod":  {"HMAC-SHA256"},
		"MaxItems":         {strconv.Itoa(maxItems)},
	}
	if input.Marker != "" {
		query.Set("Marker", input.Marker)
	}
	query.Set("Signature", signIAMQuery(query, credentials.SecretAccessKey.Reveal()))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/?"+canonicalIAMQuery(query), nil)
	if err != nil {
		return listUsersBranch(ListUsersBranchDefect, sdkgo.FailureValidation, "IAM request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return listUsersRetry(sdkgo.FailureAvailability, "IAM request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return listUsersRetry(sdkgo.FailureRateLimit, "IAM rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return listUsersRetry(sdkgo.FailureAvailability, "IAM is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return listUsersBranch(ListUsersBranchProviderRejected, sdkgo.FailureProviderRejection, "IAM rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return listUsersBranch(ListUsersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "IAM response exceeded the configured limit")
	}
	if err != nil {
		return listUsersRetry(sdkgo.FailureAvailability, "IAM response was interrupted", 0)
	}
	output, err := decodeListUsersResponse(body)
	if err != nil {
		return listUsersBranch(ListUsersBranchInvalidResponse, sdkgo.FailureProtocol, "IAM returned an invalid response")
	}
	return sdkgo.NewQueryBranch(ListUsersBranchListed, output, nil, sdkgo.Receipt{})
}

func decodeListUsersResponse(body []byte) (ListUsersOutput, error) {
	var wire struct {
		ListUserResult *struct {
			Users *struct {
				Member []struct {
					UserID   string `json:"UserId"`
					UserName string `json:"UserName"`
				} `json:"member"`
			} `json:"Users"`
			IsTruncated bool   `json:"IsTruncated"`
			Marker      string `json:"Marker"`
		} `json:"ListUserResult"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ListUserResult == nil || wire.ListUserResult.Users == nil {
		return ListUsersOutput{}, errors.New("invalid IAM response envelope")
	}
	result := wire.ListUserResult
	if len(result.Users.Member) > 100 || (result.IsTruncated && result.Marker == "") {
		return ListUsersOutput{}, errors.New("invalid IAM pagination")
	}
	output := ListUsersOutput{Users: make([]IAMUser, 0, len(result.Users.Member)), HasMore: result.IsTruncated}
	if result.IsTruncated {
		output.NextMarker = result.Marker
	}
	for _, member := range result.Users.Member {
		if member.UserID == "" || member.UserName == "" {
			return ListUsersOutput{}, errors.New("invalid IAM user")
		}
		output.Users = append(output.Users, IAMUser{UserID: member.UserID, UserName: member.UserName})
	}
	return output, nil
}

func isValidCredential(value string) bool {
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

func signIAMQuery(query url.Values, secretAccessKey string) string {
	signature := hmac.New(sha256.New, []byte(secretAccessKey))
	signature.Write([]byte(canonicalIAMQuery(query)))
	return hex.EncodeToString(signature.Sum(nil))
}

func canonicalIAMQuery(query url.Values) string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range query[key] {
			pairs = append(pairs, encodeRFC3986(key)+"="+encodeRFC3986(value))
		}
	}
	return strings.Join(pairs, "&")
}

func encodeRFC3986(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func listUsersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListUsersOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kingsoftcloud", Operation: "listUsers", Message: message}
	return sdkgo.NewQueryBranch(branch, ListUsersOutput{Users: []IAMUser{}}, &failure, sdkgo.Receipt{})
}

func listUsersRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListUsersOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kingsoftcloud", Operation: "listUsers", Message: message}
	return sdkgo.NewQueryRetry[ListUsersOutput](failure, delay)
}
