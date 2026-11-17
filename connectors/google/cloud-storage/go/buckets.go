// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,127}$`)
var projectNumberPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var bucketLabelPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

// Bucket identifies a Google Cloud Storage bucket without exposing its policy or ACL.
type Bucket struct {
	// Name is the globally unique bucket name.
	Name string `json:"name"`
	// Location is the Google Cloud storage location.
	Location string `json:"location"`
	// ProjectNumber is the numeric owner project when Google includes it.
	ProjectNumber string `json:"projectNumber,omitempty"`
}

// BucketsPage contains one page of buckets and its next cursor.
type BucketsPage struct {
	// Buckets are the returned bucket identities.
	Buckets []Bucket `json:"buckets"`
	// NextPageToken is blank after the last page.
	NextPageToken string `json:"nextPageToken"`
}

// ListBucketsInput selects a page of buckets from one project.
type ListBucketsInput struct {
	// Project is the Google Cloud project ID or number from the console project selector.
	Project string `json:"project"`
	// Prefix optionally restricts the bucket names returned.
	Prefix string `json:"prefix,omitempty"`
	// PageSize is 1 through 1000; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is an opaque cursor from a prior page; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// GetBucketInput selects a bucket by its globally unique name.
type GetBucketInput struct {
	// Name is the bucket name shown under Cloud Storage Buckets.
	Name string `json:"name"`
}

// CreateBucketInput specifies a globally unique bucket in one project.
type CreateBucketInput struct {
	// ProjectNumber is the numeric project number from Cloud Console Project Settings.
	ProjectNumber string `json:"projectNumber"`
	// Name is a globally unique bucket name selected under Cloud Storage Buckets.
	Name string `json:"name"`
	// Location is a Google Cloud location such as US; blank uses Google's location default.
	Location string `json:"location,omitempty"`
}

// UpdateBucketLabelsInput sets label values on one bucket.
type UpdateBucketLabelsInput struct {
	// Name is the bucket name shown under Cloud Storage Buckets.
	Name string `json:"name"`
	// Labels maps Google Cloud label keys to new non-secret values.
	Labels map[string]string `json:"labels"`
}

// DeleteBucketInput selects an empty bucket to delete.
type DeleteBucketInput struct {
	// Name is the bucket name shown under Cloud Storage Buckets.
	Name string `json:"name"`
}

// DeletedBucket reports the bucket name after a successful or already-complete deletion.
type DeletedBucket struct {
	// Name is the now-absent bucket name.
	Name string `json:"name"`
}

// ListBucketsOperation reads a project bucket page.
type ListBucketsOperation struct{ client *Client }

// GetBucketOperation reads one bucket.
type GetBucketOperation struct{ client *Client }

// CreateBucketOperation creates one project bucket.
type CreateBucketOperation struct{ client *Client }

// UpdateBucketLabelsOperation changes bucket label values.
type UpdateBucketLabelsOperation struct{ client *Client }

// DeleteBucketOperation deletes an empty bucket.
type DeleteBucketOperation struct{ client *Client }

// ListBuckets binds the bucket listing Query.
func (client *Client) ListBuckets() ListBucketsOperation { return ListBucketsOperation{client} }

// GetBucket binds the bucket metadata Query.
func (client *Client) GetBucket() GetBucketOperation { return GetBucketOperation{client} }

// CreateBucket binds the bucket creation Mutation.
func (client *Client) CreateBucket() CreateBucketOperation { return CreateBucketOperation{client} }

// UpdateBucketLabels binds the bucket label Mutation.
func (client *Client) UpdateBucketLabels() UpdateBucketLabelsOperation {
	return UpdateBucketLabelsOperation{client}
}

// DeleteBucket binds the bucket deletion Mutation.
func (client *Client) DeleteBucket() DeleteBucketOperation { return DeleteBucketOperation{client} }

// Definition returns the generated bucket listing contract.
func (ListBucketsOperation) Definition() sdkgo.QueryDefinition { return ListBucketsDefinition }

// Definition returns the generated bucket metadata contract.
func (GetBucketOperation) Definition() sdkgo.QueryDefinition { return GetBucketDefinition }

// Definition returns the generated bucket creation contract.
func (CreateBucketOperation) Definition() sdkgo.MutationDefinition { return CreateBucketDefinition }

// Definition returns the generated bucket label contract.
func (UpdateBucketLabelsOperation) Definition() sdkgo.MutationDefinition {
	return UpdateBucketLabelsDefinition
}

// Definition returns the generated bucket deletion contract.
func (DeleteBucketOperation) Definition() sdkgo.MutationDefinition { return DeleteBucketDefinition }

// IdempotencyKey uses the stable Dex call identity for deletion retries.
func (DeleteBucketOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteBucketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies this bucket creation across retries.
func (CreateBucketOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateBucketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies this label update across retries.
func (UpdateBucketLabelsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateBucketLabelsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists bucket identities from the selected project.
func (operation ListBucketsOperation) Invoke(call sdkgo.Call, input ListBucketsInput) sdkgo.QueryAttempt[BucketsPage] {
	if !projectIDPattern.MatchString(input.Project) || input.PageSize < 0 || input.PageSize > 1000 || !validObjectNamePrefix(input.Prefix) || !validPageToken(input.PageToken) {
		return queryBranch("listBuckets", "defect", BucketsPage{}, sdkgo.FailureValidation, "bucket page input is invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	query := url.Values{"project": {input.Project}, "maxResults": {strconv.Itoa(pageSize)}}
	if input.Prefix != "" {
		query.Set("prefix", input.Prefix)
	}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.request(call, "/storage/v1/b?"+query.Encode(), false)
	if decision, failed := classifyRead[BucketsPage](response, "listBuckets"); failed {
		return decision
	}
	var wire struct {
		Items         []Bucket `json:"items"`
		NextPageToken string   `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || len(wire.Items) > pageSize || !validPageToken(wire.NextPageToken) {
		return queryBranch("listBuckets", "invalidResponse", BucketsPage{}, sdkgo.FailureProtocol, "Google returned an invalid bucket page")
	}
	for _, bucket := range wire.Items {
		if !bucketNamePattern.MatchString(bucket.Name) || bucket.Location == "" {
			return queryBranch("listBuckets", "invalidResponse", BucketsPage{}, sdkgo.FailureProtocol, "Google returned an invalid bucket identity")
		}
	}
	return sdkgo.NewQueryBranch(ListBucketsBranchListed, BucketsPage{Buckets: wire.Items, NextPageToken: wire.NextPageToken}, nil, sdkgo.Receipt{})
}

// Invoke reads one bucket's metadata.
func (operation GetBucketOperation) Invoke(call sdkgo.Call, input GetBucketInput) sdkgo.QueryAttempt[Bucket] {
	if !bucketNamePattern.MatchString(input.Name) {
		return queryBranch("getBucket", "defect", Bucket{}, sdkgo.FailureValidation, "bucket name is invalid")
	}
	response := operation.client.request(call, "/storage/v1/b/"+url.PathEscape(input.Name), false)
	if response.status == http.StatusNotFound && response.err == nil {
		return queryBranch("getBucket", "notFound", Bucket{}, sdkgo.FailureProviderRejection, "bucket was not found")
	}
	if decision, failed := classifyRead[Bucket](response, "getBucket"); failed {
		return decision
	}
	var bucket Bucket
	if err := json.Unmarshal(response.body, &bucket); err != nil || bucket.Name != input.Name || bucket.Location == "" {
		return queryBranch("getBucket", "invalidResponse", Bucket{}, sdkgo.FailureProtocol, "Google returned invalid bucket metadata")
	}
	return sdkgo.NewQueryBranch(GetBucketBranchFound, bucket, nil, sdkgo.Receipt{})
}

// Invoke creates a bucket and reconciles retries by its globally unique name and numeric project.
func (operation CreateBucketOperation) Invoke(call sdkgo.Call, input CreateBucketInput) sdkgo.MutationAttempt[Bucket] {
	if !projectNumberPattern.MatchString(input.ProjectNumber) || !bucketNamePattern.MatchString(input.Name) || len(input.Location) > 63 {
		return bucketMutationBranch("createBucket", "defect", Bucket{}, sdkgo.FailureValidation, "bucket creation input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateBucketDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return bucketMutationBranch("createBucket", "defect", Bucket{}, sdkgo.FailureLocalDefect, "bucket dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return bucketCreateUncertain("an earlier bucket creation may have succeeded")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		bucket, isConfirmed, isInconclusive := operation.readBackCreatedBucket(call, input)
		if isConfirmed {
			return sdkgo.NewMutationBranch(CreateBucketBranchCreated, bucket, nil, sdkgo.Receipt{})
		}
		if isInconclusive || decision.Kind == singledispatch.ReadBackRequired {
			if decision.Kind == singledispatch.ReadBackRequired {
				return bucketCreateUncertain("bucket creation cannot be confirmed after the send cap")
			}
			return bucketMutationRetry("createBucket", sdkgo.FailureAvailability, "bucket creation cannot yet be confirmed", 0)
		}
	}
	requestBody, err := json.Marshal(struct {
		Name     string `json:"name"`
		Location string `json:"location,omitempty"`
	}{Name: input.Name, Location: input.Location})
	if err != nil {
		return bucketMutationBranch("createBucket", "defect", Bucket{}, sdkgo.FailureLocalDefect, "bucket request could not be encoded")
	}
	path := "/storage/v1/b?project=" + url.QueryEscape(input.ProjectNumber)
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestWithBody(call, requestContext, http.MethodPost, path, requestBody, false)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return bucketMutationRetry("createBucket", sdkgo.FailureAvailability, "bucket creation was not dispatched", 0)
	}
	if errors.Is(err, errCredential) {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return bucketMutationRetry("createBucket", sdkgo.FailureAvailability, "credential failure could not be checkpointed", 0)
		}
		return bucketMutationBranch("createBucket", "defect", Bucket{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return bucketMutationRetry("createBucket", sdkgo.FailureAvailability, "bucket retry could not be checkpointed", 0)
		}
		return bucketMutationRetry("createBucket", sdkgo.FailureRateLimit, "Google deferred bucket creation", response.retryAfter)
	}
	if response.status == http.StatusConflict && response.err == nil {
		bucket, isConfirmed, isInconclusive := operation.readBackCreatedBucket(call, input)
		if isConfirmed {
			return sdkgo.NewMutationBranch(CreateBucketBranchCreated, bucket, nil, sdkgo.Receipt{})
		}
		if isInconclusive {
			return bucketMutationRetry("createBucket", sdkgo.FailureAvailability, "bucket ownership could not be checked", 0)
		}
		return bucketMutationBranch("createBucket", "nameTaken", Bucket{}, sdkgo.FailureProviderRejection, "bucket name is already in use")
	}
	if response.err != nil || response.status >= 500 || response.status == http.StatusRequestTimeout {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return bucketCreateUncertain("bucket creation outcome is unknown after the send cap")
		}
		return bucketMutationRetry("createBucket", sdkgo.FailureAvailability, "bucket creation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return bucketMutationBranch("createBucket", "providerRejected", Bucket{}, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
	var bucket Bucket
	if err := json.Unmarshal(response.body, &bucket); err != nil || bucket.Name != input.Name || bucket.ProjectNumber != input.ProjectNumber {
		return bucketMutationRetry("createBucket", sdkgo.FailureProtocol, "Google returned invalid bucket creation metadata", 0)
	}
	return sdkgo.NewMutationBranch(CreateBucketBranchCreated, bucket, nil, sdkgo.Receipt{})
}

func (operation CreateBucketOperation) readBackCreatedBucket(call sdkgo.Call, input CreateBucketInput) (Bucket, bool, bool) {
	response := operation.client.request(call, "/storage/v1/b/"+url.PathEscape(input.Name), false)
	if response.status == http.StatusNotFound && response.err == nil {
		return Bucket{}, false, false
	}
	if response.err != nil || response.status != http.StatusOK {
		return Bucket{}, false, true
	}
	var bucket Bucket
	if err := json.Unmarshal(response.body, &bucket); err != nil || bucket.Name != input.Name {
		return Bucket{}, false, true
	}
	if bucket.ProjectNumber != input.ProjectNumber {
		return Bucket{}, false, false
	}
	return bucket, true, false
}

func bucketCreateUncertain(message string) sdkgo.MutationAttempt[Bucket] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "google-cloud-storage", Operation: "createBucket", Message: message}
	return sdkgo.NewMutationUncertain(Bucket{}, failure, sdkgo.Receipt{})
}

// Invoke sets explicit label values with a repeatable patch.
func (operation UpdateBucketLabelsOperation) Invoke(call sdkgo.Call, input UpdateBucketLabelsInput) sdkgo.MutationAttempt[Bucket] {
	if !bucketNamePattern.MatchString(input.Name) || len(input.Labels) == 0 || len(input.Labels) > 64 {
		return bucketMutationBranch("updateBucketLabels", "defect", Bucket{}, sdkgo.FailureValidation, "bucket labels input is invalid")
	}
	for key, value := range input.Labels {
		if !bucketLabelPattern.MatchString(key) || len(value) > 63 {
			return bucketMutationBranch("updateBucketLabels", "defect", Bucket{}, sdkgo.FailureValidation, "bucket label is invalid")
		}
	}
	requestBody, err := json.Marshal(struct {
		Labels map[string]string `json:"labels"`
	}{Labels: input.Labels})
	if err != nil {
		return bucketMutationBranch("updateBucketLabels", "defect", Bucket{}, sdkgo.FailureLocalDefect, "bucket label request could not be encoded")
	}
	response := operation.client.requestWithBody(call, call.Context, http.MethodPatch, "/storage/v1/b/"+url.PathEscape(input.Name), requestBody, false)
	if errors.Is(response.err, errCredential) {
		return bucketMutationBranch("updateBucketLabels", "defect", Bucket{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if response.status == http.StatusNotFound && response.err == nil {
		return bucketMutationBranch("updateBucketLabels", "notFound", Bucket{}, sdkgo.FailureProviderRejection, "bucket was not found")
	}
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return bucketMutationRetry("updateBucketLabels", sdkgo.FailureAvailability, "bucket label update is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return bucketMutationBranch("updateBucketLabels", "providerRejected", Bucket{}, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
	var bucket Bucket
	if err := json.Unmarshal(response.body, &bucket); err != nil || bucket.Name != input.Name {
		return bucketMutationRetry("updateBucketLabels", sdkgo.FailureProtocol, "Google returned invalid bucket metadata", 0)
	}
	return sdkgo.NewMutationBranch(UpdateBucketLabelsBranchUpdated, bucket, nil, sdkgo.Receipt{})
}

func bucketMutationBranch(operation string, branch sdkgo.BranchID, value Bucket, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Bucket] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func bucketMutationRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[Bucket] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: operation, Message: message}
	return sdkgo.NewMutationRetry[Bucket](failure, delay)
}

// Invoke deletes an empty bucket, including one already absent on a prior attempt.
func (operation DeleteBucketOperation) Invoke(call sdkgo.Call, input DeleteBucketInput) sdkgo.MutationAttempt[DeletedBucket] {
	if !bucketNamePattern.MatchString(input.Name) {
		return bucketDeleteBranch(DeleteBucketBranchDefect, DeletedBucket{}, sdkgo.FailureValidation, "bucket name is invalid")
	}
	response := operation.client.requestMethod(call, http.MethodDelete, "/storage/v1/b/"+url.PathEscape(input.Name), false)
	if errors.Is(response.err, errCredential) {
		return bucketDeleteBranch(DeleteBucketBranchDefect, DeletedBucket{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if response.err != nil {
		return bucketDeleteRetry(sdkgo.FailureTransport, "Google Cloud Storage deletion failed", 0)
	}
	switch response.status {
	case http.StatusNoContent, http.StatusNotFound:
		return sdkgo.NewMutationBranch(DeleteBucketBranchDeleted, DeletedBucket{Name: input.Name}, nil, sdkgo.Receipt{})
	case http.StatusTooManyRequests:
		return bucketDeleteRetry(sdkgo.FailureRateLimit, "Google rate limited the deletion", response.retryAfter)
	case http.StatusRequestTimeout, http.StatusServiceUnavailable:
		return bucketDeleteRetry(sdkgo.FailureAvailability, "Google Cloud Storage deletion is temporarily unavailable", response.retryAfter)
	default:
		if response.status >= 500 {
			return bucketDeleteRetry(sdkgo.FailureAvailability, "Google Cloud Storage deletion is temporarily unavailable", response.retryAfter)
		}
		return bucketDeleteBranch(DeleteBucketBranchProviderRejected, DeletedBucket{}, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status))
	}
}

func bucketDeleteBranch(branch sdkgo.BranchID, value DeletedBucket, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedBucket] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: "deleteBucket", Message: message}
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func bucketDeleteRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedBucket] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: "deleteBucket", Message: message}
	return sdkgo.NewMutationRetry[DeletedBucket](failure, delay)
}

