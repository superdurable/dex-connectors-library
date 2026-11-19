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

func TestCopyObjectStoresDestinationWithReadBackMarker(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/weekly.md", s3fake.Object{Body: []byte("weekly"), ContentType: "text/plain"})
	client := newPathStyleClient(t, store)
	input := s3.CopyObjectInput{SourceBucket: testBucket, SourceKey: "reports/weekly.md", DestinationBucket: otherBucket, DestinationKey: "reports/weekly.md", ContentType: "text/plain", Metadata: map[string]string{"report-id": "weekly"}}
	result, err := runMutation(t, newDexContext("copy"), client.CopyObject(), input)
	require.NoError(t, err)
	require.Equal(t, s3.CopyObjectBranchCopied, result.Branch)
	require.Equal(t, otherBucket, result.Value.DestinationBucket)
	object, exists := store.Object(otherBucket, "reports/weekly.md")
	require.True(t, exists)
	require.Equal(t, []byte("weekly"), object.Body)
	require.Equal(t, "weekly", object.Metadata["report-id"])
	require.NotEmpty(t, object.Metadata[s3.IdempotencyMetadataKey])
	require.Equal(t, 1, store.RequestCount(http.MethodPut))
}

func TestCopyObjectClassifiesRejectionAndUnknownOutcome(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/weekly.md", s3fake.Object{Body: []byte("weekly")})
	input := s3.CopyObjectInput{SourceBucket: testBucket, SourceKey: "reports/weekly.md", DestinationBucket: otherBucket, DestinationKey: "reports/weekly.md", ContentType: "text/plain"}
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodPut {
			return &s3fake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}
		}
		return nil
	})
	client := newPathStyleClient(t, store)
	result, err := runMutation(t, newDexContext("copy-denied"), client.CopyObject(), input)
	require.NoError(t, err)
	require.Equal(t, s3.CopyObjectBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	requireNoSecrets(t, result)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodPut {
			return &s3fake.Response{StatusCode: http.StatusInternalServerError, Code: "InternalError"}
		}
		return nil
	})
	context := newDexContext("copy-retry")
	_, err = runMutation(t, context, client.CopyObject(), input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	store.Intercept(nil)
	result, err = runMutation(t, context.nextAttempt(), client.CopyObject(), input)
	require.NoError(t, err)
	require.Equal(t, s3.CopyObjectBranchCopied, result.Branch)
	require.Equal(t, 3, store.RequestCount(http.MethodPut))
}
