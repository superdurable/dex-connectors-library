// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unisound

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

// ListSystemVoicesInput requests the predefined voice catalog without parameters.
type ListSystemVoicesInput struct{}

// SystemVoice describes one predefined text-to-speech voice.
type SystemVoice struct {
	// VoiceID is the identifier accepted by Unisound text-to-speech APIs.
	VoiceID string `json:"voiceID"`
	// Name is the displayed Chinese voice name.
	Name string `json:"name"`
	// Languages are the voice's language codes.
	Languages []string `json:"languages"`
}

// ListSystemVoicesOutput contains up to 25 predefined voices.
type ListSystemVoicesOutput struct {
	// Voices are provider-ordered system voices.
	Voices []SystemVoice `json:"voices"`
}

// ListSystemVoicesOperation reads Unisound's system voice catalog.
type ListSystemVoicesOperation struct{ client *Client }

// Definition returns the generated voice catalog contract.
func (ListSystemVoicesOperation) Definition() sdkgo.QueryDefinition {
	return ListSystemVoicesDefinition
}

// Invoke obtains system voices while omitting cloned and designed voice metadata.
func (operation ListSystemVoicesOperation) Invoke(call sdkgo.Call, _ ListSystemVoicesInput) sdkgo.QueryAttempt[ListSystemVoicesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return voicesBranch(ListSystemVoicesBranchDefect, sdkgo.FailureAuthentication, "Unisound API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, voiceQueryEndpoint, bytes.NewBufferString(`{"voice_type":"system"}`))
	if err != nil {
		return voicesBranch(ListSystemVoicesBranchDefect, sdkgo.FailureValidation, "Unisound voice request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return voicesRetry(sdkgo.FailureAvailability, "Unisound voice request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return voicesRetry(sdkgo.FailureRateLimit, "Unisound rate limited the voice request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return voicesRetry(sdkgo.FailureAvailability, "Unisound voice service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyVoicesHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return voicesBranch(ListSystemVoicesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Unisound response exceeded the configured limit")
	}
	if err != nil {
		return voicesRetry(sdkgo.FailureAvailability, "Unisound response was interrupted", 0)
	}
	var wire struct {
		SystemVoices []struct {
			VoiceID   string   `json:"voice_id"`
			Name      string   `json:"voice_name"`
			Languages []string `json:"language"`
		} `json:"system_voice"`
		BaseResponse struct {
			StatusCode *int `json:"status_code"`
		} `json:"base_resp"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.BaseResponse.StatusCode == nil {
		return voicesBranch(ListSystemVoicesBranchInvalidResponse, sdkgo.FailureProtocol, "Unisound returned invalid voice data")
	}
	if *wire.BaseResponse.StatusCode != 0 {
		if *wire.BaseResponse.StatusCode == 100501 {
			return voicesRetry(sdkgo.FailureRateLimit, "Unisound rate limited the voice request", 0)
		}
		if *wire.BaseResponse.StatusCode == 100999 {
			return voicesRetry(sdkgo.FailureAvailability, "Unisound voice service is temporarily unavailable", 0)
		}
		return voicesBranch(ListSystemVoicesBranchProviderRejected, sdkgo.FailureProviderRejection, "Unisound rejected the voice request with code "+strconv.Itoa(*wire.BaseResponse.StatusCode))
	}
	if wire.SystemVoices == nil || len(wire.SystemVoices) > 1000 {
		return voicesBranch(ListSystemVoicesBranchInvalidResponse, sdkgo.FailureProtocol, "Unisound returned invalid system voices")
	}
	voices := make([]SystemVoice, 0, min(len(wire.SystemVoices), 25))
	for _, item := range wire.SystemVoices {
		if len(voices) == 25 {
			break
		}
		if item.VoiceID == "" || len(item.VoiceID) > 128 || strings.TrimSpace(item.VoiceID) != item.VoiceID || len(item.Name) > 200 || item.Languages == nil || len(item.Languages) > 20 {
			return voicesBranch(ListSystemVoicesBranchInvalidResponse, sdkgo.FailureProtocol, "Unisound returned invalid system voice")
		}
		voices = append(voices, SystemVoice{VoiceID: item.VoiceID, Name: item.Name, Languages: item.Languages})
	}
	return sdkgo.NewQueryBranch(ListSystemVoicesBranchFound, ListSystemVoicesOutput{Voices: voices}, nil, sdkgo.Receipt{})
}
func classifyVoicesHTTPStatus(status int) sdkgo.QueryAttempt[ListSystemVoicesOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return voicesBranch(ListSystemVoicesBranchProviderRejected, kind, "Unisound rejected voice request with HTTP "+strconv.Itoa(status))
}
func voicesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListSystemVoicesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "unisound", Operation: "listSystemVoices", Message: message}
	return sdkgo.NewQueryBranch(branch, ListSystemVoicesOutput{}, &failure, sdkgo.Receipt{})
}
func voicesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListSystemVoicesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "unisound", Operation: "listSystemVoices", Message: message}
	return sdkgo.NewQueryRetry[ListSystemVoicesOutput](failure, delay)
}
