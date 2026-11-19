// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateContactPropertyInput creates a typed contact property.
type CreateContactPropertyInput struct {
	// Key is an alphanumeric or underscore identifier, at most 50 characters.
	Key string `json:"key"`
	// Type is string or number and is immutable after creation.
	Type string `json:"type"`
	// FallbackValue is an optional string or number matching Type.
	FallbackValue any `json:"fallbackValue,omitempty"`
}

// GetContactPropertyInput selects a contact property.
type GetContactPropertyInput struct {
	// ID is the property ID.
	ID string `json:"id"`
}

// ListContactPropertiesInput selects a bounded property page.
type ListContactPropertiesInput struct {
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive property ID cursor.
	After string `json:"after,omitempty"`
}

// UpdateContactPropertyInput replaces a property's fallback value.
type UpdateContactPropertyInput struct {
	// ID is the property ID.
	ID string `json:"id"`
	// FallbackValue is a string or number matching the property's type.
	FallbackValue any `json:"fallbackValue"`
}

// DeleteContactPropertyInput selects a property for deletion.
type DeleteContactPropertyInput struct {
	// ID is the property ID.
	ID string `json:"id"`
}

// ContactPropertyReference identifies a created or updated property.
type ContactPropertyReference struct {
	// ID is the stable provider property ID.
	ID string `json:"id"`
}

// ContactProperty contains a typed property definition.
type ContactProperty struct {
	// ID is the stable provider property ID.
	ID string `json:"id"`
	// Key is the contact property identifier.
	Key string `json:"key"`
	// Type is string or number.
	Type string `json:"type"`
	// FallbackValue is the provider's default string or number.
	FallbackValue any `json:"fallback_value"`
}

// ContactPropertyPage contains one property page.
type ContactPropertyPage struct {
	// Data contains property definitions.
	Data []ContactProperty `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedContactProperty records a provider deletion acknowledgement.
type DeletedContactProperty struct {
	// ID is the deleted property ID.
	ID string `json:"id"`
	// Deleted confirms that deletion succeeded.
	Deleted bool `json:"deleted"`
}

// CreateContactPropertyOperation creates a property with bounded resend.
type CreateContactPropertyOperation struct{ client *Client }

// GetContactPropertyOperation retrieves one property.
type GetContactPropertyOperation struct{ client *Client }

// ListContactPropertiesOperation lists properties.
type ListContactPropertiesOperation struct{ client *Client }

// UpdateContactPropertyOperation replaces a fallback value.
type UpdateContactPropertyOperation struct{ client *Client }

// DeleteContactPropertyOperation removes a property.
type DeleteContactPropertyOperation struct{ client *Client }

// CreateContactProperty binds property creation to this client.
func (client *Client) CreateContactProperty() CreateContactPropertyOperation {
	return CreateContactPropertyOperation{client}
}

// GetContactProperty binds property retrieval to this client.
func (client *Client) GetContactProperty() GetContactPropertyOperation {
	return GetContactPropertyOperation{client}
}

// ListContactProperties binds property listing to this client.
func (client *Client) ListContactProperties() ListContactPropertiesOperation {
	return ListContactPropertiesOperation{client}
}

// UpdateContactProperty binds fallback replacement to this client.
func (client *Client) UpdateContactProperty() UpdateContactPropertyOperation {
	return UpdateContactPropertyOperation{client}
}

// DeleteContactProperty binds property deletion to this client.
func (client *Client) DeleteContactProperty() DeleteContactPropertyOperation {
	return DeleteContactPropertyOperation{client}
}

// Definition returns the generated createContactProperty contract.
func (CreateContactPropertyOperation) Definition() sdkgo.MutationDefinition {
	return CreateContactPropertyDefinition
}

// Definition returns the generated getContactProperty contract.
func (GetContactPropertyOperation) Definition() sdkgo.QueryDefinition {
	return GetContactPropertyDefinition
}

// Definition returns the generated listContactProperties contract.
func (ListContactPropertiesOperation) Definition() sdkgo.QueryDefinition {
	return ListContactPropertiesDefinition
}

// Definition returns the generated updateContactProperty contract.
func (UpdateContactPropertyOperation) Definition() sdkgo.MutationDefinition {
	return UpdateContactPropertyDefinition
}

// Definition returns the generated deleteContactProperty contract.
func (DeleteContactPropertyOperation) Definition() sdkgo.MutationDefinition {
	return DeleteContactPropertyDefinition
}

// IdempotencyKey correlates possible property creation attempts.
func (CreateContactPropertyOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactPropertyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute fallback replacements.
func (UpdateContactPropertyOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactPropertyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeat deletions.
func (DeleteContactPropertyOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactPropertyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a typed contact property with read-back by key.
func (operation CreateContactPropertyOperation) Invoke(call sdkgo.Call, input CreateContactPropertyInput) sdkgo.MutationAttempt[ContactPropertyReference] {
	if !validContactPropertyKey(input.Key) || !oneOf(input.Type, "string", "number") || !validPropertyFallback(input.Type, input.FallbackValue) {
		return mutationFailure[ContactPropertyReference]("createContactProperty", CreateContactPropertyBranchDefect, sdkgo.FailureValidation, "property key, type, or fallback is invalid")
	}
	fields := map[string]any{"key": input.Key, "type": input.Type}
	if input.FallbackValue != nil {
		fields["fallback_value"] = input.FallbackValue
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[ContactPropertyReference]("createContactProperty", CreateContactPropertyBranchDefect, sdkgo.FailureValidation, "property cannot be encoded")
	}
	request := resendLimitedCreate[ContactPropertyReference]{client: operation.client, call: call, operation: "createContactProperty", path: "/contact-properties", body: body, timeout: CreateContactPropertyDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateContactPropertyBranchCreated, rejected: CreateContactPropertyBranchProviderRejected, defect: CreateContactPropertyBranchDefect, decode: decodeContactPropertyReference, readBack: func(call sdkgo.Call) (ContactPropertyReference, readBackState) {
		return operation.readBackProperty(call, input)
	}}
	return request.invoke()
}

// Invoke retrieves a contact property by ID.
func (operation GetContactPropertyOperation) Invoke(call sdkgo.Call, input GetContactPropertyInput) sdkgo.QueryAttempt[ContactProperty] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[ContactProperty]("getContactProperty", GetContactPropertyBranchDefect, sdkgo.FailureValidation, "property ID is required")
	}
	return runQuery(operation.client, call, "getContactProperty", "/contact-properties/"+url.PathEscape(input.ID), GetContactPropertyBranchFound, GetContactPropertyBranchNotFound, GetContactPropertyBranchProviderRejected, GetContactPropertyBranchInvalidResponse, GetContactPropertyBranchDefect, decodeContactProperty)
}

// Invoke lists one contact property page.
func (operation ListContactPropertiesOperation) Invoke(call sdkgo.Call, input ListContactPropertiesInput) sdkgo.QueryAttempt[ContactPropertyPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[ContactPropertyPage]("listContactProperties", ListContactPropertiesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listContactProperties", "/contact-properties"+query, ListContactPropertiesBranchListed, "", ListContactPropertiesBranchProviderRejected, ListContactPropertiesBranchInvalidResponse, ListContactPropertiesBranchDefect, decodeContactPropertyPage)
}

// Invoke replaces a property's fallback value.
func (operation UpdateContactPropertyOperation) Invoke(call sdkgo.Call, input UpdateContactPropertyInput) sdkgo.MutationAttempt[ContactPropertyReference] {
	if strings.TrimSpace(input.ID) == "" || !validPropertyFallback("", input.FallbackValue) || input.FallbackValue == nil {
		return mutationFailure[ContactPropertyReference]("updateContactProperty", UpdateContactPropertyBranchDefect, sdkgo.FailureValidation, "property ID and string or number fallback are required")
	}
	body, err := json.Marshal(map[string]any{"fallback_value": input.FallbackValue})
	if err != nil {
		return mutationFailure[ContactPropertyReference]("updateContactProperty", UpdateContactPropertyBranchDefect, sdkgo.FailureValidation, "fallback cannot be encoded")
	}
	return runMutation(operation.client, call, "updateContactProperty", http.MethodPatch, "/contact-properties/"+url.PathEscape(input.ID), body, "", "", UpdateContactPropertyBranchUpdated, UpdateContactPropertyBranchNotFound, UpdateContactPropertyBranchProviderRejected, UpdateContactPropertyBranchInvalidResponse, UpdateContactPropertyBranchDefect, decodeContactPropertyReference)
}

// Invoke deletes a contact property by ID.
func (operation DeleteContactPropertyOperation) Invoke(call sdkgo.Call, input DeleteContactPropertyInput) sdkgo.MutationAttempt[DeletedContactProperty] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedContactProperty]("deleteContactProperty", DeleteContactPropertyBranchDefect, sdkgo.FailureValidation, "property ID is required")
	}
	return runMutation(operation.client, call, "deleteContactProperty", http.MethodDelete, "/contact-properties/"+url.PathEscape(input.ID), nil, "", "", DeleteContactPropertyBranchDeleted, DeleteContactPropertyBranchNotFound, DeleteContactPropertyBranchProviderRejected, DeleteContactPropertyBranchInvalidResponse, DeleteContactPropertyBranchDefect, decodeDeletedContactProperty)
}
func (operation CreateContactPropertyOperation) readBackProperty(call sdkgo.Call, input CreateContactPropertyInput) (ContactPropertyReference, readBackState) {
	cursor := ""
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/contact-properties"+query)
		if state != readBackMatched {
			return ContactPropertyReference{}, readBackRetry
		}
		page, err := decodeContactPropertyPage(body)
		if err != nil {
			return ContactPropertyReference{}, readBackRetry
		}
		for _, property := range page.Data {
			if property.Key == input.Key {
				if property.Type != input.Type {
					return ContactPropertyReference{}, readBackConflicted
				}
				expected, expectedErr := json.Marshal(input.FallbackValue)
				actual, actualErr := json.Marshal(property.FallbackValue)
				if expectedErr != nil || actualErr != nil {
					return ContactPropertyReference{}, readBackRetry
				}
				if !bytes.Equal(expected, actual) {
					return ContactPropertyReference{}, readBackConflicted
				}
				return ContactPropertyReference{ID: property.ID}, readBackMatched
			}
		}
		if !page.HasMore {
			return ContactPropertyReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return ContactPropertyReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return ContactPropertyReference{}, readBackRetry
}
func validContactPropertyKey(key string) bool {
	if len(key) == 0 || len(key) > 50 {
		return false
	}
	for _, character := range key {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_') {
			return false
		}
	}
	return true
}
func validPropertyFallback(propertyType string, value any) bool {
	if value == nil {
		return true
	}
	switch value.(type) {
	case string:
		return propertyType == "" || propertyType == "string"
	case float64, float32, int, int64, int32:
		return propertyType == "" || propertyType == "number"
	default:
		return false
	}
}
func decodeContactPropertyReference(body []byte) (ContactPropertyReference, error) {
	var value ContactPropertyReference
	err := json.Unmarshal(body, &value)
	if err == nil && value.ID == "" {
		err = fmt.Errorf("property ID missing")
	}
	return value, err
}
func decodeContactProperty(body []byte) (ContactProperty, error) {
	var value ContactProperty
	err := json.Unmarshal(body, &value)
	if err == nil && (value.ID == "" || value.Key == "" || value.Type == "") {
		err = fmt.Errorf("property fields missing")
	}
	return value, err
}
func decodeContactPropertyPage(body []byte) (ContactPropertyPage, error) {
	var page ContactPropertyPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("property page data missing")
	}
	return page, err
}
func decodeDeletedContactProperty(body []byte) (DeletedContactProperty, error) {
	var value DeletedContactProperty
	err := json.Unmarshal(body, &value)
	if err == nil && (!value.Deleted || value.ID == "") {
		err = fmt.Errorf("property deletion acknowledgement missing")
	}
	return value, err
}
