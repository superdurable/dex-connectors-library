// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetObjectBytesReturnsBinaryAndBoundsLargeObject(t *testing.T) {
	store := newFakeStore(t)
	binary := []byte{0x00, 0xff, 0x81, 0x42}
	store.SetObject(testBucket, "images/icon.bin", s3fake.Object{Body: binary, ContentType: "application/octet-stream"})
	store.SetObject(testBucket, "images/large.bin", s3fake.Object{Body: bytes.Repeat([]byte{0x42}, 5<<20), ContentType: "application/octet-stream"})
	client := newPathStyleClient(t, store)
	result, err := runQuery(t, "bytes", client.GetObjectBytes(), s3.GetObjectBytesInput{Key: "images/icon.bin"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectBytesBranchRead, result.Branch)
	require.Equal(t, binary, result.Value.Bytes)
	large, err := runQuery(t, "large-bytes", client.GetObjectBytes(), s3.GetObjectBytesInput{Key: "images/large.bin"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectBytesBranchTooLarge, large.Branch)
	require.Empty(t, large.Value.Bytes)
}

func TestGetObjectBytesClassifiesMissingRejectionAndRetry(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	missing, err := runQuery(t, "missing-bytes", client.GetObjectBytes(), s3.GetObjectBytesInput{Key: "images/missing.bin"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectBytesBranchNotFound, missing.Branch)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodGet {
			return &s3fake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}
		}
		return nil
	})
	rejected, err := runQuery(t, "denied-bytes", client.GetObjectBytes(), s3.GetObjectBytesInput{Key: "images/icon.bin"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectBytesBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, rejected.Failure.Kind)
	requireNoSecrets(t, rejected)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodGet {
			return &s3fake.Response{StatusCode: http.StatusServiceUnavailable}
		}
		return nil
	})
	_, err = runQuery(t, "retry-bytes", client.GetObjectBytes(), s3.GetObjectBytesInput{Key: "images/icon.bin"})
	requireRetry(t, err, sdkgo.FailureAvailability)
}
