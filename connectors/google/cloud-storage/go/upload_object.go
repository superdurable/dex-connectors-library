// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UploadObjectInput supplies a destination and bounded Base64 content.
type UploadObjectInput struct {
	// Bucket is the destination bucket name shown in Cloud Storage Buckets.
	Bucket string `json:"bucket"`
	// Name is the exact destination object name to create if absent.
	Name string `json:"name"`
	// ContentBase64 is the complete object content encoded as Base64.
	ContentBase64 string `json:"contentBase64"`
	// ContentType is a valid HTTP media type, such as text/plain.
	ContentType string `json:"contentType"`
}

// UploadObjectOperation creates one object conditionally.
type UploadObjectOperation struct{ client *Client }

// UploadObject binds the conditional object upload Mutation.
func (client *Client) UploadObject() UploadObjectOperation { return UploadObjectOperation{client} }

// Definition returns the generated object upload Mutation contract.
func (UploadObjectOperation) Definition() sdkgo.MutationDefinition { return UploadObjectDefinition }

// IdempotencyKey supplies a stable Dex call identity; the generation precondition prevents duplicate writes.
func (UploadObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UploadObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an absent object and verifies matching content after a precondition failure.
func (operation UploadObjectOperation) Invoke(call sdkgo.Call, input UploadObjectInput) sdkgo.MutationAttempt[Object] {
	if !bucketNamePattern.MatchString(input.Bucket) || !validObjectName(input.Name) || input.ContentType == "" || len(input.ContentBase64) > base64.StdEncoding.EncodedLen(int(operation.client.maxUploadBytes))+4 {
		return uploadFailure(UploadObjectBranchDefect, sdkgo.FailureValidation, "object upload input is invalid")
	}
	mediaType, _, err := mime.ParseMediaType(input.ContentType)
	if err != nil || mediaType == "" || !providerhttp.IsHeaderSafeCredential(input.ContentType) {
		return uploadFailure(UploadObjectBranchDefect, sdkgo.FailureValidation, "object content type is invalid")
	}
	content, err := base64.StdEncoding.Strict().DecodeString(input.ContentBase64)
	if err != nil || int64(len(content)) > operation.client.maxUploadBytes {
		return uploadFailure(UploadObjectBranchDefect, sdkgo.FailureValidation, "object content is invalid or too large")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return uploadFailure(UploadObjectBranchDefect, sdkgo.FailureAuthentication, "connection requires reauthorization")
	}
	if err != nil {
		return uploadRetry(sdkgo.FailureTransport, "connection credentials could not be refreshed", 0)
	}
	if credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return uploadFailure(UploadObjectBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	query := url.Values{"uploadType": {"media"}, "name": {input.Name}, "ifGenerationMatch": {"0"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/upload/storage/v1/b/"+url.PathEscape(input.Bucket)+"/o?"+query.Encode(), bytes.NewReader(content))
	if err != nil {
		return uploadFailure(UploadObjectBranchDefect, sdkgo.FailureLocalDefect, "object request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", input.ContentType)
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return uploadRetry(sdkgo.FailureTransport, "Google Cloud Storage upload failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusPreconditionFailed {
		return operation.confirmExistingObject(call, input, content)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return uploadRetry(sdkgo.FailureRateLimit, "Google rate limited the upload", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return uploadRetry(sdkgo.FailureAvailability, "Google Cloud Storage upload is temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return uploadFailure(UploadObjectBranchProviderRejected, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil || bytes.Contains(responseBody, []byte(credentials.AccessToken.Reveal())) {
		return uploadRetry(sdkgo.FailureTransport, "Google upload response could not be read safely", 0)
	}
	var object Object
	if err := json.Unmarshal(responseBody, &object); err != nil || object.Name != input.Name || !generationPattern.MatchString(object.Generation) {
		return uploadRetry(sdkgo.FailureProtocol, "Google returned invalid upload metadata; checking object on retry", 0)
	}
	return sdkgo.NewMutationBranch(UploadObjectBranchUploaded, object, nil, sdkgo.Receipt{})
}

func (operation UploadObjectOperation) confirmExistingObject(call sdkgo.Call, input UploadObjectInput, content []byte) sdkgo.MutationAttempt[Object] {
	path := "/storage/v1/b/" + url.PathEscape(input.Bucket) + "/o/" + url.PathEscape(input.Name)
	metadataResponse := operation.client.request(call, path, false)
	if metadataResponse.err != nil || metadataResponse.status == http.StatusTooManyRequests || metadataResponse.status >= 500 {
		return uploadRetry(sdkgo.FailureAvailability, "matching object metadata could not be confirmed", metadataResponse.retryAfter)
	}
	if metadataResponse.status != http.StatusOK {
		return uploadRetry(sdkgo.FailureAvailability, "matching object changed during confirmation", 0)
	}
	var object Object
	if err := json.Unmarshal(metadataResponse.body, &object); err != nil || object.Name != input.Name || !generationPattern.MatchString(object.Generation) {
		return uploadRetry(sdkgo.FailureProtocol, "Google returned invalid object metadata", 0)
	}
	mediaResponse := operation.client.request(call, path+"?alt=media&generation="+url.QueryEscape(object.Generation), true)
	if mediaResponse.status == http.StatusNotFound {
		return uploadRetry(sdkgo.FailureAvailability, "object changed during upload confirmation", 0)
	}
	if errors.Is(mediaResponse.err, providerhttp.ErrBodyTooLarge) {
		return uploadFailure(UploadObjectBranchObjectExists, sdkgo.FailureProviderRejection, "an object with different content already exists")
	}
	if mediaResponse.err != nil || mediaResponse.status == http.StatusTooManyRequests || mediaResponse.status >= 500 {
		return uploadRetry(sdkgo.FailureAvailability, "existing object could not be confirmed", mediaResponse.retryAfter)
	}
	if mediaResponse.status != http.StatusOK {
		return uploadFailure(UploadObjectBranchProviderRejected, sdkgo.FailureProviderRejection, "Google denied object confirmation")
	}
	if !bytes.Equal(mediaResponse.body, content) {
		return uploadFailure(UploadObjectBranchObjectExists, sdkgo.FailureProviderRejection, "an object with different content already exists")
	}
	return sdkgo.NewMutationBranch(UploadObjectBranchUploaded, object, nil, sdkgo.Receipt{})
}

func uploadFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Object] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: "uploadObject", Message: message}
	return sdkgo.NewMutationBranch(branch, Object{}, &failure, sdkgo.Receipt{})
}

func uploadRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[Object] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: "uploadObject", Message: message}
	return sdkgo.NewMutationRetry[Object](failure, delay)
}
