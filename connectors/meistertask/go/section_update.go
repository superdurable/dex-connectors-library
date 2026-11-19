// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateSectionInput replaces selected section fields.
type UpdateSectionInput struct {
	// SectionID is the positive section ID returned by MeisterTask.
	SectionID string `json:"sectionId"`
	// Name replaces the section name when provided; blank is invalid.
	Name *string `json:"name,omitempty"`
	// Status is 1 active or 2 trashed.
	Status *int `json:"status,omitempty"`
}

// UpdateSectionOperation replaces selected section fields.
type UpdateSectionOperation struct{ client *Client }

// UpdateSection binds the Mutation to this client.
func (client *Client) UpdateSection() UpdateSectionOperation { return UpdateSectionOperation{client} }

// Definition returns the generated section Mutation contract.
func (UpdateSectionOperation) Definition() sdkgo.MutationDefinition { return UpdateSectionDefinition }

// IdempotencyKey supplies a stable call identity for the absolute update.
func (UpdateSectionOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSectionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one absolute section update.
func (operation UpdateSectionOperation) Invoke(call sdkgo.Call, input UpdateSectionInput) sdkgo.MutationAttempt[SectionSummary] {
	if !validTaskID(input.SectionID) || (input.Name == nil && input.Status == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.Status != nil && *input.Status != 1 && *input.Status != 2) {
		return sectionMutationBranch("updateSection", UpdateSectionBranchDefect, SectionSummary{}, sdkgo.FailureValidation, "section ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name   *string `json:"name,omitempty"`
		Status *int    `json:"status,omitempty"`
	}{input.Name, input.Status})
	if err != nil {
		return sectionMutationBranch("updateSection", UpdateSectionBranchDefect, SectionSummary{}, sdkgo.FailureLocalDefect, "section update could not be encoded")
	}
	response := operation.client.put(call, "/sections/"+input.SectionID, payload)
	if errors.Is(response.err, errCredential) {
		return sectionMutationBranch("updateSection", UpdateSectionBranchDefect, SectionSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[SectionSummary](sectionFailure("updateSection", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[SectionSummary](sectionFailure("updateSection", sdkgo.FailureRateLimit, "MeisterTask rate limited the update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[SectionSummary](sectionFailure("updateSection", sdkgo.FailureAvailability, "MeisterTask update outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sectionMutationBranch("updateSection", UpdateSectionBranchNotFound, SectionSummary{}, sdkgo.FailureNotFound, "MeisterTask did not expose the section")
	}
	if response.status != http.StatusOK {
		return sectionMutationBranch("updateSection", UpdateSectionBranchProviderRejected, SectionSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sectionMutationBranch("updateSection", UpdateSectionBranchInvalidResponse, SectionSummary{}, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[SectionSummary](sectionFailure("updateSection", sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0)
	}
	section, err := decodeSection(response.body, input.SectionID)
	if err != nil {
		return sectionMutationBranch("updateSection", UpdateSectionBranchInvalidResponse, SectionSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid section JSON")
	}
	return sdkgo.NewMutationBranch(UpdateSectionBranchUpdated, section, nil, sdkgo.Receipt{})
}

func sectionMutationBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := sectionFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
