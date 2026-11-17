// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// AttributeEnumerationValue defines one category attribute choice.
type AttributeEnumerationValue struct {
	// Value is Brevo's numeric category option value.
	Value int `json:"value"`
	// Label is the visible option label, up to 200 characters.
	Label string `json:"label"`
}

// CreateAttributeInput defines a contact attribute.
type CreateAttributeInput struct {
	// Category is normal, transactional, category, calculated, or global.
	Category string `json:"category"`
	// Name is an uppercase contact attribute name.
	Name string `json:"name"`
	// Type is required for normal, transactional, and category attributes.
	Type string `json:"type,omitempty"`
	// Value is required for calculated and global attributes.
	Value string `json:"value,omitempty"`
	// Options holds normal multiple-choice values, each up to 200 characters.
	Options []string `json:"options,omitempty"`
	// Enumeration holds category choices.
	Enumeration []AttributeEnumerationValue `json:"enumeration,omitempty"`
}

// UpdateAttributeInput replaces options or a calculated value.
type UpdateAttributeInput struct {
	// Category is normal, category, calculated, or global.
	Category string `json:"category"`
	// Name is an uppercase contact attribute name.
	Name string `json:"name"`
	// Options is the complete desired normal multiple-choice option set.
	Options []string `json:"options,omitempty"`
	// Enumeration is the complete desired category option set.
	Enumeration []AttributeEnumerationValue `json:"enumeration,omitempty"`
	// Value replaces the calculated or global formula.
	Value string `json:"value,omitempty"`
}

// DeleteAttributeInput identifies an attribute by category and name.
type DeleteAttributeInput struct {
	// Category is normal, transactional, category, calculated, or global.
	Category string `json:"category"`
	// Name is an uppercase contact attribute name.
	Name string `json:"name"`
}

// AttributeChangeResult identifies the affected contact attribute.
type AttributeChangeResult struct {
	// Category is Brevo's attribute category.
	Category string `json:"category"`
	// Name is the attribute name.
	Name string `json:"name"`
}

// CreateAttributeOperation creates a contact attribute with bounded repeats.
type CreateAttributeOperation struct{ client *Client }

// UpdateAttributeOperation replaces attribute choices or a formula.
type UpdateAttributeOperation struct{ client *Client }

// DeleteAttributeOperation deletes a contact attribute.
type DeleteAttributeOperation struct{ client *Client }

// CreateAttribute binds attribute creation to this client.
func (client *Client) CreateAttribute() CreateAttributeOperation {
	return CreateAttributeOperation{client}
}

// UpdateAttribute binds attribute option updates to this client.
func (client *Client) UpdateAttribute() UpdateAttributeOperation {
	return UpdateAttributeOperation{client}
}

// DeleteAttribute binds attribute deletion to this client.
func (client *Client) DeleteAttribute() DeleteAttributeOperation {
	return DeleteAttributeOperation{client}
}

// Definition returns the generated attribute creation contract.
func (CreateAttributeOperation) Definition() sdkgo.MutationDefinition {
	return CreateAttributeDefinition
}

// Definition returns the generated attribute update contract.
func (UpdateAttributeOperation) Definition() sdkgo.MutationDefinition {
	return UpdateAttributeDefinition
}

// Definition returns the generated attribute deletion contract.
func (DeleteAttributeOperation) Definition() sdkgo.MutationDefinition {
	return DeleteAttributeDefinition
}

// IdempotencyKey preserves the call identity across bounded create sends.
func (CreateAttributeOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAttributeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves the call identity across absolute option updates.
func (UpdateAttributeOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAttributeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves the call identity across deletions.
func (DeleteAttributeOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAttributeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one contact attribute.
func (operation CreateAttributeOperation) Invoke(call sdkgo.Call, input CreateAttributeInput) sdkgo.MutationAttempt[AttributeChangeResult] {
	const name = "createAttribute"
	if !validAttributeCategory(input.Category) || !validAttributeName(input.Name) || !validAttributeCreateFields(input) {
		return brevoMutationBranch(CreateAttributeBranchDefect, AttributeChangeResult{}, name, sdkgo.FailureValidation, "attribute category, name, or body is invalid")
	}
	payload, err := json.Marshal(struct {
		Type        string                      `json:"type,omitempty"`
		Value       string                      `json:"value,omitempty"`
		Options     []string                    `json:"multiCategoryOptions,omitempty"`
		Enumeration []AttributeEnumerationValue `json:"enumeration,omitempty"`
	}{input.Type, input.Value, input.Options, input.Enumeration})
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(CreateAttributeBranchDefect, AttributeChangeResult{}, name, sdkgo.FailureValidation, "attribute body is invalid or too large")
	}
	fence, err := singledispatch.NewFence(call, CreateAttributeDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return brevoMutationBranch(CreateAttributeBranchDefect, AttributeChangeResult{}, name, sdkgo.FailureLocalDefect, "attribute dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(AttributeChangeResult{}, *brevoFailure(name, sdkgo.FailureTransport, "an earlier attribute may have been created"), sdkgo.Receipt{})
	}
	path := "/contacts/attributes/" + input.Category + "/" + url.PathEscape(input.Name)
	request, failure := operation.client.newRequest(call, http.MethodPost, path, payload, name)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateAttributeBranchDefect, AttributeChangeResult{}, failure, sdkgo.Receipt{})
	}
	var response *http.Response
	err = fence.Dispatch(func(ctx context.Context) error {
		response, err = operation.client.httpClient.Do(request.WithContext(ctx))
		return err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[AttributeChangeResult](*brevoFailure(name, sdkgo.FailureAvailability, "attribute dispatch was not recorded or sent"), 0)
	}
	if response != nil {
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			if checkpointError := fence.RecordCanResend(); checkpointError != nil {
				return sdkgo.NewMutationRetry[AttributeChangeResult](*brevoFailure(name, sdkgo.FailureAvailability, "attribute retry checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[AttributeChangeResult](*brevoFailure(name, sdkgo.FailureRateLimit, "Brevo asked to retry attribute creation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		}
	}
	if err != nil || response == nil || isRetryable(response.StatusCode) {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(AttributeChangeResult{}, *brevoFailure(name, sdkgo.FailureTransport, "attribute outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[AttributeChangeResult](*brevoFailure(name, sdkgo.FailureTransport, "attribute outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusCreated {
		return brevoMutationBranch(CreateAttributeBranchProviderRejected, AttributeChangeResult{}, name, failureKind(response.StatusCode), "Brevo rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	return sdkgo.NewMutationBranch(CreateAttributeBranchCreated, AttributeChangeResult{Category: input.Category, Name: input.Name}, nil, sdkgo.Receipt{})
}

// Invoke replaces a supported attribute's choices or formula.
func (operation UpdateAttributeOperation) Invoke(call sdkgo.Call, input UpdateAttributeInput) sdkgo.MutationAttempt[AttributeChangeResult] {
	const name = "updateAttribute"
	if !validAttributeName(input.Name) || !validAttributeUpdateFields(input) {
		return brevoMutationBranch(UpdateAttributeBranchDefect, AttributeChangeResult{}, name, sdkgo.FailureValidation, "attribute category, name, or body is invalid")
	}
	payload, err := json.Marshal(struct {
		Options     []string                    `json:"multiCategoryOptions,omitempty"`
		Enumeration []AttributeEnumerationValue `json:"enumeration,omitempty"`
		Value       string                      `json:"value,omitempty"`
	}{input.Options, input.Enumeration, input.Value})
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(UpdateAttributeBranchDefect, AttributeChangeResult{}, name, sdkgo.FailureValidation, "attribute options are invalid or too large")
	}
	result := AttributeChangeResult{Category: input.Category, Name: input.Name}
	return invokeBrevoNoContentMutation(call, operation.client, name, http.MethodPut, "/contacts/attributes/"+input.Category+"/"+url.PathEscape(input.Name), payload, result, UpdateAttributeBranchUpdated, UpdateAttributeBranchNotFound, UpdateAttributeBranchProviderRejected, UpdateAttributeBranchDefect)
}

// Invoke removes the identified contact attribute.
func (operation DeleteAttributeOperation) Invoke(call sdkgo.Call, input DeleteAttributeInput) sdkgo.MutationAttempt[AttributeChangeResult] {
	const name = "deleteAttribute"
	if !validAttributeCategory(input.Category) || !validAttributeName(input.Name) {
		return brevoMutationBranch(DeleteAttributeBranchDefect, AttributeChangeResult{}, name, sdkgo.FailureValidation, "valid category and uppercase name are required")
	}
	result := AttributeChangeResult{Category: input.Category, Name: input.Name}
	return invokeBrevoNoContentMutation(call, operation.client, name, http.MethodDelete, "/contacts/attributes/"+input.Category+"/"+url.PathEscape(input.Name), nil, result, DeleteAttributeBranchDeleted, DeleteAttributeBranchNotFound, DeleteAttributeBranchProviderRejected, DeleteAttributeBranchDefect)
}

func validAttributeCategory(category string) bool {
	switch category {
	case "normal", "transactional", "category", "calculated", "global":
		return true
	default:
		return false
	}
}

func validAttributeName(name string) bool {
	if name == "" || len(name) > 100 || strings.ToUpper(name) != name {
		return false
	}
	for _, character := range name {
		if character != '_' && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validAttributeCreateFields(input CreateAttributeInput) bool {
	switch input.Category {
	case "normal":
		return input.Type != "" && input.Value == "" && len(input.Enumeration) == 0 && (input.Type == "multiple-choice" || len(input.Options) == 0) && validAttributeOptions(input.Options) && (input.Type != "multiple-choice" || len(input.Options) > 0)
	case "transactional":
		return input.Type != "" && input.Value == "" && len(input.Options) == 0 && len(input.Enumeration) == 0
	case "category":
		return input.Type == "category" && input.Value == "" && len(input.Options) == 0 && validAttributeEnumeration(input.Enumeration)
	case "calculated", "global":
		return input.Type == "" && strings.TrimSpace(input.Value) != "" && len(input.Options) == 0 && len(input.Enumeration) == 0
	default:
		return false
	}
}

func validAttributeUpdateFields(input UpdateAttributeInput) bool {
	switch input.Category {
	case "normal":
		return len(input.Options) > 0 && validAttributeOptions(input.Options) && len(input.Enumeration) == 0 && input.Value == ""
	case "category":
		return validAttributeEnumeration(input.Enumeration) && len(input.Options) == 0 && input.Value == ""
	case "calculated", "global":
		return strings.TrimSpace(input.Value) != "" && len(input.Options) == 0 && len(input.Enumeration) == 0
	default:
		return false
	}
}

func validAttributeOptions(options []string) bool {
	for _, option := range options {
		if strings.TrimSpace(option) == "" || len(option) > 200 {
			return false
		}
	}
	return true
}

func validAttributeEnumeration(values []AttributeEnumerationValue) bool {
	if len(values) == 0 {
		return false
	}
	for _, option := range values {
		if strings.TrimSpace(option.Label) == "" || len(option.Label) > 200 {
			return false
		}
	}
	return true
}
