// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateObjectMetadataInput supplies absolute object metadata values.
type UpdateObjectMetadataInput struct {
	// Bucket is the bucket name shown in Cloud Storage Buckets.
	Bucket string `json:"bucket"`
	// Name is the object name returned by listObjects.
	Name string `json:"name"`
	// ContentType is an optional HTTP media type to set; blank preserves the current type.
	ContentType string `json:"contentType,omitempty"`
	// Metadata supplies non-secret custom metadata key-value pairs to set.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DeleteObjectInput selects an object to delete.
type DeleteObjectInput struct {
	// Bucket is the bucket name shown in Cloud Storage Buckets.
	Bucket string `json:"bucket"`
	// Name is the object name returned by listObjects.
	Name string `json:"name"`
	// Generation optionally restricts deletion to this immutable object version.
	Generation string `json:"generation,omitempty"`
}

// DeletedObject identifies an object that is absent after deletion.
type DeletedObject struct {
	// Bucket is the bucket name.
	Bucket string `json:"bucket"`
	// Name is the object name.
	Name string `json:"name"`
}

// UpdateObjectMetadataOperation changes selected metadata fields.
type UpdateObjectMetadataOperation struct{ client *Client }

// DeleteObjectOperation deletes an object.
type DeleteObjectOperation struct{ client *Client }

// UpdateObjectMetadata binds the object metadata Mutation.
func (client *Client) UpdateObjectMetadata() UpdateObjectMetadataOperation {
	return UpdateObjectMetadataOperation{client}
}

// DeleteObject binds the object deletion Mutation.
func (client *Client) DeleteObject() DeleteObjectOperation { return DeleteObjectOperation{client} }

// Definition returns the generated object metadata contract.
func (UpdateObjectMetadataOperation) Definition() sdkgo.MutationDefinition {
	return UpdateObjectMetadataDefinition
}

// Definition returns the generated object deletion contract.
func (DeleteObjectOperation) Definition() sdkgo.MutationDefinition { return DeleteObjectDefinition }

// IdempotencyKey identifies one metadata update across retries.
func (UpdateObjectMetadataOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateObjectMetadataInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies one deletion across retries.
func (DeleteObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets selected object metadata values without replacing object content.
func (operation UpdateObjectMetadataOperation) Invoke(call sdkgo.Call, input UpdateObjectMetadataInput) sdkgo.MutationAttempt[Object] {
	if !bucketNamePattern.MatchString(input.Bucket) || !validObjectName(input.Name) || (input.ContentType == "" && len(input.Metadata) == 0) || len(input.Metadata) > 128 {
		return objectMutationBranch("updateObjectMetadata", "defect", Object{}, sdkgo.FailureValidation, "object metadata input is invalid")
	}
	if input.ContentType != "" {
		if mediaType, _, err := mime.ParseMediaType(input.ContentType); err != nil || mediaType == "" || strings.ContainsAny(input.ContentType, "\r\n") {
			return objectMutationBranch("updateObjectMetadata", "defect", Object{}, sdkgo.FailureValidation, "object content type is invalid")
		}
	}
	for key, value := range input.Metadata {
		if key == "" || len(key) > 128 || len(value) > 1024 || strings.ContainsAny(key+value, "\r\n") {
			return objectMutationBranch("updateObjectMetadata", "defect", Object{}, sdkgo.FailureValidation, "object metadata entry is invalid")
		}
	}
	requestBody, err := json.Marshal(struct {
		ContentType string            `json:"contentType,omitempty"`
		Metadata    map[string]string `json:"metadata,omitempty"`
	}{ContentType: input.ContentType, Metadata: input.Metadata})
	if err != nil {
		return objectMutationBranch("updateObjectMetadata", "defect", Object{}, sdkgo.FailureLocalDefect, "object metadata request could not be encoded")
	}
	path := "/storage/v1/b/" + url.PathEscape(input.Bucket) + "/o/" + url.PathEscape(input.Name)
	response := operation.client.requestWithBody(call, call.Context, http.MethodPatch, path, requestBody, false)
	if errors.Is(response.err, errCredential) {
		return objectMutationBranch("updateObjectMetadata", "defect", Object{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if response.status == http.StatusNotFound && response.err == nil {
		return objectMutationBranch("updateObjectMetadata", "notFound", Object{}, sdkgo.FailureProviderRejection, "object was not found")
	}
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return objectMutationRetry("updateObjectMetadata", sdkgo.FailureAvailability, "object metadata update is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return objectMutationBranch("updateObjectMetadata", "providerRejected", Object{}, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
	var object Object
	if err := json.Unmarshal(response.body, &object); err != nil || object.Name != input.Name || !generationPattern.MatchString(object.Generation) {
		return objectMutationRetry("updateObjectMetadata", sdkgo.FailureProtocol, "Google returned invalid object metadata", 0)
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("updated"), object, nil, sdkgo.Receipt{})
}

// Invoke deletes an object and accepts an already absent object as complete.
func (operation DeleteObjectOperation) Invoke(call sdkgo.Call, input DeleteObjectInput) sdkgo.MutationAttempt[DeletedObject] {
	if !bucketNamePattern.MatchString(input.Bucket) || !validObjectName(input.Name) || (input.Generation != "" && !generationPattern.MatchString(input.Generation)) {
		return objectDeleteBranch("defect", DeletedObject{}, sdkgo.FailureValidation, "object deletion input is invalid")
	}
	path := "/storage/v1/b/" + url.PathEscape(input.Bucket) + "/o/" + url.PathEscape(input.Name)
	if input.Generation != "" {
		path += "?generation=" + url.QueryEscape(input.Generation)
	}
	response := operation.client.requestMethod(call, http.MethodDelete, path, false)
	if errors.Is(response.err, errCredential) {
		return objectDeleteBranch("defect", DeletedObject{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if response.err != nil {
		return objectDeleteRetry(sdkgo.FailureTransport, "object deletion response failed", 0)
	}
	switch response.status {
	case http.StatusNoContent, http.StatusNotFound:
		return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), DeletedObject{Bucket: input.Bucket, Name: input.Name}, nil, sdkgo.Receipt{})
	case http.StatusTooManyRequests:
		return objectDeleteRetry(sdkgo.FailureRateLimit, "Google rate limited the deletion", response.retryAfter)
	case http.StatusRequestTimeout, http.StatusServiceUnavailable:
		return objectDeleteRetry(sdkgo.FailureAvailability, "object deletion is temporarily unavailable", response.retryAfter)
	default:
		if response.status >= 500 {
			return objectDeleteRetry(sdkgo.FailureAvailability, "object deletion is temporarily unavailable", response.retryAfter)
		}
		return objectDeleteBranch("providerRejected", DeletedObject{}, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
}

func objectMutationBranch(operation string, branch sdkgo.BranchID, value Object, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Object] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func objectMutationRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[Object] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: operation, Message: message}
	return sdkgo.NewMutationRetry[Object](failure, delay)
}

func objectDeleteBranch(branch sdkgo.BranchID, value DeletedObject, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedObject] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: "deleteObject", Message: message}
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func objectDeleteRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedObject] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: "deleteObject", Message: message}
	return sdkgo.NewMutationRetry[DeletedObject](failure, delay)
}
