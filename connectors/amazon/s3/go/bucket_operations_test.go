// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestBucketLifecycleUsesSignedRequests(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	listed, err := runQuery(t, "list-buckets", client.ListBuckets(), s3.ListBucketsInput{})
	require.NoError(t, err)
	require.Equal(t, s3.ListBucketsBranchListed, listed.Branch)
	require.NotEmpty(t, listed.Value.Buckets)
	created, err := runMutation(t, newDexContext("create-bucket"), client.CreateBucket(), s3.CreateBucketInput{Bucket: "new-reports-bucket"})
	require.NoError(t, err)
	require.Equal(t, s3.CreateBucketBranchCreated, created.Branch)
	require.Equal(t, "new-reports-bucket", created.Value.Bucket)
	deleted, err := runMutation(t, newDexContext("delete-bucket"), client.DeleteBucket(), s3.DeleteBucketInput{Bucket: "new-reports-bucket"})
	require.NoError(t, err)
	require.Equal(t, s3.DeleteBucketBranchDeleted, deleted.Branch)
	require.Equal(t, 1, store.RequestCount(http.MethodPut))
	require.Equal(t, 1, store.RequestCount(http.MethodDelete))
}

func TestBucketOperationsClassifyRejectionAndRetry(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodPut {
			return &s3fake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}
		}
		return nil
	})
	client := newPathStyleClient(t, store)
	result, err := runMutation(t, newDexContext("bucket-denied"), client.CreateBucket(), s3.CreateBucketInput{Bucket: "new-reports-bucket"})
	require.NoError(t, err)
	require.Equal(t, s3.CreateBucketBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	requireNoSecrets(t, result)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodDelete {
			return &s3fake.Response{StatusCode: http.StatusTooManyRequests}
		}
		return nil
	})
	_, err = runMutation(t, newDexContext("bucket-retry"), client.DeleteBucket(), s3.DeleteBucketInput{Bucket: testBucket})
	requireRetry(t, err, sdkgo.FailureRateLimit)
}
