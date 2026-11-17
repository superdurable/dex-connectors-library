// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetFeatureInput identifies a Productboard feature.
type GetFeatureInput struct {
	// FeatureID is the UUID shown in Productboard's feature URL or API response.
	FeatureID string `json:"featureId"`
}

// RenameFeatureInput replaces a feature name with a new absolute value.
type RenameFeatureInput struct {
	// FeatureID is the UUID shown in Productboard's feature URL or API response.
	FeatureID string `json:"featureId"`
	// Name is the new nonempty feature name.
	Name string `json:"name"`
}

// DeleteFeatureInput identifies a feature to remove permanently.
type DeleteFeatureInput struct {
	// FeatureID is the UUID shown in Productboard's feature URL or API response.
	FeatureID string `json:"featureId"`
}

// DeletedFeature confirms which feature ID was deleted.
type DeletedFeature struct {
	// ID is the deleted Productboard entity ID.
	ID string `json:"id"`
}

// GetFeatureOperation reads one feature without including other configured fields.
type GetFeatureOperation struct{ client *Client }

// GetFeature binds the Query to this client.
func (client *Client) GetFeature() GetFeatureOperation { return GetFeatureOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetFeatureOperation) Definition() sdkgo.QueryDefinition { return GetFeatureDefinition }

// Invoke reads the feature and classifies definite and retryable failures.
func (operation GetFeatureOperation) Invoke(call sdkgo.Call, input GetFeatureInput) sdkgo.QueryAttempt[FeatureMetadata] {
	if !validFeatureID(input.FeatureID) {
		return featureQueryBranch(GetFeatureBranchDefect, sdkgo.FailureValidation, "feature ID must be a UUID")
	}
	response := operation.client.get(call, "/entities/"+url.PathEscape(input.FeatureID)+"?fields%5B%5D=name")
	if errors.Is(response.err, errCredential) {
		return featureQueryBranch(GetFeatureBranchDefect, sdkgo.FailureAuthentication, "Productboard credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return featureQueryBranch(GetFeatureBranchNotFound, sdkgo.FailureNotFound, "feature was not found")
	}
	if retryKind, isRetry := featureRetryKind(response); isRetry {
		return sdkgo.NewQueryRetry[FeatureMetadata](featureFailure("getFeature", retryKind, "Productboard feature lookup is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return featureQueryBranch(GetFeatureBranchProviderRejected, sdkgo.FailureProviderRejection, "Productboard rejected the feature lookup")
	}
	if response.err != nil {
		return featureQueryBranch(GetFeatureBranchInvalidResponse, sdkgo.FailureProtocol, "Productboard feature response could not be read")
	}
	feature, isValid := decodeFeature(response.body)
	if !isValid || feature.ID != input.FeatureID {
		return featureQueryBranch(GetFeatureBranchInvalidResponse, sdkgo.FailureProtocol, "Productboard returned an invalid feature")
	}
	return sdkgo.NewQueryBranch(GetFeatureBranchFound, feature, nil, sdkgo.Receipt{})
}

// RenameFeatureOperation replaces a feature's name, so repeating the same request is safe.
type RenameFeatureOperation struct{ client *Client }

// RenameFeature binds the Mutation to this client.
func (client *Client) RenameFeature() RenameFeatureOperation { return RenameFeatureOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (RenameFeatureOperation) Definition() sdkgo.MutationDefinition { return RenameFeatureDefinition }

// IdempotencyKey identifies retries of the same rename.
func (RenameFeatureOperation) IdempotencyKey(callID sdkgo.CallID, _ RenameFeatureInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the name and classifies the provider response.
func (operation RenameFeatureOperation) Invoke(call sdkgo.Call, input RenameFeatureInput) sdkgo.MutationAttempt[FeatureMetadata] {
	if !validFeatureID(input.FeatureID) || !validFeatureName(input.Name) {
		return featureMutationBranch(RenameFeatureBranchDefect, "renameFeature", sdkgo.FailureValidation, "feature ID or name is invalid", FeatureMetadata{})
	}
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{"fields": map[string]string{"name": input.Name}}})
	response := operation.client.request(call, http.MethodPatch, "/entities/"+url.PathEscape(input.FeatureID), payload)
	if errors.Is(response.err, errCredential) {
		return featureMutationBranch(RenameFeatureBranchDefect, "renameFeature", sdkgo.FailureAuthentication, "Productboard credentials are unavailable", FeatureMetadata{})
	}
	if response.status == http.StatusNotFound {
		return featureMutationBranch(RenameFeatureBranchNotFound, "renameFeature", sdkgo.FailureNotFound, "feature was not found", FeatureMetadata{})
	}
	if retryKind, isRetry := featureRetryKind(response); isRetry {
		return sdkgo.NewMutationRetry[FeatureMetadata](featureFailure("renameFeature", retryKind, "Productboard rename is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return featureMutationBranch(RenameFeatureBranchProviderRejected, "renameFeature", sdkgo.FailureProviderRejection, "Productboard rejected the feature rename", FeatureMetadata{})
	}
	if response.err != nil {
		return featureMutationBranch(RenameFeatureBranchInvalidResponse, "renameFeature", sdkgo.FailureProtocol, "Productboard rename response could not be read", FeatureMetadata{})
	}
	featureID, isValid := decodeCreatedFeatureID(response.body)
	if !isValid || featureID != input.FeatureID {
		return featureMutationBranch(RenameFeatureBranchInvalidResponse, "renameFeature", sdkgo.FailureProtocol, "Productboard returned an invalid feature", FeatureMetadata{})
	}
	return sdkgo.NewMutationBranch(RenameFeatureBranchRenamed, FeatureMetadata{ID: featureID, Name: input.Name}, nil, sdkgo.Receipt{})
}

// DeleteFeatureOperation removes a feature and its subfeatures.
type DeleteFeatureOperation struct{ client *Client }

// DeleteFeature binds the Mutation to this client.
func (client *Client) DeleteFeature() DeleteFeatureOperation { return DeleteFeatureOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (DeleteFeatureOperation) Definition() sdkgo.MutationDefinition { return DeleteFeatureDefinition }

// IdempotencyKey identifies retries of the same delete.
func (DeleteFeatureOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteFeatureInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one feature and reports its stable ID.
func (operation DeleteFeatureOperation) Invoke(call sdkgo.Call, input DeleteFeatureInput) sdkgo.MutationAttempt[DeletedFeature] {
	if !validFeatureID(input.FeatureID) {
		return featureMutationBranch(DeleteFeatureBranchDefect, "deleteFeature", sdkgo.FailureValidation, "feature ID must be a UUID", DeletedFeature{})
	}
	response := operation.client.request(call, http.MethodDelete, "/entities/"+url.PathEscape(input.FeatureID), nil)
	if errors.Is(response.err, errCredential) {
		return featureMutationBranch(DeleteFeatureBranchDefect, "deleteFeature", sdkgo.FailureAuthentication, "Productboard credentials are unavailable", DeletedFeature{})
	}
	if response.status == http.StatusNotFound {
		return featureMutationBranch(DeleteFeatureBranchNotFound, "deleteFeature", sdkgo.FailureNotFound, "feature was not found", DeletedFeature{})
	}
	if retryKind, isRetry := featureRetryKind(response); isRetry {
		return sdkgo.NewMutationRetry[DeletedFeature](featureFailure("deleteFeature", retryKind, "Productboard deletion is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusNoContent || response.err != nil {
		return featureMutationBranch(DeleteFeatureBranchProviderRejected, "deleteFeature", sdkgo.FailureProviderRejection, "Productboard rejected the feature delete", DeletedFeature{})
	}
	return sdkgo.NewMutationBranch(DeleteFeatureBranchDeleted, DeletedFeature{ID: input.FeatureID}, nil, sdkgo.Receipt{})
}

func validFeatureID(featureID string) bool {
	if len(featureID) != 36 {
		return false
	}
	for index, character := range featureID {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

func validFeatureName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 500 && validCursor(name)
}

func decodeFeature(body []byte) (FeatureMetadata, bool) {
	var wire struct {
		Data struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Fields struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data.Type != "feature" || wire.Data.ID == "" || wire.Data.Fields.Name == "" {
		return FeatureMetadata{}, false
	}
	return FeatureMetadata{ID: wire.Data.ID, Name: wire.Data.Fields.Name}, true
}

func featureRetryKind(response providerResponse) (sdkgo.FailureKind, bool) {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.FailureRateLimit, true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.FailureAvailability, true
	}
	if response.err != nil && response.status == 0 {
		return sdkgo.FailureTransport, true
	}
	return "", false
}

func featureFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "productboard", Operation: operation, Message: message}
}

func featureQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[FeatureMetadata] {
	failure := featureFailure("getFeature", kind, message)
	return sdkgo.NewQueryBranch(branch, FeatureMetadata{}, &failure, sdkgo.Receipt{})
}

func featureMutationBranch[T any](branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string, zero T) sdkgo.MutationAttempt[T] {
	failure := featureFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}
