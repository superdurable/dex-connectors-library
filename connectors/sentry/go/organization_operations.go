// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sentry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateOrganizationInput creates an organization for the authenticated Sentry user.
type CreateOrganizationInput struct {
	// Slug is the new globally unique organization slug.
	Slug string `json:"slug"`
	// Name is the organization display name.
	Name string `json:"name"`
	// AgreeTerms confirms the user agrees to Sentry's terms and privacy policy.
	AgreeTerms bool `json:"agreeTerms"`
}

// GetOrganizationInput selects an organization.
type GetOrganizationInput struct {
	// Slug is the organization slug.
	Slug string `json:"slug"`
}

// UpdateOrganizationInput replaces an organization's slug and name.
type UpdateOrganizationInput struct {
	// Slug is the current organization slug.
	Slug string `json:"slug"`
	// NewSlug is the desired organization slug.
	NewSlug string `json:"newSlug"`
	// Name is the desired display name.
	Name string `json:"name"`
}

// CreateOrganizationOperation creates an organization with bounded resend.
type CreateOrganizationOperation struct{ client *Client }

// GetOrganizationOperation retrieves an organization.
type GetOrganizationOperation struct{ client *Client }

// UpdateOrganizationOperation updates an organization.
type UpdateOrganizationOperation struct{ client *Client }

// CreateOrganization binds organization creation to this client.
func (client *Client) CreateOrganization() CreateOrganizationOperation {
	return CreateOrganizationOperation{client}
}

// GetOrganization binds organization retrieval to this client.
func (client *Client) GetOrganization() GetOrganizationOperation {
	return GetOrganizationOperation{client}
}

// UpdateOrganization binds organization updates to this client.
func (client *Client) UpdateOrganization() UpdateOrganizationOperation {
	return UpdateOrganizationOperation{client}
}

// Definition returns the generated createOrganization contract.
func (CreateOrganizationOperation) Definition() sdkgo.MutationDefinition {
	return CreateOrganizationDefinition
}

// Definition returns the generated getOrganization contract.
func (GetOrganizationOperation) Definition() sdkgo.QueryDefinition { return GetOrganizationDefinition }

// Definition returns the generated updateOrganization contract.
func (UpdateOrganizationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateOrganizationDefinition
}

// IdempotencyKey correlates possible create sends.
func (CreateOrganizationOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrganizationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute updates.
func (UpdateOrganizationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOrganizationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an organization with a stable slug.
func (operation CreateOrganizationOperation) Invoke(call sdkgo.Call, input CreateOrganizationInput) sdkgo.MutationAttempt[Organization] {
	if !validSentrySegments(input.Slug, input.Name) || !input.AgreeTerms {
		return mutationFailure[Organization]("createOrganization", CreateOrganizationBranchDefect, sdkgo.FailureValidation, "slug, name, and agreement to terms are required")
	}
	body, err := json.Marshal(struct {
		Slug       string `json:"slug"`
		Name       string `json:"name"`
		AgreeTerms bool   `json:"agreeTerms"`
	}{input.Slug, input.Name, input.AgreeTerms})
	if err != nil {
		return mutationFailure[Organization]("createOrganization", CreateOrganizationBranchDefect, sdkgo.FailureValidation, "organization cannot be encoded")
	}
	request := sentryLimitedCreate[Organization]{client: operation.client, call: call, operation: "createOrganization", path: "/organizations/", body: body, timeout: CreateOrganizationDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateOrganizationBranchCreated, rejected: CreateOrganizationBranchProviderRejected, defect: CreateOrganizationBranchDefect, decode: func(body []byte) (Organization, error) { return decodeOrganizationSlug(body, input.Slug) }, readBack: func(call sdkgo.Call) (Organization, readBackState) {
		return operation.readBackOrganization(call, input)
	}}
	return request.invoke()
}

// Invoke retrieves an organization by slug.
func (operation GetOrganizationOperation) Invoke(call sdkgo.Call, input GetOrganizationInput) sdkgo.QueryAttempt[Organization] {
	if !validSentrySegments(input.Slug) {
		return queryFailure[Organization]("getOrganization", GetOrganizationBranchDefect, sdkgo.FailureValidation, "organization slug is required")
	}
	return runQuery(operation.client, call, "getOrganization", organizationPath(input.Slug), GetOrganizationBranchFound, GetOrganizationBranchNotFound, GetOrganizationBranchProviderRejected, GetOrganizationBranchInvalidResponse, GetOrganizationBranchDefect, func(body []byte) (Organization, error) { return decodeOrganizationSlug(body, input.Slug) })
}

// Invoke replaces an organization's slug and name.
func (operation UpdateOrganizationOperation) Invoke(call sdkgo.Call, input UpdateOrganizationInput) sdkgo.MutationAttempt[Organization] {
	if !validSentrySegments(input.Slug, input.NewSlug, input.Name) {
		return mutationFailure[Organization]("updateOrganization", UpdateOrganizationBranchDefect, sdkgo.FailureValidation, "current slug, new slug, and name are required")
	}
	body, err := json.Marshal(struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}{input.NewSlug, input.Name})
	if err != nil {
		return mutationFailure[Organization]("updateOrganization", UpdateOrganizationBranchDefect, sdkgo.FailureValidation, "organization update cannot be encoded")
	}
	return runMutation(operation.client, call, "updateOrganization", http.MethodPut, organizationPath(input.Slug), body, "", "", UpdateOrganizationBranchUpdated, UpdateOrganizationBranchNotFound, UpdateOrganizationBranchProviderRejected, UpdateOrganizationBranchInvalidResponse, UpdateOrganizationBranchDefect, func(body []byte) (Organization, error) { return decodeOrganizationSlug(body, input.NewSlug) })
}

func (operation CreateOrganizationOperation) readBackOrganization(call sdkgo.Call, input CreateOrganizationInput) (Organization, readBackState) {
	body, state := operation.client.readBackResource(call, organizationPath(input.Slug))
	if state != readBackMatched {
		return Organization{}, state
	}
	organization, err := decodeOrganizationSlug(body, input.Slug)
	if err != nil {
		return Organization{}, readBackRetry
	}
	if organization.Name != input.Name {
		return Organization{}, readBackConflicted
	}
	return organization, readBackMatched
}

func decodeOrganizationSlug(body []byte, slug string) (Organization, error) {
	var organization Organization
	if err := json.Unmarshal(body, &organization); err != nil {
		return Organization{}, err
	}
	if strings.TrimSpace(organization.ID) == "" || organization.Slug != slug || strings.TrimSpace(organization.Name) == "" {
		return Organization{}, fmt.Errorf("organization identity is missing or differs from the requested slug")
	}
	return organization, nil
}

func organizationPath(slug string) string { return "/organizations/" + url.PathEscape(slug) + "/" }
