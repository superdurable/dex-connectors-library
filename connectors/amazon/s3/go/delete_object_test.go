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

func TestDeleteObjectRemovesCurrentKeyAndRepeatsSafely(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/weekly.md", s3fake.Object{Body: []byte("weekly")})
	client := newPathStyleClient(t, store)
	for attempt := 0; attempt < 2; attempt++ {
		result, err := runMutation(t, newDexContext("delete"), client.DeleteObject(), s3.DeleteObjectInput{Key: "reports/weekly.md"})
		require.NoError(t, err)
		require.Equal(t, s3.DeleteObjectBranchDeleted, result.Branch)
		require.Equal(t, s3.DeletedObject{Bucket: testBucket, Key: "reports/weekly.md"}, result.Value)
	}
	_, exists := store.Object(testBucket, "reports/weekly.md")
	require.False(t, exists)
	require.Equal(t, 2, store.RequestCount(http.MethodDelete))
}

func TestDeleteObjectClassifiesRejectionAndUnknownOutcome(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodDelete {
			return &s3fake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}
		}
		return nil
	})
	result, err := runMutation(t, newDexContext("denied"), newPathStyleClient(t, store).DeleteObject(), s3.DeleteObjectInput{Key: "reports/weekly.md"})
	require.NoError(t, err)
	require.Equal(t, s3.DeleteObjectBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	requireNoSecrets(t, result)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodDelete {
			return &s3fake.Response{StatusCode: http.StatusServiceUnavailable, Code: "ServiceUnavailable"}
		}
		return nil
	})
	_, err = runMutation(t, newDexContext("unavailable"), newPathStyleClient(t, store).DeleteObject(), s3.DeleteObjectInput{Key: "reports/weekly.md"})
	requireRetry(t, err, sdkgo.FailureAvailability)
}
