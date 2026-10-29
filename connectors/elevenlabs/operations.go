// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListVoicesInput selects one page of account voices.
type ListVoicesInput struct {
	// PageSize is the requested page size; zero uses the provider default.
	PageSize int `json:"pageSize,omitempty"`
	// NextPageToken is a token returned by the preceding page; blank starts at page one.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// Voice identifies an ElevenLabs voice.
type Voice struct {
	// VoiceID is the stable provider voice identifier.
	VoiceID string `json:"voiceId"`
	// Name is the display name.
	Name string `json:"name"`
}

// ListVoicesOutput holds one voice page.
type ListVoicesOutput struct {
	// Voices is the page of available voices.
	Voices []Voice `json:"voices"`
	// HasMore reports whether another page is available.
	HasMore bool `json:"hasMore"`
	// NextPageToken selects the next page when HasMore is true.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// CreateSpeechInput selects a voice and the text to synthesize.
type CreateSpeechInput struct {
	// VoiceID is returned by listVoices or shown in the ElevenLabs voice library.
	VoiceID string `json:"voiceId"`
	// Text is the text to speak.
	Text string `json:"text"`
	// ModelID selects a text-to-speech model; blank uses eleven_multilingual_v2.
	ModelID string `json:"modelId,omitempty"`
}

// CreateSpeechOutput holds bounded MP3 audio as base64.
type CreateSpeechOutput struct {
	// AudioBase64 is the MP3 payload encoded for Dex persistence.
	AudioBase64 string `json:"audioBase64"`
	// ContentType identifies the MP3 format.
	ContentType string `json:"contentType"`
}

// ListVoicesOperation retrieves one page of voices.
type ListVoicesOperation struct{ client *Client }

// CreateSpeechOperation generates one billed audio result.
type CreateSpeechOperation struct{ client *Client }

// ListVoices binds the query to this client.
func (client *Client) ListVoices() ListVoicesOperation { return ListVoicesOperation{client} }

// CreateSpeech binds the query to this client.
func (client *Client) CreateSpeech() CreateSpeechOperation { return CreateSpeechOperation{client} }

// Definition returns the generated list contract.
func (ListVoicesOperation) Definition() sdkgo.QueryDefinition { return ListVoicesDefinition }

// Definition returns the generated speech contract.
func (CreateSpeechOperation) Definition() sdkgo.QueryDefinition { return CreateSpeechDefinition }

// Invoke lists voices and returns the provider's continuation token.
func (operation ListVoicesOperation) Invoke(call sdkgo.Call, input ListVoicesInput) sdkgo.QueryAttempt[ListVoicesOutput] {
	branches := queryBranches{found: ListVoicesBranchListed, rejected: ListVoicesBranchProviderRejected, invalid: ListVoicesBranchInvalidResponse, defect: ListVoicesBranchDefect}
	if input.PageSize < 0 || input.PageSize > 100 || strings.ContainsAny(input.NextPageToken, "\r\n") {
		return queryBranch("listVoices", branches.defect, ListVoicesOutput{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	query := url.Values{}
	if input.PageSize > 0 {
		query.Set("page_size", strconv.Itoa(input.PageSize))
	}
	if input.NextPageToken != "" {
		query.Set("next_page_token", input.NextPageToken)
	}
	return invokeJSONQuery(call, operation.client, "listVoices", branches, http.MethodGet, "/v2/voices", query, nil, decodeVoices)
}

// Invoke synthesizes bounded MP3 audio for one voice and text input.
func (operation CreateSpeechOperation) Invoke(call sdkgo.Call, input CreateSpeechInput) sdkgo.QueryAttempt[CreateSpeechOutput] {
	branches := queryBranches{found: CreateSpeechBranchCreated, rejected: CreateSpeechBranchProviderRejected, invalid: CreateSpeechBranchInvalidResponse, defect: CreateSpeechBranchDefect}
	if !voiceIDPattern.MatchString(input.VoiceID) || strings.TrimSpace(input.Text) == "" {
		return queryBranch("createSpeech", branches.defect, CreateSpeechOutput{}, sdkgo.FailureValidation, "voiceId and text are required")
	}
	if input.ModelID == "" {
		input.ModelID = "eleven_multilingual_v2"
	}
	payload, err := json.Marshal(struct {
		Text    string `json:"text"`
		ModelID string `json:"model_id"`
	}{input.Text, input.ModelID})
	if err != nil {
		return queryBranch("createSpeech", branches.defect, CreateSpeechOutput{}, sdkgo.FailureValidation, "speech input could not be encoded")
	}
	query := url.Values{"output_format": {"mp3_44100_128"}}
	return invokeJSONQuery(call, operation.client, "createSpeech", branches, http.MethodPost, "/v1/text-to-speech/"+input.VoiceID, query, payload, decodeSpeech)
}

var voiceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func decodeVoices(body []byte) (ListVoicesOutput, error) {
	var wire struct {
		Voices []struct {
			VoiceID string `json:"voice_id"`
			Name    string `json:"name"`
		} `json:"voices"`
		HasMore       bool   `json:"has_more"`
		NextPageToken string `json:"next_page_token"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ListVoicesOutput{}, err
	}
	if wire.Voices == nil {
		return ListVoicesOutput{}, fmt.Errorf("voices missing")
	}
	output := ListVoicesOutput{Voices: make([]Voice, 0, len(wire.Voices)), HasMore: wire.HasMore, NextPageToken: wire.NextPageToken}
	for _, voice := range wire.Voices {
		if voice.VoiceID == "" {
			return ListVoicesOutput{}, fmt.Errorf("voice ID missing")
		}
		output.Voices = append(output.Voices, Voice{VoiceID: voice.VoiceID, Name: voice.Name})
	}
	return output, nil
}

func decodeSpeech(body []byte) (CreateSpeechOutput, error) {
	if len(body) == 0 {
		return CreateSpeechOutput{}, fmt.Errorf("audio missing")
	}
	return CreateSpeechOutput{AudioBase64: base64.StdEncoding.EncodeToString(body), ContentType: "audio/mpeg"}, nil
}
