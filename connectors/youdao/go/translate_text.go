// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youdao

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// TranslateTextInput gives the bounded source text and language codes.
type TranslateTextInput struct {
	// Text is UTF-8 source text of at most 5000 characters.
	Text string `json:"text"`
	// SourceLanguage is a Youdao language code or auto.
	SourceLanguage string `json:"sourceLanguage"`
	// TargetLanguage is a Youdao target language code.
	TargetLanguage string `json:"targetLanguage"`
}

// TextTranslation contains only the returned translations and language direction.
type TextTranslation struct {
	// Translation holds one or more translated strings.
	Translation []string `json:"translation"`
	// SourceLanguage is the requested source language code.
	SourceLanguage string `json:"sourceLanguage"`
	// TargetLanguage is the requested target language code.
	TargetLanguage string `json:"targetLanguage"`
}

// TranslateTextOperation translates text through the signed Youdao API.
type TranslateTextOperation struct{ client *Client }

// TranslateText binds the Query to this client.
func (client *Client) TranslateText() TranslateTextOperation { return TranslateTextOperation{client} }

// Definition returns the manifest-generated Query worker.
func (TranslateTextOperation) Definition() sdkgo.QueryDefinition { return TranslateTextDefinition }

// Invoke sends one signed request and classifies the provider outcome.
func (operation TranslateTextOperation) Invoke(call sdkgo.Call, input TranslateTextInput) sdkgo.QueryAttempt[TextTranslation] {
	if !validTranslationInput(input) {
		return translationBranch(TranslateTextBranchDefect, sdkgo.FailureValidation, "Youdao text or language code is invalid")
	}
	response := operation.client.translate(call, input)
	if errors.Is(response.err, errCredential) {
		return translationBranch(TranslateTextBranchDefect, sdkgo.FailureAuthentication, "Youdao credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TextTranslation](translationFailure(sdkgo.FailureTransport, "Youdao request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TextTranslation](translationFailure(sdkgo.FailureRateLimit, "Youdao rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TextTranslation](translationFailure(sdkgo.FailureAvailability, "Youdao is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Youdao response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TextTranslation](translationFailure(sdkgo.FailureTransport, "Youdao response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return translationBranch(TranslateTextBranchProviderRejected, sdkgo.FailureProviderRejection, "Youdao rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "Youdao returned invalid UTF-8")
	}
	var wire struct {
		ErrorCode   string   `json:"errorCode"`
		Translation []string `json:"translation"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "Youdao returned invalid JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "Youdao returned trailing JSON")
	}
	if wire.ErrorCode == "" {
		return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "Youdao omitted errorCode")
	}
	switch wire.ErrorCode {
	case "0":
		if len(wire.Translation) == 0 || len(wire.Translation) > 10 {
			return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "Youdao returned no or too many translations")
		}
		for _, value := range wire.Translation {
			if value == "" || len(value) > 65536 {
				return translationBranch(TranslateTextBranchInvalidResponse, sdkgo.FailureProtocol, "Youdao returned invalid translation text")
			}
		}
		return sdkgo.NewQueryBranch(TranslateTextBranchTranslated, TextTranslation{Translation: wire.Translation, SourceLanguage: input.SourceLanguage, TargetLanguage: input.TargetLanguage}, nil, sdkgo.Receipt{})
	case "411", "412":
		return sdkgo.NewQueryRetry[TextTranslation](translationFailure(sdkgo.FailureRateLimit, "Youdao rate limited translation ("+wire.ErrorCode+")"), response.retryAfter)
	case "302", "303", "304":
		return sdkgo.NewQueryRetry[TextTranslation](translationFailure(sdkgo.FailureAvailability, "Youdao translation unavailable ("+wire.ErrorCode+")"), response.retryAfter)
	case "108", "111", "202", "203", "206", "401":
		return translationBranch(TranslateTextBranchProviderRejected, sdkgo.FailureAuthentication, "Youdao rejected credentials or account ("+wire.ErrorCode+")")
	default:
		return translationBranch(TranslateTextBranchProviderRejected, sdkgo.FailureProviderRejection, "Youdao rejected translation ("+wire.ErrorCode+")")
	}
}
func validTranslationInput(input TranslateTextInput) bool {
	if !utf8.ValidString(input.Text) || strings.TrimSpace(input.Text) == "" || utf8.RuneCountInString(input.Text) > 5000 {
		return false
	}
	return validLanguageCode(input.SourceLanguage) && validLanguageCode(input.TargetLanguage) && input.TargetLanguage != "auto"
}
func validLanguageCode(value string) bool {
	if len(value) == 0 || len(value) > 24 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && character != '-' {
			return false
		}
	}
	return true
}
func translationFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "youdao", Operation: "translateText", Message: message}
}
func translationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TextTranslation] {
	failure := translationFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TextTranslation{}, &failure, sdkgo.Receipt{})
}
