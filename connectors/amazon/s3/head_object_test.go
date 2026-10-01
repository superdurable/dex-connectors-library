// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestHeadObjectReturnsMetadataWithoutReadingContent(t *testing.T) {
	store := newFakeStore(t, func(config *s3fake.Config) { config.IsVersioned = true })
	modified := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	store.SetObject(testBucket, "reports/2026-09.md", s3fake.Object{
		Body: []byte("# September"), ContentType: "text/markdown; charset=utf-8", LastModified: modified,
		Metadata: map[string]string{"report-id": "2026-09", "owner": "ops"},
	})
	result, err := runQuery(t, "head", newPathStyleClient(t, store).HeadObject(), s3.HeadObjectInput{Key: "reports/2026-09.md"})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchFound, result.Branch)
	stored, _ := store.Object(testBucket, "reports/2026-09.md")
	require.Equal(t, s3.ObjectMetadata{
		Bucket: testBucket, Key: "reports/2026-09.md", SizeBytes: int64(len("# September")), ETag: stored.ETag,
		ContentType: "text/markdown; charset=utf-8", LastModified: modified, VersionID: "fake-version-0001",
		Metadata: map[string]string{"report-id": "2026-09", "owner": "ops"},
	}, result.Value)
	require.Equal(t, http.MethodHead, store.Requests()[0].Method)
	require.Equal(t, testBucket+"/reports/2026-09.md", result.Receipt.ProviderObjectID)
}

func TestHeadObjectSelectsNotFoundForAMissingKeyOrBucket(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	for _, input := range []s3.HeadObjectInput{{Key: "reports/missing.md"}, {Bucket: "acme-missing", Key: "a.md"}} {
		result, err := runQuery(t, "missing", client.HeadObject(), input)
		require.NoError(t, err)
		require.Equal(t, s3.HeadObjectBranchNotFound, result.Branch, "a HEAD response has no body to tell a missing bucket from a missing key")
		require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	}
}

func TestHeadObjectWithoutListBucketPermissionSelectsProviderRejected(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(s3fake.Request) *s3fake.Response { return &s3fake.Response{StatusCode: http.StatusForbidden} })
	result, err := runQuery(t, "forbidden", newPathStyleClient(t, store).HeadObject(), s3.HeadObjectInput{Key: "reports/missing.md"})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "S3 denied the object with HTTP 403", result.Failure.Message)
}

func TestHeadObjectSelectsInvalidResponseWithoutAModificationTime(t *testing.T) {
	server := newStaticServer(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Length", "3")
		response.WriteHeader(http.StatusOK)
	})
	client, err := s3.New(s3.Config{Endpoint: server.URL, Region: testRegion, DefaultBucket: testBucket}, staticCredentials(""))
	require.NoError(t, err)
	result, err := runQuery(t, "no-time", client.HeadObject(), s3.HeadObjectInput{Key: "a.md"})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchInvalidResponse, result.Branch)
	require.Equal(t, "a.md", result.Value.Key)
}

func TestHeadObjectDropsMetadataThatRepeatsTheSecret(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "a.md", s3fake.Object{Body: []byte("a"), Metadata: map[string]string{"leak": testSecretAccessKey, "kept": "yes"}})
	result, err := runQuery(t, "leak", newPathStyleClient(t, store).HeadObject(), s3.HeadObjectInput{Key: "a.md"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"kept": "yes"}, result.Value.Metadata)
	requireNoSecrets(t, result)
}

func TestObjectOperationsRejectInvalidKeysWithoutARequest(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	for name, key := range map[string]string{
		"a blank key":            "",
		"a key above 1024 bytes": string(make([]byte, 1025)),
		"a dot segment":          "reports/../secret.md",
		"a control character":    "reports/\n.md",
		"invalid UTF-8":          "reports/\xff.md",
	} {
		result, err := runQuery(t, "invalid-key", client.HeadObject(), s3.HeadObjectInput{Key: key})
		require.NoError(t, err, name)
		require.Equal(t, s3.HeadObjectBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Zero(t, store.RequestCount(""))
}

func TestVirtualHostedAddressingRefusesADottedBucketOverHTTPS(t *testing.T) {
	client, err := s3.New(s3.Config{Region: "us-east-1", AddressingStyle: s3.AddressingStyleVirtualHosted}, staticCredentials(""))
	require.NoError(t, err)
	result, err := runQuery(t, "dotted", client.HeadObject(), s3.HeadObjectInput{Bucket: "reports.example.com", Key: "a.md"})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "addressingStyle")
}
