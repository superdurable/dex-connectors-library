// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package polyv

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

const channelsEndpoint = "https://api.polyv.net/live/v3/user/channels"

// ListChannelsInput requests the complete live channel-ID list for the account.
type ListChannelsInput struct{}

// ListChannelsOutput contains only provider channel IDs, with no channel configuration.
type ListChannelsOutput struct {
	ChannelIDs []string `json:"channelIds"`
}

// ListChannelsOperation reads the signed Polyv channel-ID list.
type ListChannelsOperation struct{ client *Client }

// Definition returns the generated channel-list contract.
func (ListChannelsOperation) Definition() sdkgo.QueryDefinition { return ListChannelsDefinition }

// Invoke lists channel IDs and classifies provider and transport outcomes.
func (operation ListChannelsOperation) Invoke(call sdkgo.Call, _ ListChannelsInput) sdkgo.QueryAttempt[ListChannelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredentialText(credentials.AppID) || !isValidCredentialText(credentials.AppSecret.Reveal()) {
		return channelCatalogBranch(ListChannelsBranchDefect, sdkgo.FailureAuthentication, "Polyv credentials are unavailable or invalid")
	}
	values := url.Values{}
	values.Set("appId", credentials.AppID)
	values.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	values.Set("sign", signPolyvParameters(values, credentials.AppSecret.Reveal()))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, channelsEndpoint+"?"+values.Encode(), nil)
	if err != nil {
		return channelCatalogBranch(ListChannelsBranchDefect, sdkgo.FailureValidation, "Polyv request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return channelCatalogRetry(sdkgo.FailureAvailability, "Polyv request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return channelCatalogRetry(sdkgo.FailureRateLimit, "Polyv rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return channelCatalogRetry(sdkgo.FailureAvailability, "Polyv is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return channelCatalogBranch(ListChannelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Polyv response exceeded the configured limit")
	}
	if err != nil {
		return channelCatalogRetry(sdkgo.FailureAvailability, "Polyv response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return channelCatalogBranch(ListChannelsBranchProviderRejected, kind, "Polyv rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return channelCatalogBranch(ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, "Polyv returned invalid channel-list data")
	}
	if *wire.Code != 200 {
		if wire.Message == "qps exceeds number of calls" {
			return channelCatalogRetry(sdkgo.FailureRateLimit, "Polyv rate limited the request", retryAfter)
		}
		kind := sdkgo.FailureProviderRejection
		if wire.Message == "invalid signature." || wire.Message == "need to login" {
			kind = sdkgo.FailureAuthentication
		}
		if wire.Message == "access forbidden" {
			kind = sdkgo.FailureAuthorization
		}
		return channelCatalogBranch(ListChannelsBranchProviderRejected, kind, "Polyv rejected the signed channel-list request")
	}
	var channelData struct {
		Channels []json.RawMessage `json:"channels"`
	}
	if json.Unmarshal(wire.Data, &channelData) != nil || channelData.Channels == nil {
		return channelCatalogBranch(ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, "Polyv omitted the channel list")
	}
	identifiers := make([]string, 0, len(channelData.Channels))
	for _, raw := range channelData.Channels {
		var identifier string
		if len(raw) > 0 && raw[0] == '"' {
			if json.Unmarshal(raw, &identifier) != nil {
				return channelCatalogBranch(ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, "Polyv returned an invalid channel ID")
			}
		} else {
			identifier = string(raw)
		}
		if identifier == "" || len(identifier) > 64 {
			return channelCatalogBranch(ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, "Polyv returned an invalid channel ID")
		}
		for _, digit := range identifier {
			if digit < '0' || digit > '9' {
				return channelCatalogBranch(ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, "Polyv returned an invalid channel ID")
			}
		}
		identifiers = append(identifiers, identifier)
	}
	return sdkgo.NewQueryBranch(ListChannelsBranchListed, ListChannelsOutput{ChannelIDs: identifiers}, nil, sdkgo.Receipt{})
}

func signPolyvParameters(values url.Values, secret string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "sign" && values.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var text strings.Builder
	text.WriteString(secret)
	for _, key := range keys {
		text.WriteString(key)
		text.WriteString(values.Get(key))
	}
	text.WriteString(secret)
	sum := md5.Sum([]byte(text.String()))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
func isValidCredentialText(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func channelCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListChannelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "polyv", Operation: "listChannels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListChannelsOutput{}, &failure, sdkgo.Receipt{})
}
func channelCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListChannelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "polyv", Operation: "listChannels", Message: message}
	return sdkgo.NewQueryRetry[ListChannelsOutput](failure, delay)
}
