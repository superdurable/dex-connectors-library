// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetChannelInput selects a connected Buffer channel by opaque ID.
type GetChannelInput struct {
	// ChannelID is the ID returned by Buffer channel queries.
	ChannelID string `json:"channelId"`
}

// ChannelProfile contains public metadata for one connected channel.
type ChannelProfile struct {
	// ID is the Buffer channel ID.
	ID string `json:"id"`
	// Name is the connected channel name.
	Name string `json:"name"`
	// Service is the social platform identifier.
	Service string `json:"service"`
}

// GetChannelOperation reads one channel through Buffer GraphQL.
type GetChannelOperation struct{ client *Client }

// GetChannel binds the Query to this client.
func (client *Client) GetChannel() GetChannelOperation {
	return GetChannelOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetChannelOperation) Definition() sdkgo.QueryDefinition { return GetChannelDefinition }

// Invoke sends the read-only query and classifies its result.
func (operation GetChannelOperation) Invoke(call sdkgo.Call, input GetChannelInput) sdkgo.QueryAttempt[ChannelProfile] {
	if !validGraphQLID(input.ChannelID) {
		return channelBranch(GetChannelBranchDefect, sdkgo.FailureValidation, "channelId must contain 1–128 printable characters")
	}
	response := operation.client.get(call, input)
	if errors.Is(response.err, errCredential) {
		return channelBranch(GetChannelBranchDefect, sdkgo.FailureAuthentication, "Buffer credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ChannelProfile](channelFailure(sdkgo.FailureTransport, "Buffer request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ChannelProfile](channelFailure(sdkgo.FailureRateLimit, "Buffer rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ChannelProfile](channelFailure(sdkgo.FailureAvailability, "Buffer is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return channelBranch(GetChannelBranchProviderRejected, sdkgo.FailureProviderRejection, "Buffer rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return channelBranch(GetChannelBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Buffer response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ChannelProfile](channelFailure(sdkgo.FailureTransport, "Buffer response was interrupted"), 0)
	}
	profile, isNotFound, isRejected, err := decodeChannel(response.body, input.ChannelID)
	if err != nil {
		return channelBranch(GetChannelBranchInvalidResponse, sdkgo.FailureProtocol, "Buffer returned invalid channel JSON")
	}
	if isRejected {
		return channelBranch(GetChannelBranchProviderRejected, sdkgo.FailureProviderRejection, "Buffer rejected the GraphQL query")
	}
	if isNotFound {
		return channelBranch(GetChannelBranchNotFound, sdkgo.FailureNotFound, "Buffer did not expose the channel")
	}
	return sdkgo.NewQueryBranch(GetChannelBranchFound, profile, nil, sdkgo.Receipt{})
}

func validGraphQLID(value string) bool {
	if len(value) == 0 || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func decodeChannel(body []byte, channelID string) (ChannelProfile, bool, bool, error) {
	var wire struct {
		Data *struct {
			Channel *struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Service string `json:"service"`
			} `json:"channel"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ChannelProfile{}, false, false, err
	}
	if len(wire.Errors) > 0 {
		return ChannelProfile{}, false, true, nil
	}
	if wire.Data == nil {
		return ChannelProfile{}, false, false, errors.New("GraphQL data is missing")
	}
	if wire.Data.Channel == nil {
		return ChannelProfile{}, true, false, nil
	}
	if wire.Data.Channel.ID != channelID || wire.Data.Channel.Name == "" || wire.Data.Channel.Service == "" {
		return ChannelProfile{}, false, false, errors.New("channel identity, name, or service is invalid")
	}
	return ChannelProfile{ID: wire.Data.Channel.ID, Name: wire.Data.Channel.Name, Service: wire.Data.Channel.Service}, false, false, nil
}

func channelFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "buffer", Operation: "getChannel", Message: message}
}
func channelBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ChannelProfile] {
	failure := channelFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ChannelProfile{}, &failure, sdkgo.Receipt{})
}
