// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetVoiceInput identifies one provider voice.
type GetVoiceInput struct {
	// VoiceID is returned by listVoices or shown in the ElevenLabs voice library.
	VoiceID string `json:"voiceId"`
}

// UpdateVoiceSettingsInput absolutely replaces five generation settings.
type UpdateVoiceSettingsInput struct {
	// VoiceID identifies the voice owned by the connection.
	VoiceID string `json:"voiceId"`
	// Stability is a value from zero to one.
	Stability float64 `json:"stability"`
	// SimilarityBoost is a value from zero to one.
	SimilarityBoost float64 `json:"similarityBoost"`
	// Style is a value from zero to one.
	Style float64 `json:"style"`
	// Speed is a positive playback speed multiplier.
	Speed float64 `json:"speed"`
	// UseSpeakerBoost enables extra speaker similarity processing.
	UseSpeakerBoost bool `json:"useSpeakerBoost"`
}

// UpdatedVoiceSettings confirms the target voice ID.
type UpdatedVoiceSettings struct {
	// VoiceID identifies the voice whose settings were replaced.
	VoiceID string `json:"voiceId"`
}

// GetVoiceOperation reads one voice's identity and name.
type GetVoiceOperation struct{ client *Client }

// UpdateVoiceSettingsOperation replaces a voice's primary settings.
type UpdateVoiceSettingsOperation struct{ client *Client }

// GetVoice binds the read Query to this client.
func (client *Client) GetVoice() GetVoiceOperation { return GetVoiceOperation{client} }

// UpdateVoiceSettings binds the settings Mutation to this client.
func (client *Client) UpdateVoiceSettings() UpdateVoiceSettingsOperation {
	return UpdateVoiceSettingsOperation{client}
}

// Definition returns the generated voice read contract.
func (GetVoiceOperation) Definition() sdkgo.QueryDefinition { return GetVoiceDefinition }

// Definition returns the generated settings replacement contract.
func (UpdateVoiceSettingsOperation) Definition() sdkgo.MutationDefinition {
	return UpdateVoiceSettingsDefinition
}

// IdempotencyKey records a stable call identity; the replacement itself is safe to repeat.
func (UpdateVoiceSettingsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateVoiceSettingsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke returns the voice's provider identity and name.
func (operation GetVoiceOperation) Invoke(call sdkgo.Call, input GetVoiceInput) sdkgo.QueryAttempt[Voice] {
	branches := queryBranches{found: GetVoiceBranchFound, notFound: GetVoiceBranchNotFound, rejected: GetVoiceBranchProviderRejected, invalid: GetVoiceBranchInvalidResponse, defect: GetVoiceBranchDefect}
	if !voiceIDPattern.MatchString(input.VoiceID) {
		return queryBranch("getVoice", branches.defect, Voice{}, sdkgo.FailureValidation, "voiceId is invalid")
	}
	return invokeJSONQuery(call, operation.client, "getVoice", branches, http.MethodGet, "/v1/voices/"+input.VoiceID, nil, nil, decodeVoice)
}

// Invoke sends absolute settings and retries an unknown outcome safely.
func (operation UpdateVoiceSettingsOperation) Invoke(call sdkgo.Call, input UpdateVoiceSettingsInput) sdkgo.MutationAttempt[UpdatedVoiceSettings] {
	if !voiceIDPattern.MatchString(input.VoiceID) || !validVoiceSetting(input.Stability) || !validVoiceSetting(input.SimilarityBoost) || !validVoiceSetting(input.Style) || math.IsNaN(input.Speed) || math.IsInf(input.Speed, 0) || input.Speed <= 0 {
		return updateVoiceSettingsFailure(UpdateVoiceSettingsBranchDefect, sdkgo.FailureValidation, "voice ID or settings are invalid")
	}
	payload, err := json.Marshal(struct {
		Stability       float64 `json:"stability"`
		SimilarityBoost float64 `json:"similarity_boost"`
		Style           float64 `json:"style"`
		Speed           float64 `json:"speed"`
		UseSpeakerBoost bool    `json:"use_speaker_boost"`
	}{input.Stability, input.SimilarityBoost, input.Style, input.Speed, input.UseSpeakerBoost})
	if err != nil {
		return updateVoiceSettingsFailure(UpdateVoiceSettingsBranchDefect, sdkgo.FailureValidation, "settings could not be encoded")
	}
	body, status, retryAfter, err := operation.client.do(call, http.MethodPost, "/v1/voices/"+input.VoiceID+"/settings/edit", nil, payload)
	if errors.Is(err, errCredential) {
		return updateVoiceSettingsFailure(UpdateVoiceSettingsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return updateVoiceSettingsRetry(sdkgo.FailureAvailability, "provider request failed", 0)
	}
	if status == http.StatusTooManyRequests {
		return updateVoiceSettingsRetry(sdkgo.FailureRateLimit, "provider rate limited the settings update", retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= http.StatusInternalServerError {
		return updateVoiceSettingsRetry(sdkgo.FailureAvailability, "provider is temporarily unavailable", retryAfter)
	}
	if status == http.StatusNotFound {
		return updateVoiceSettingsFailure(UpdateVoiceSettingsBranchNotFound, sdkgo.FailureNotFound, "provider did not find the voice")
	}
	if status != http.StatusOK {
		return updateVoiceSettingsFailure(UpdateVoiceSettingsBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected the settings with HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return updateVoiceSettingsRetry(sdkgo.FailureResponseTooLarge, "provider confirmation exceeded the configured limit", 0)
	}
	if err != nil {
		return updateVoiceSettingsRetry(sdkgo.FailureAvailability, "provider response was interrupted", 0)
	}
	var response struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &response) != nil || response.Status != "ok" {
		return updateVoiceSettingsRetry(sdkgo.FailureProtocol, "provider confirmation was unusable", 0)
	}
	return sdkgo.NewMutationBranch(UpdateVoiceSettingsBranchUpdated, UpdatedVoiceSettings{VoiceID: input.VoiceID}, nil, sdkgo.Receipt{})
}

func decodeVoice(body []byte) (Voice, error) {
	var wire struct {
		VoiceID string `json:"voice_id"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Voice{}, err
	}
	if !voiceIDPattern.MatchString(wire.VoiceID) || wire.Name == "" {
		return Voice{}, errors.New("voice identity missing")
	}
	return Voice{VoiceID: wire.VoiceID, Name: wire.Name}, nil
}

func validVoiceSetting(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func updateVoiceSettingsFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UpdatedVoiceSettings] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elevenlabs", Operation: "updateVoiceSettings", Message: message}
	return sdkgo.NewMutationBranch(branch, UpdatedVoiceSettings{}, &failure, sdkgo.Receipt{})
}

func updateVoiceSettingsRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[UpdatedVoiceSettings] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elevenlabs", Operation: "updateVoiceSettings", Message: message}
	return sdkgo.NewMutationRetry[UpdatedVoiceSettings](failure, retryAfter)
}
