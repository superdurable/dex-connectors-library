// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	listBucketsOperationID  = "listBuckets"
	createBucketOperationID = "createBucket"
	deleteBucketOperationID = "deleteBucket"
)

// ListBucketsInput selects one bounded page of bucket names.
type ListBucketsInput struct {
	// Prefix filters names by their leading characters; blank lists all names.
	Prefix string `json:"prefix,omitempty"`
	// ContinuationToken is the opaque token from an earlier page; blank starts at the beginning.
	ContinuationToken string `json:"continuationToken,omitempty"`
	// MaxBuckets is the page size from 1 to 10000; zero uses 100.
	MaxBuckets int `json:"maxBuckets,omitempty"`
}

// BucketSummary names a bucket owned by the connection's AWS account.
type BucketSummary struct {
	// Name is the bucket name.
	Name string `json:"name"`
	// CreationDate is the bucket creation time.
	CreationDate time.Time `json:"creationDate"`
	// Region is the bucket's AWS Region when S3 includes it.
	Region string `json:"region,omitempty"`
}

// ListBucketsOutput contains one page and its continuation token.
type ListBucketsOutput struct {
	// Buckets are the bucket summaries in the response.
	Buckets []BucketSummary `json:"buckets"`
	// NextContinuationToken resumes the listing when S3 returned another page.
	NextContinuationToken string `json:"nextContinuationToken,omitempty"`
}

// ListBucketsOperation implements the listBuckets Query.
type ListBucketsOperation struct{ client *Client }

// Definition returns the generated Query contract.
func (ListBucketsOperation) Definition() sdkgo.QueryDefinition { return ListBucketsDefinition }

// Invoke lists one bounded page of buckets owned by the signing account.
func (operation ListBucketsOperation) Invoke(call sdkgo.Call, input ListBucketsInput) sdkgo.QueryAttempt[ListBucketsOutput] {
	if input.MaxBuckets == 0 {
		input.MaxBuckets = 100
	}
	if input.MaxBuckets < 1 || input.MaxBuckets > 10000 || len(input.Prefix) > 63 || len(input.ContinuationToken) > 1024 {
		return sdkgo.NewQueryBranch(ListBucketsBranchDefect, ListBucketsOutput{}, defectFailure(listBucketsOperationID, errors.New("bucket listing parameters are invalid")), sdkgo.Receipt{})
	}
	client := operation.client
	session, cancel, failure := client.startSession(call, listBucketsOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListBucketsBranchDefect, ListBucketsOutput{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	query := url.Values{"max-buckets": {strconv.Itoa(input.MaxBuckets)}}
	if input.Prefix != "" {
		query.Set("prefix", input.Prefix)
	}
	if input.ContinuationToken != "" {
		query.Set("continuation-token", input.ContinuationToken)
	}
	response, err := client.exchange(session, s3Request{method: http.MethodGet, query: query})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewQueryBranch(ListBucketsBranchDefect, ListBucketsOutput{}, failurePointer(listBucketsOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ListBucketsOutput](transportFailure(listBucketsOperationID, "bucket listing"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(listBucketsOperationID, "bucket listing", client.readErrorResponse(response, session.credentials), false)
		return queryAttemptForFailure(classified, client.receipt(call, classified.requestID, objectLocation{}), "", ListBucketsBranchProviderRejected, ListBucketsBranchDefect, ListBucketsOutput{})
	}
	contents, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return sdkgo.NewQueryBranch(ListBucketsBranchInvalidResponse, ListBucketsOutput{}, failurePointer(listBucketsOperationID, sdkgo.FailureResponseTooLarge, "S3 bucket list exceeds maxResponseBytes"), sdkgo.Receipt{})
	}
	if containsCredential(string(contents), session.credentials, true) {
		return sdkgo.NewQueryBranch(ListBucketsBranchInvalidResponse, ListBucketsOutput{}, failurePointer(listBucketsOperationID, sdkgo.FailureProtocol, "S3 bucket list contains connection credentials"), sdkgo.Receipt{})
	}
	var document struct {
		XMLName xml.Name `xml:"ListAllMyBucketsResult"`
		Buckets []struct {
			Name         string    `xml:"Name"`
			CreationDate time.Time `xml:"CreationDate"`
			Region       string    `xml:"BucketRegion"`
		} `xml:"Buckets>Bucket"`
		ContinuationToken string `xml:"ContinuationToken"`
	}
	if xml.Unmarshal(contents, &document) != nil || document.XMLName.Local != "ListAllMyBucketsResult" || len(document.Buckets) > input.MaxBuckets || len(document.ContinuationToken) > 1024 {
		return sdkgo.NewQueryBranch(ListBucketsBranchInvalidResponse, ListBucketsOutput{}, failurePointer(listBucketsOperationID, sdkgo.FailureProtocol, "S3 returned an invalid bucket list"), sdkgo.Receipt{})
	}
	output := ListBucketsOutput{Buckets: make([]BucketSummary, 0, len(document.Buckets)), NextContinuationToken: document.ContinuationToken}
	for _, bucket := range document.Buckets {
		if validateBucketName(bucket.Name) != nil || bucket.CreationDate.IsZero() || (bucket.Region != "" && !regionPattern.MatchString(bucket.Region)) {
			return sdkgo.NewQueryBranch(ListBucketsBranchInvalidResponse, ListBucketsOutput{}, failurePointer(listBucketsOperationID, sdkgo.FailureProtocol, "S3 returned an invalid bucket summary"), sdkgo.Receipt{})
		}
		output.Buckets = append(output.Buckets, BucketSummary{Name: bucket.Name, CreationDate: bucket.CreationDate, Region: bucket.Region})
	}
	return sdkgo.NewQueryBranch(ListBucketsBranchListed, output, nil, client.receipt(call, safeRequestID(response.Header), objectLocation{}))
}

// CreateBucketInput names a new general purpose bucket in the connection's Region.
type CreateBucketInput struct {
	// Bucket is a globally unique bucket name.
	Bucket string `json:"bucket"`
}

// CreatedBucket identifies a bucket accepted by S3.
type CreatedBucket struct {
	// Bucket is the new bucket name.
	Bucket string `json:"bucket"`
	// Region is the Region selected by the connection.
	Region string `json:"region"`
}

// CreateBucketOperation implements bounded creation of a general purpose bucket.
type CreateBucketOperation struct{ client *Client }

// Definition returns the generated Mutation contract.
func (CreateBucketOperation) Definition() sdkgo.MutationDefinition { return CreateBucketDefinition }

// IdempotencyKey keeps the Dex Call identity stable across retries.
func (CreateBucketOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateBucketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a bucket, reading it back after an ambiguous send before another possible send.
func (operation CreateBucketOperation) Invoke(call sdkgo.Call, input CreateBucketInput) sdkgo.MutationAttempt[CreatedBucket] {
	client := operation.client
	if validateBucketName(input.Bucket) != nil {
		return sdkgo.NewMutationBranch(CreateBucketBranchDefect, CreatedBucket{}, defectFailure(createBucketOperationID, errors.New("bucket name is invalid")), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateBucketDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateBucketBranchDefect, CreatedBucket{}, failurePointer(createBucketOperationID, sdkgo.FailureLocalDefect, "S3 bucket creation fence could not be initialized"), sdkgo.Receipt{})
	}
	value := CreatedBucket{Bucket: input.Bucket, Region: client.region}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(value, newFailure(createBucketOperationID, sdkgo.FailureTransport, "S3 bucket creation checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		if operation.bucketExists(call, input.Bucket) {
			return sdkgo.NewMutationBranch(CreateBucketBranchCreated, value, nil, client.receipt(call, "", objectLocation{bucket: input.Bucket}))
		}
		if fence.Decide().Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(value, newFailure(createBucketOperationID, sdkgo.FailureTransport, "S3 bucket creation could not be confirmed after the send cap"), sdkgo.Receipt{})
		}
	}
	if client.region == "us-east-1" && call.Context.Attempt() == 1 && operation.bucketExists(call, input.Bucket) {
		return sdkgo.NewMutationBranch(CreateBucketBranchProviderRejected, value, failurePointer(createBucketOperationID, sdkgo.FailureConflict, "S3 bucket already exists; recreating it in us-east-1 can reset its ACL"), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, createBucketOperationID)
	if failure != nil {
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationUncertain(value, newFailure(createBucketOperationID, sdkgo.FailureTransport, "S3 unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreateBucketBranchDefect, value, failure, sdkgo.Receipt{})
	}
	defer cancel()
	var body []byte
	var header http.Header
	if client.region != "us-east-1" && client.region != "auto" {
		body = []byte(`<CreateBucketConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><LocationConstraint>` + client.region + `</LocationConstraint></CreateBucketConfiguration>`)
		header = http.Header{"Content-Type": {"application/xml"}}
	}
	dispatch := &bucketCreateDispatch{client: client, session: session, request: s3Request{method: http.MethodPut, location: objectLocation{bucket: input.Bucket}, body: body, header: header}}
	if fence.Dispatch(dispatch.send) != nil {
		return sdkgo.NewMutationUncertain(value, newFailure(createBucketOperationID, sdkgo.FailureTransport, "S3 bucket creation dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response, err := dispatch.response, dispatch.err
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewMutationBranch(CreateBucketBranchDefect, value, failurePointer(createBucketOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[CreatedBucket](transportFailure(createBucketOperationID, "bucket creation"), 0)
	}
	defer closeResponseBody(response)
	if isSuccessStatus(response.StatusCode) {
		return sdkgo.NewMutationBranch(CreateBucketBranchCreated, value, nil, client.receipt(call, safeRequestID(response.Header), objectLocation{bucket: input.Bucket}))
	}
	classified := classifyErrorResponse(createBucketOperationID, "bucket creation", client.readErrorResponse(response, session.credentials), false)
	if classified.outcome == outcomeRetry {
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			if fence.RecordCanResend() != nil {
				return sdkgo.NewMutationUncertain(value, newFailure(createBucketOperationID, sdkgo.FailureTransport, "S3 resend checkpoint could not be recorded"), sdkgo.Receipt{})
			}
		}
		return sdkgo.NewMutationRetry[CreatedBucket](classified.failure, classified.retryAfter)
	}
	return sdkgo.NewMutationBranch(CreateBucketBranchProviderRejected, value, &classified.failure, client.receipt(call, classified.requestID, objectLocation{bucket: input.Bucket}))
}

func (operation CreateBucketOperation) bucketExists(call sdkgo.Call, bucket string) bool {
	session, cancel, failure := operation.client.startSession(call, createBucketOperationID)
	if failure != nil {
		return false
	}
	defer cancel()
	response, err := operation.client.exchange(session, s3Request{method: http.MethodHead, location: objectLocation{bucket: bucket}})
	if err != nil {
		return false
	}
	defer closeResponseBody(response)
	return response.StatusCode == http.StatusOK
}

type bucketCreateDispatch struct {
	client   *Client
	session  *operationSession
	request  s3Request
	response *http.Response
	err      error
}

func (dispatch *bucketCreateDispatch) send(requestContext context.Context) error {
	previous := dispatch.session.context
	dispatch.session.context = requestContext
	dispatch.response, dispatch.err = dispatch.client.exchange(dispatch.session, dispatch.request)
	dispatch.session.context = previous
	return nil
}

// DeleteBucketInput names an empty bucket to delete.
type DeleteBucketInput struct {
	// Bucket is the bucket name; blank uses the connection default.
	Bucket string `json:"bucket,omitempty"`
}

// DeletedBucket names a bucket S3 accepted for deletion.
type DeletedBucket struct {
	// Bucket is the bucket name.
	Bucket string `json:"bucket"`
}

// DeleteBucketOperation implements deletion of an empty bucket.
type DeleteBucketOperation struct{ client *Client }

// Definition returns the generated Mutation contract.
func (DeleteBucketOperation) Definition() sdkgo.MutationDefinition { return DeleteBucketDefinition }

// IdempotencyKey keeps the Dex Call identity stable across retries.
func (DeleteBucketOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteBucketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes an empty bucket; an already absent bucket is also deleted.
func (operation DeleteBucketOperation) Invoke(call sdkgo.Call, input DeleteBucketInput) sdkgo.MutationAttempt[DeletedBucket] {
	client := operation.client
	bucket, err := client.resolveBucket(input.Bucket)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteBucketBranchDefect, DeletedBucket{}, defectFailure(deleteBucketOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, deleteBucketOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteBucketBranchDefect, DeletedBucket{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	value := DeletedBucket{Bucket: bucket}
	response, err := client.exchange(session, s3Request{method: http.MethodDelete, location: objectLocation{bucket: bucket}})
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewMutationBranch(DeleteBucketBranchDefect, value, failurePointer(deleteBucketOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedBucket](transportFailure(deleteBucketOperationID, "bucket deletion"), 0)
	}
	defer closeResponseBody(response)
	if isSuccessStatus(response.StatusCode) {
		return sdkgo.NewMutationBranch(DeleteBucketBranchDeleted, value, nil, client.receipt(call, safeRequestID(response.Header), objectLocation{bucket: bucket}))
	}
	providerError := client.readErrorResponse(response, session.credentials)
	if response.StatusCode == http.StatusNotFound && providerError.code == "NoSuchBucket" {
		return sdkgo.NewMutationBranch(DeleteBucketBranchDeleted, value, nil, client.receipt(call, providerError.requestID, objectLocation{bucket: bucket}))
	}
	classified := classifyErrorResponse(deleteBucketOperationID, "bucket deletion", providerError, false)
	return mutationAttemptForFailure(classified, client.receipt(call, classified.requestID, objectLocation{bucket: bucket}), DeleteBucketBranchProviderRejected, DeleteBucketBranchDefect, value)
}
