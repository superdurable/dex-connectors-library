// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updatePagePropertiesOperationID    = "updatePageProperties"
	updatePagePropertiesFailureSubject = "page update"
)

// UpdatePagePropertiesInput sets property values on one page. Properties left
// out keep their values; every listed value replaces the property's whole
// value, so repeating the same input leaves the page unchanged.
type UpdatePagePropertiesInput struct {
	// PageID is the page ID, with or without dashes, or the page's Notion URL.
	PageID string `json:"pageId"`
	// Properties maps property names or IDs to typed values, 1 to 100. Use
	// ClearedValue to empty a property.
	Properties map[string]PropertyValue `json:"properties"`
}

// UpdatePagePropertiesOutput is the page after the update.
type UpdatePagePropertiesOutput struct {
	// Page is the page Notion returned after applying the values; it is empty on other branches.
	Page Page `json:"page"`
}

// UpdatePagePropertiesOperation implements the updatePageProperties Mutation.
type UpdatePagePropertiesOperation struct{ client *Client }

type updatePageRequestBody struct {
	Properties map[string]any `json:"properties"`
}

// Definition returns the immutable connector operation definition.
func (UpdatePagePropertiesOperation) Definition() sdkgo.MutationDefinition {
	return UpdatePagePropertiesDefinition
}

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Notion accepts no idempotency key; the absolute values make a repeated update converge instead.
func (UpdatePagePropertiesOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePagePropertiesInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PATCH of absolute property values. Because sending it again leaves the same
// state, transport failures, 408, 409, 429, 529, and 5xx, including a 503 after Notion saved, are retried.
func (operation UpdatePagePropertiesOperation) Invoke(call sdkgo.Call, input UpdatePagePropertiesInput) sdkgo.MutationAttempt[UpdatePagePropertiesOutput] {
	client := operation.client
	pageID, body, err := buildUpdatePageRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchDefect, UpdatePagePropertiesOutput{},
			validationFailure(updatePagePropertiesOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, sessionFailure := client.startSession(call, updatePagePropertiesOperationID, writeOperationDeadline)
	if sessionFailure != nil {
		return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchDefect, UpdatePagePropertiesOutput{}, sessionFailure, sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, notionRequest{
		method: http.MethodPatch, path: "/pages/" + url.PathEscape(pageID), payload: body, timeout: writeRequestTimeout,
	})
	classification := client.classifyRepeatableWrite(updatePagePropertiesOperationID, updatePagePropertiesFailureSubject, result)
	receipt := client.receipt(session, result.response, pageID)
	switch classification.outcome {
	case writeAccepted:
	case writeRetry:
		return sdkgo.NewMutationRetry[UpdatePagePropertiesOutput](classification.failure, classification.retryAfter)
	case writeNotFound:
		return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchNotFound, UpdatePagePropertiesOutput{}, &classification.failure, receipt)
	case writeRejected:
		return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchProviderRejected, UpdatePagePropertiesOutput{}, &classification.failure, receipt)
	case writeDefect:
		return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchDefect, UpdatePagePropertiesOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchInvalidResponse, UpdatePagePropertiesOutput{}, &classification.failure, receipt)
	}
	page, err := decodePageBody(result.response.body)
	if err != nil || page.ID != pageID {
		return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchInvalidResponse, UpdatePagePropertiesOutput{},
			failurePointer(updatePagePropertiesOperationID, sdkgo.FailureProtocol, "Notion applied the page update but returned an unusable page"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdatePagePropertiesBranchUpdated, UpdatePagePropertiesOutput{Page: page}, nil, receipt)
}

func buildUpdatePageRequest(input UpdatePagePropertiesInput) (string, updatePageRequestBody, error) {
	pageID, err := parseFieldID(input.PageID, "pageId")
	if err != nil {
		return "", updatePageRequestBody{}, err
	}
	if len(input.Properties) == 0 {
		return "", updatePageRequestBody{}, errors.New("properties needs at least one value")
	}
	properties, err := encodePropertyValues(input.Properties, "properties")
	if err != nil {
		return "", updatePageRequestBody{}, err
	}
	return pageID, updatePageRequestBody{Properties: properties}, nil
}
