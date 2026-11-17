// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// EditVoiceMetadataInput replaces a voice name and optionally its description.
type EditVoiceMetadataInput struct {
	// VoiceID is the owned voice returned by ListVoices.
	VoiceID string `json:"voiceId"`
	// Name is the replacement display name.
	Name string `json:"name"`
	// Description replaces the description when nonempty; blank leaves it unchanged.
	Description string `json:"description,omitempty"`
}

// EditedVoiceMetadata confirms the accepted voice name.
type EditedVoiceMetadata struct {
	// VoiceID is the edited provider voice ID.
	VoiceID string `json:"voiceId"`
	// Name is the requested replacement display name.
	Name string `json:"name"`
}

// DeletedVoice confirms a voice is absent.
type DeletedVoice struct {
	// VoiceID is the selected provider voice ID.
	VoiceID string `json:"voiceId"`
	// Deleted is true after deletion or confirmed absence.
	Deleted bool `json:"deleted"`
}

// EditVoiceMetadataOperation replaces voice display metadata without uploading samples.
type EditVoiceMetadataOperation struct{ client *Client }

// DeleteVoiceOperation permanently deletes one voice.
type DeleteVoiceOperation struct{ client *Client }

// EditVoiceMetadata binds the metadata Mutation to this client.
func (client *Client) EditVoiceMetadata() EditVoiceMetadataOperation {
	return EditVoiceMetadataOperation{client}
}

// DeleteVoice binds the deletion Mutation to this client.
func (client *Client) DeleteVoice() DeleteVoiceOperation { return DeleteVoiceOperation{client} }

// Definition returns the generated voice metadata edit contract.
func (EditVoiceMetadataOperation) Definition() sdkgo.MutationDefinition {
	return EditVoiceMetadataDefinition
}

// Definition returns the generated voice deletion contract.
func (DeleteVoiceOperation) Definition() sdkgo.MutationDefinition { return DeleteVoiceDefinition }

// IdempotencyKey records a stable identity for absolute metadata replacement.
func (EditVoiceMetadataOperation) IdempotencyKey(callID sdkgo.CallID, _ EditVoiceMetadataInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records a stable identity for repeatable deletion.
func (DeleteVoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ GetVoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces display metadata while leaving voice samples unchanged.
func (operation EditVoiceMetadataOperation) Invoke(call sdkgo.Call, input EditVoiceMetadataInput) sdkgo.MutationAttempt[EditedVoiceMetadata] {
	if !voiceIDPattern.MatchString(input.VoiceID) || !validVoiceDisplayText(input.Name, 256, true) || !validVoiceDisplayText(input.Description, 1000, false) {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchDefect, sdkgo.FailureValidation, "voice ID, name, or description is invalid")
	}
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	if err := writer.WriteField("name", input.Name); err != nil {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchDefect, sdkgo.FailureLocalDefect, "voice edit body could not be built")
	}
	if input.Description != "" {
		if err := writer.WriteField("description", input.Description); err != nil {
			return editVoiceMetadataFailure(EditVoiceMetadataBranchDefect, sdkgo.FailureLocalDefect, "voice edit body could not be built")
		}
	}
	if err := writer.Close(); err != nil {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchDefect, sdkgo.FailureLocalDefect, "voice edit body could not be built")
	}
	body, status, retryAfter, err := operation.client.doWithContentType(call, http.MethodPost, "/v1/voices/"+input.VoiceID+"/edit", nil, payload.Bytes(), writer.FormDataContentType())
	if errors.Is(err, errCredential) {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if errors.Is(err, errRequest) {
		return editVoiceMetadataRetry(sdkgo.FailureTransport, "voice edit request failed", 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		kind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return editVoiceMetadataRetry(kind, "voice edit is unavailable", retryAfter)
	}
	if status == http.StatusNotFound {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchNotFound, sdkgo.FailureNotFound, "voice is unavailable")
	}
	if status != http.StatusOK {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchProviderRejected, sdkgo.FailureProviderRejection, "ElevenLabs rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "voice edit confirmation exceeded the response limit")
	}
	if err != nil {
		return editVoiceMetadataRetry(sdkgo.FailureTransport, "voice edit response was interrupted", 0)
	}
	var wire struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Status != "ok" {
		return editVoiceMetadataFailure(EditVoiceMetadataBranchInvalidResponse, sdkgo.FailureProtocol, "voice edit confirmation is invalid")
	}
	return sdkgo.NewMutationBranch(EditVoiceMetadataBranchEdited, EditedVoiceMetadata{VoiceID: input.VoiceID, Name: input.Name}, nil, sdkgo.Receipt{})
}

// Invoke permanently deletes one voice; a missing voice is already absent.
func (operation DeleteVoiceOperation) Invoke(call sdkgo.Call, input GetVoiceInput) sdkgo.MutationAttempt[DeletedVoice] {
	if !voiceIDPattern.MatchString(input.VoiceID) {
		return deleteVoiceFailure(DeleteVoiceBranchDefect, sdkgo.FailureValidation, "voice ID is invalid")
	}
	body, status, retryAfter, err := operation.client.do(call, http.MethodDelete, "/v1/voices/"+input.VoiceID, nil, nil)
	if errors.Is(err, errCredential) {
		return deleteVoiceFailure(DeleteVoiceBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if errors.Is(err, errRequest) {
		return deleteVoiceRetry(sdkgo.FailureTransport, "voice deletion request failed", 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		kind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return deleteVoiceRetry(kind, "voice deletion is unavailable", retryAfter)
	}
	if status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteVoiceBranchDeleted, DeletedVoice{VoiceID: input.VoiceID, Deleted: true}, nil, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return deleteVoiceFailure(DeleteVoiceBranchProviderRejected, sdkgo.FailureProviderRejection, "ElevenLabs rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return deleteVoiceFailure(DeleteVoiceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "voice deletion confirmation exceeded the response limit")
	}
	if err != nil {
		return deleteVoiceRetry(sdkgo.FailureTransport, "voice deletion response was interrupted", 0)
	}
	var wire struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Status != "ok" {
		return deleteVoiceFailure(DeleteVoiceBranchInvalidResponse, sdkgo.FailureProtocol, "voice deletion confirmation is invalid")
	}
	return sdkgo.NewMutationBranch(DeleteVoiceBranchDeleted, DeletedVoice{VoiceID: input.VoiceID, Deleted: true}, nil, sdkgo.Receipt{})
}

func validVoiceDisplayText(value string, maximum int, required bool) bool {
	if required && strings.TrimSpace(value) == "" || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func editVoiceMetadataFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EditedVoiceMetadata] {
	failure := queryFailure("editVoiceMetadata", kind, message)
	return sdkgo.NewMutationBranch(branch, EditedVoiceMetadata{}, &failure, sdkgo.Receipt{})
}
func editVoiceMetadataRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[EditedVoiceMetadata] {
	return sdkgo.NewMutationRetry[EditedVoiceMetadata](queryFailure("editVoiceMetadata", kind, message), retryAfter)
}
func deleteVoiceFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedVoice] {
	failure := queryFailure("deleteVoice", kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedVoice{}, &failure, sdkgo.Receipt{})
}
func deleteVoiceRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[DeletedVoice] {
	return sdkgo.NewMutationRetry[DeletedVoice](queryFailure("deleteVoice", kind, message), retryAfter)
}
