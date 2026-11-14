// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package niutrans

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// TranslateTextInput supplies bounded source text and language codes.
type TranslateTextInput struct {
	// Text is the source content, limited to 1000 Unicode characters.
	Text string `json:"text"`
	// From is the NiuTrans source language code, such as zh or auto.
	From string `json:"from"`
	// To is the NiuTrans target language code, such as en.
	To string `json:"to"`
}

// TranslateTextOutput contains only the translated text, not provider echoes or the API key.
type TranslateTextOutput struct {
	// TranslatedText is the returned translation.
	TranslatedText string `json:"translatedText"`
}

// TranslateTextOperation calls NiuTrans's metered text translation API.
type TranslateTextOperation struct{ client *Client }

// Definition returns the generated translation Query contract.
func (TranslateTextOperation) Definition() sdkgo.QueryDefinition { return TranslateTextDefinition }

// Invoke sends a bounded metered translation request and classifies provider outcomes.
func (operation TranslateTextOperation) Invoke(call sdkgo.Call, input TranslateTextInput) sdkgo.QueryAttempt[TranslateTextOutput] {
	if strings.TrimSpace(input.Text) == "" || utf8.RuneCountInString(input.Text) > 1000 || !utf8.ValidString(input.Text) || !isValidLanguageCode(input.From) || !isValidLanguageCode(input.To) || input.From == input.To {
		return translationBranch(TranslateTextBranchDefect, sdkgo.FailureValidation, "translation text or language codes are invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return translationBranch(TranslateTextBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	form := url.Values{"from": {input.From}, "to": {input.To}, "apikey": {credentials.APIKey.Reveal()}, "src_text": {input.Text}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/NiuTransServer/translation", strings.NewReader(form.Encode()))
	if err != nil {
		return translationBranch(TranslateTextBranchDefect, sdkgo.FailureValidation, "translation request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return translationRetry(sdkgo.FailureAvailability, "NiuTrans request failed; a prior attempt may have consumed points", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return translationRetry(sdkgo.FailureRateLimit, "NiuTrans rate limit reached", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return translationRetry(sdkgo.FailureAvailability, "NiuTrans is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "NiuTrans response exceeded the configured limit")
	}
	if err != nil {
		return translationRetry(sdkgo.FailureAvailability, "NiuTrans response was interrupted; points may have been consumed", 0)
	}
	if response.StatusCode != http.StatusOK {
		return translationBranch(TranslateTextBranchProviderRejected, classifyNiuTransHTTPStatus(response.StatusCode), "NiuTrans rejected the translation")
	}
	var wire struct {
		ErrorCode  string  `json:"error_code"`
		TargetText *string `json:"tgt_text"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "NiuTrans returned malformed translation data")
	}
	if wire.ErrorCode != "" {
		if wire.ErrorCode == "10001" {
			return translationRetry(sdkgo.FailureRateLimit, "NiuTrans QPS limit reached", 0)
		}
		if wire.ErrorCode == "13008" {
			return translationRetry(sdkgo.FailureAvailability, "NiuTrans processing timed out", 0)
		}
		return translationBranch(TranslateTextBranchProviderRejected, sdkgo.FailureProviderRejection, "NiuTrans rejected the translation")
	}
	if wire.TargetText == nil || *wire.TargetText == "" || len(*wire.TargetText) > int(operation.client.maxResponseBytes) {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "NiuTrans returned invalid translated text")
	}
	return sdkgo.NewQueryBranch(TranslateTextBranchTranslated, TranslateTextOutput{TranslatedText: *wire.TargetText}, nil, sdkgo.Receipt{})
}

var languageCodePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z-]{1,11}$`)

func isValidLanguageCode(code string) bool { return languageCodePattern.MatchString(code) }
func isValidAPIKey(apiKey string) bool {
	if apiKey == "" || len(apiKey) > 512 || strings.TrimSpace(apiKey) != apiKey {
		return false
	}
	for _, character := range apiKey {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyNiuTransHTTPStatus(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return sdkgo.FailureAuthentication
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func translationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TranslateTextOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "niutrans", Operation: "translateText", Message: message}
	return sdkgo.NewQueryBranch(branch, TranslateTextOutput{}, &failure, sdkgo.Receipt{})
}
func translationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[TranslateTextOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "niutrans", Operation: "translateText", Message: message}
	return sdkgo.NewQueryRetry[TranslateTextOutput](failure, delay)
}
