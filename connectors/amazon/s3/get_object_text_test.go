// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetObjectTextReadsTextAndUnlabeledUTF8Content(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/2026-09.md", s3fake.Object{Body: []byte("\xef\xbb\xbf# Září\n"), ContentType: "text/markdown; charset=utf-8"})
	store.SetObject(testBucket, "exports/rows.csv", s3fake.Object{Body: []byte("a,b\n1,2\n"), ContentType: "binary/octet-stream"})
	store.SetObject(testBucket, "exports/data.json", s3fake.Object{Body: []byte(`{"a":1}`), ContentType: "application/vnd.api+json"})
	client := newPathStyleClient(t, store)

	result, err := runQuery(t, "markdown", client.GetObjectText(), s3.GetObjectTextInput{Key: "reports/2026-09.md"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectTextBranchRead, result.Branch)
	require.Equal(t, "\xef\xbb\xbf# Září\n", result.Value.Text, "the content is returned unchanged")
	require.Equal(t, int64(len("\xef\xbb\xbf# Září\n")), result.Value.SizeBytes)
	require.Equal(t, "text/markdown; charset=utf-8", result.Value.ContentType)
	require.NotEmpty(t, result.Value.ETag)

	for key, text := range map[string]string{"exports/rows.csv": "a,b\n1,2\n", "exports/data.json": `{"a":1}`} {
		result, err := runQuery(t, "unlabeled", client.GetObjectText(), s3.GetObjectTextInput{Key: key})
		require.NoError(t, err, key)
		require.Equal(t, s3.GetObjectTextBranchRead, result.Branch, key)
		require.Equal(t, text, result.Value.Text, key)
	}
}

func TestGetObjectTextRefusesBinaryEncodedAndOversizedObjects(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "image.png", s3fake.Object{Body: []byte("\x89PNG"), ContentType: "image/png"})
	store.SetObject(testBucket, "report.md.gz", s3fake.Object{Body: []byte("\x1f\x8b"), ContentType: "text/markdown", ContentEncoding: "gzip"})
	store.SetObject(testBucket, "binary.bin", s3fake.Object{Body: []byte("a\x00b"), ContentType: "application/octet-stream"})
	store.SetObject(testBucket, "latin1.txt", s3fake.Object{Body: []byte("caf\xe9"), ContentType: "text/plain"})
	store.SetObject(testBucket, "large.txt", s3fake.Object{Body: []byte(strings.Repeat("a", 65)), ContentType: "text/plain"})
	client := newPathStyleClient(t, store, func(config *s3.Config) { config.MaxTextBytes = 64 })

	for key, branch := range map[string]sdkgo.BranchID{
		"image.png": s3.GetObjectTextBranchUnsupportedContent, "report.md.gz": s3.GetObjectTextBranchUnsupportedContent,
		"binary.bin": s3.GetObjectTextBranchUnsupportedContent, "latin1.txt": s3.GetObjectTextBranchUnsupportedContent,
		"large.txt": s3.GetObjectTextBranchTooLarge,
	} {
		result, err := runQuery(t, "refused", client.GetObjectText(), s3.GetObjectTextInput{Key: key})
		require.NoError(t, err, key)
		require.Equal(t, branch, result.Branch, key)
		require.Empty(t, result.Value.Text, key)
		require.Equal(t, key, result.Value.Key, key)
		require.NotEmpty(t, result.Value.ContentType, key)
	}
}

func TestGetObjectTextSelectsNotFoundForAMissingKeyButRejectsAMissingBucket(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	result, err := runQuery(t, "missing-key", client.GetObjectText(), s3.GetObjectTextInput{Key: "missing.md"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectTextBranchNotFound, result.Branch)

	result, err = runQuery(t, "missing-bucket", client.GetObjectText(), s3.GetObjectTextInput{Bucket: "acme-missing", Key: "missing.md"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectTextBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "HTTP 404 NoSuchBucket")
	requireNoSecrets(t, result)
}

func TestGetObjectTextNeverReturnsContentThatRepeatsTheSecret(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "config.env", s3fake.Object{Body: []byte("KEY=" + testSecretAccessKey), ContentType: "text/plain"})
	result, err := runQuery(t, "secret", newPathStyleClient(t, store).GetObjectText(), s3.GetObjectTextInput{Key: "config.env"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectTextBranchInvalidResponse, result.Branch)
	requireNoSecrets(t, result)
}

func TestGetObjectTextBoundsAChunkedResponseWithoutContentLength(t *testing.T) {
	server := newStaticServer(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		response.WriteHeader(http.StatusOK)
		for range 10 {
			if _, err := response.Write([]byte(strings.Repeat("a", 16))); err != nil {
				return // The client stops reading once the body passes maxTextBytes.
			}
			response.(http.Flusher).Flush()
		}
	})
	client, err := s3.New(s3.Config{Endpoint: server.URL, Region: testRegion, DefaultBucket: testBucket, MaxTextBytes: 64}, staticCredentials(""))
	require.NoError(t, err)
	result, err := runQuery(t, "chunked", client.GetObjectText(), s3.GetObjectTextInput{Key: "stream.txt"})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectTextBranchTooLarge, result.Branch)
}

func TestGetObjectTextRetriesAnUnavailableStore(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(s3fake.Request) *s3fake.Response {
		return &s3fake.Response{StatusCode: http.StatusServiceUnavailable, Code: "SlowDown", Header: http.Header{"Retry-After": {"2"}}}
	})
	_, err := runQuery(t, "slow-down", newPathStyleClient(t, store).GetObjectText(), s3.GetObjectTextInput{Key: "a.md"})
	retry := requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Contains(t, retry.Error(), "slow down")
}
