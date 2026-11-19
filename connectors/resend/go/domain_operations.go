// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateDomainInput sets selected absolute domain settings; nil fields are omitted.
type UpdateDomainInput struct {
	// ID is the domain ID from Resend's Domains page.
	ID string `json:"id"`
	// OpenTracking sets whether email opens are tracked.
	OpenTracking *bool `json:"openTracking,omitempty"`
	// ClickTracking sets whether link clicks are tracked.
	ClickTracking *bool `json:"clickTracking,omitempty"`
	// TrackingSubdomain is the desired DNS label for tracking links.
	TrackingSubdomain *string `json:"trackingSubdomain,omitempty"`
	// TLS is opportunistic or enforced.
	TLS *string `json:"tls,omitempty"`
	// Sending is enabled or disabled; nil preserves the current capability.
	Sending *string `json:"sending,omitempty"`
	// Receiving is enabled or disabled; nil preserves the current capability.
	Receiving *string `json:"receiving,omitempty"`
}

// VerifyDomainInput selects a domain for DNS verification.
type VerifyDomainInput struct {
	// ID is the domain ID from Resend's Domains page.
	ID string `json:"id"`
}

// UpdateDomainOperation changes domain settings.
type UpdateDomainOperation struct{ client *Client }

// VerifyDomainOperation starts a domain verification cycle.
type VerifyDomainOperation struct{ client *Client }

// UpdateDomain binds domain updates to this client.
func (client *Client) UpdateDomain() UpdateDomainOperation { return UpdateDomainOperation{client} }

// VerifyDomain binds domain verification to this client.
func (client *Client) VerifyDomain() VerifyDomainOperation { return VerifyDomainOperation{client} }

// Definition returns the generated updateDomain contract.
func (UpdateDomainOperation) Definition() sdkgo.MutationDefinition { return UpdateDomainDefinition }

// Definition returns the generated verifyDomain contract.
func (VerifyDomainOperation) Definition() sdkgo.MutationDefinition { return VerifyDomainDefinition }

// IdempotencyKey correlates absolute domain setting updates.
func (UpdateDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless verification restarts.
func (VerifyDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ VerifyDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets selected absolute tracking, TLS, and capability settings.
func (operation UpdateDomainOperation) Invoke(call sdkgo.Call, input UpdateDomainInput) sdkgo.MutationAttempt[DomainReference] {
	if strings.TrimSpace(input.ID) == "" || (input.OpenTracking == nil && input.ClickTracking == nil && input.TrackingSubdomain == nil && input.TLS == nil && input.Sending == nil && input.Receiving == nil) {
		return mutationFailure[DomainReference]("updateDomain", UpdateDomainBranchDefect, sdkgo.FailureValidation, "domain ID and at least one setting are required")
	}
	if (input.TLS != nil && *input.TLS != "opportunistic" && *input.TLS != "enforced") || (input.TrackingSubdomain != nil && strings.TrimSpace(*input.TrackingSubdomain) == "") || !validDomainCapability(input.Sending) || !validDomainCapability(input.Receiving) || (input.Sending != nil && input.Receiving != nil && *input.Sending == "disabled" && *input.Receiving == "disabled") {
		return mutationFailure[DomainReference]("updateDomain", UpdateDomainBranchDefect, sdkgo.FailureValidation, "domain settings are invalid")
	}
	body, err := json.Marshal(input.updateBody())
	if err != nil {
		return mutationFailure[DomainReference]("updateDomain", UpdateDomainBranchDefect, sdkgo.FailureValidation, "domain settings cannot be encoded")
	}
	return runMutation(operation.client, call, "updateDomain", http.MethodPatch, "/domains/"+url.PathEscape(input.ID), body, "", "", UpdateDomainBranchUpdated, UpdateDomainBranchNotFound, UpdateDomainBranchProviderRejected, UpdateDomainBranchInvalidResponse, UpdateDomainBranchDefect, decodeDomainReference)
}

// Invoke starts or restarts asynchronous DNS verification.
func (operation VerifyDomainOperation) Invoke(call sdkgo.Call, input VerifyDomainInput) sdkgo.MutationAttempt[DomainReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DomainReference]("verifyDomain", VerifyDomainBranchDefect, sdkgo.FailureValidation, "domain ID is required")
	}
	return runMutation(operation.client, call, "verifyDomain", http.MethodPost, "/domains/"+url.PathEscape(input.ID)+"/verify", nil, "", "", VerifyDomainBranchVerificationStarted, VerifyDomainBranchNotFound, VerifyDomainBranchProviderRejected, VerifyDomainBranchInvalidResponse, VerifyDomainBranchDefect, decodeDomainReference)
}

func (input UpdateDomainInput) updateBody() map[string]any {
	fields := make(map[string]any)
	if input.OpenTracking != nil {
		fields["open_tracking"] = *input.OpenTracking
	}
	if input.ClickTracking != nil {
		fields["click_tracking"] = *input.ClickTracking
	}
	if input.TrackingSubdomain != nil {
		fields["tracking_subdomain"] = *input.TrackingSubdomain
	}
	if input.TLS != nil {
		fields["tls"] = *input.TLS
	}
	capabilities := make(map[string]string)
	if input.Sending != nil {
		capabilities["sending"] = *input.Sending
	}
	if input.Receiving != nil {
		capabilities["receiving"] = *input.Receiving
	}
	if len(capabilities) > 0 {
		fields["capabilities"] = capabilities
	}
	return fields
}

func validDomainCapability(value *string) bool {
	return value == nil || *value == "enabled" || *value == "disabled"
}
