// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"crypto/md5"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const reportText = "# September\n\nRefunds above 500 need approval.\n"

func reportInput(isCreateOnly bool) s3.PutObjectInput {
	return s3.PutObjectInput{
		Key: "reports/2026-09.md", ContentType: "text/markdown; charset=utf-8", TextContent: reportText,
		Metadata: map[string]string{"report-id": "2026-09"}, IsCreateOnly: isCreateOnly,
	}
}

func TestPutObjectStoresContentTypeMetadataAndTheIdempotencyMarker(t *testing.T) {
	store := newFakeStore(t, func(config *s3fake.Config) { config.IsVersioned = true })
	context := newDexContext("put")
	result, err := runMutation(t, context, newPathStyleClient(t, store).PutObject(), reportInput(false))
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, result.Branch)
	stored, isStored := store.Object(testBucket, "reports/2026-09.md")
	require.True(t, isStored)
	require.Equal(t, reportText, string(stored.Body))
	require.Equal(t, "text/markdown; charset=utf-8", stored.ContentType)
	require.Equal(t, map[string]string{"report-id": "2026-09", s3.IdempotencyMetadataKey: string(result.Receipt.IdempotencyKey)}, stored.Metadata)
	require.Equal(t, s3.StoredObject{
		Bucket: testBucket, Key: "reports/2026-09.md", ETag: stored.ETag, VersionID: "fake-version-0001",
		SizeBytes: int64(len(reportText)), ContentType: "text/markdown; charset=utf-8",
	}, result.Value)

	request := store.Requests()[0]
	digest := md5.Sum([]byte(reportText))
	require.Equal(t, base64.StdEncoding.EncodeToString(digest[:]), request.Header.Get("Content-MD5"))
	require.Empty(t, request.Header.Get("If-None-Match"), "overwrite mode sends no precondition")
	require.Contains(t, request.Header.Get("Authorization"), "content-md5;content-type;host;x-amz-content-sha256;x-amz-date;x-amz-meta-dex-idempotency-key;x-amz-meta-report-id")
	require.Equal(t, string(result.Receipt.IdempotencyKey), string(result.Receipt.CallID), "the marker is the stable Call ID")
}

// TestARepeatedOverwriteAttemptSendsTheSameRequestAndLeavesTheSameObject proves overwrite-mode idempotency.
func TestARepeatedOverwriteAttemptSendsTheSameRequestAndLeavesTheSameObject(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	first := newDexContext("overwrite")
	firstResult, err := runMutation(t, first, client.PutObject(), reportInput(false))
	require.NoError(t, err)
	afterFirst, _ := store.Object(testBucket, "reports/2026-09.md")
	secondResult, err := runMutation(t, first.nextAttempt(), client.PutObject(), reportInput(false))
	require.NoError(t, err)
	afterSecond, _ := store.Object(testBucket, "reports/2026-09.md")

	require.Equal(t, s3.PutObjectBranchStored, secondResult.Branch)
	require.Equal(t, firstResult.Value, secondResult.Value)
	require.Equal(t, afterFirst.Body, afterSecond.Body)
	require.Equal(t, afterFirst.Metadata, afterSecond.Metadata)
	require.Equal(t, afterFirst.ETag, afterSecond.ETag)
	require.Equal(t, 1, store.ObjectCount())
	requests := store.Requests()
	require.Len(t, requests, 2)
	require.Equal(t, requests[0].Body, requests[1].Body)
	for _, name := range []string{"Content-Type", "Content-Md5", "X-Amz-Content-Sha256", "X-Amz-Meta-Report-Id", "X-Amz-Meta-Dex-Idempotency-Key"} {
		require.Equal(t, requests[0].Header.Get(name), requests[1].Header.Get(name), name)
	}
}

func TestCreateOnlyStoresOnceAndRecognizesItsOwnEarlierAttempt(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	first := newDexContext("create-only")
	result, err := runMutation(t, first, client.PutObject(), reportInput(true))
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, result.Branch)
	require.False(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, "*", store.Requests()[0].Header.Get("If-None-Match"))

	retried, err := runMutation(t, first.nextAttempt(), client.PutObject(), reportInput(true))
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, retried.Branch, "a duplicate dispatch is not another writer")
	require.True(t, retried.Value.IsFromEarlierAttempt)
	require.Equal(t, result.Value.ETag, retried.Value.ETag)
	require.Equal(t, int64(len(reportText)), retried.Value.SizeBytes)
	require.False(t, retried.Value.LastModified.IsZero())
	require.Equal(t, 2, store.RequestCount(http.MethodPut))
	require.Equal(t, 1, store.RequestCount(http.MethodHead))
}

func TestCreateOnlyRetriesALostResponseAndThenFindsTheStoredObject(t *testing.T) {
	store := newFakeStore(t)
	store.DelayPutResponses(2 * time.Second)
	client, err := s3.New(s3.Config{Endpoint: store.URL, Region: testRegion, DefaultBucket: testBucket}, staticCredentials(""),
		s3.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))
	require.NoError(t, err)
	first := newDexContext("lost-response")
	_, err = runMutation(t, first, client.PutObject(), reportInput(true))
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Eventually(t, func() bool { _, isStored := store.Object(testBucket, "reports/2026-09.md"); return isStored }, 5*time.Second, 20*time.Millisecond)

	retried, err := runMutation(t, first.nextAttempt(), client.PutObject(), reportInput(true))
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, retried.Branch)
	require.True(t, retried.Value.IsFromEarlierAttempt)
	require.Equal(t, 1, store.ObjectCount())
}

func TestCreateOnlySelectsAlreadyExistsForAnotherWritersObjectWithoutOverwritingIt(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/2026-09.md", s3fake.Object{
		Body: []byte("earlier report"), ContentType: "text/plain", Metadata: map[string]string{s3.IdempotencyMetadataKey: "another-call"},
	})
	result, err := runMutation(t, newDexContext("conflict"), newPathStyleClient(t, store).PutObject(), reportInput(true))
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchAlreadyExists, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	existing, _ := store.Object(testBucket, "reports/2026-09.md")
	require.Equal(t, "earlier report", string(existing.Body), "create-only never replaces the existing object")
	require.Equal(t, existing.ETag, result.Value.ETag)
	require.Equal(t, int64(len("earlier report")), result.Value.SizeBytes)
	require.Equal(t, "text/plain", result.Value.ContentType)
	require.False(t, result.Value.IsFromEarlierAttempt)
}

func TestCreateOnlyCheckOutcomesNeverGuessWhoWroteTheObject(t *testing.T) {
	for name, test := range map[string]struct {
		headResponse *s3fake.Response
		branch       sdkgo.BranchID
		retryKind    sdkgo.FailureKind
	}{
		"a forbidden read":     {headResponse: &s3fake.Response{StatusCode: http.StatusForbidden}, branch: s3.PutObjectBranchProviderRejected},
		"a removed object":     {headResponse: &s3fake.Response{StatusCode: http.StatusNotFound}, retryKind: sdkgo.FailureConflict},
		"an unavailable store": {headResponse: &s3fake.Response{StatusCode: http.StatusServiceUnavailable}, retryKind: sdkgo.FailureAvailability},
	} {
		store := newFakeStore(t)
		store.SetObject(testBucket, "reports/2026-09.md", s3fake.Object{Body: []byte("earlier")})
		store.Intercept(func(request s3fake.Request) *s3fake.Response {
			if request.Method == http.MethodHead {
				return test.headResponse
			}
			return nil
		})
		result, err := runMutation(t, newDexContext("check"), newPathStyleClient(t, store).PutObject(), reportInput(true))
		if test.retryKind != "" {
			requireRetry(t, err, test.retryKind)
			continue
		}
		require.NoError(t, err, name)
		require.Equal(t, test.branch, result.Branch, name)
		require.Contains(t, result.Failure.Message, "s3:GetObject", name)
	}
}

func TestPutObjectRetriesAConditionalConflictAndExplainsAStoreWithoutConditionalWrites(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(s3fake.Request) *s3fake.Response {
		return &s3fake.Response{StatusCode: http.StatusConflict, Code: "ConditionalRequestConflict"}
	})
	_, err := runMutation(t, newDexContext("conflict"), newPathStyleClient(t, store).PutObject(), reportInput(true))
	requireRetry(t, err, sdkgo.FailureConflict)

	store.Intercept(func(s3fake.Request) *s3fake.Response {
		return &s3fake.Response{StatusCode: http.StatusNotImplemented, Code: "NotImplemented"}
	})
	result, err := runMutation(t, newDexContext("unsupported"), newPathStyleClient(t, store).PutObject(), reportInput(true))
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "leave isCreateOnly false")
	requireNoSecrets(t, result)
}

// TestCreateOnlyDependsOnTheStoreHonoringIfNoneMatch records the documented limit of create-only mode.
func TestCreateOnlyDependsOnTheStoreHonoringIfNoneMatch(t *testing.T) {
	store := newFakeStore(t, func(config *s3fake.Config) { config.IgnoresIfNoneMatch = true })
	store.SetObject(testBucket, "reports/2026-09.md", s3fake.Object{Body: []byte("earlier")})
	result, err := runMutation(t, newDexContext("ignored"), newPathStyleClient(t, store).PutObject(), reportInput(true))
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, result.Branch)
	overwritten, _ := store.Object(testBucket, "reports/2026-09.md")
	require.Equal(t, reportText, string(overwritten.Body))
}

func TestPutObjectRetriesATransportFailureBecauseARepeatConverges(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	store.Close()
	_, err := runMutation(t, newDexContext("closed"), client.PutObject(), reportInput(false))
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestPutObjectStoresBytesAndEmptyObjects(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	result, err := runMutation(t, newDexContext("bytes"), client.PutObject(), s3.PutObjectInput{
		Key: "images/pixel.png", ContentType: "image/png", ByteContent: []byte{0x89, 'P', 'N', 'G', 0},
	})
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, result.Branch)
	stored, _ := store.Object(testBucket, "images/pixel.png")
	require.Equal(t, []byte{0x89, 'P', 'N', 'G', 0}, stored.Body)

	result, err = runMutation(t, newDexContext("empty"), client.PutObject(), s3.PutObjectInput{Key: "markers/done", ContentType: "text/plain"})
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, result.Branch)
	require.Zero(t, result.Value.SizeBytes)
	require.Zero(t, store.SignatureErrorCount())
}

func TestPutObjectRejectsInvalidInputWithoutARequest(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store, func(config *s3.Config) { config.MaxUploadBytes = 16 })
	valid := s3.PutObjectInput{Key: "a.txt", ContentType: "text/plain", TextContent: "ok"}
	for name, mutate := range map[string]func(*s3.PutObjectInput){
		"both text and bytes":          func(input *s3.PutObjectInput) { input.ByteContent = []byte("x") },
		"content above maxUploadBytes": func(input *s3.PutObjectInput) { input.TextContent = strings.Repeat("a", 17) },
		"a blank content type":         func(input *s3.PutObjectInput) { input.ContentType = "" },
		"an invalid content type":      func(input *s3.PutObjectInput) { input.ContentType = "text/plain; charset" },
		"the reserved metadata name":   func(input *s3.PutObjectInput) { input.Metadata = map[string]string{s3.IdempotencyMetadataKey: "x"} },
		"an uppercase metadata name":   func(input *s3.PutObjectInput) { input.Metadata = map[string]string{"Report-ID": "x"} },
		"a non-ASCII metadata value":   func(input *s3.PutObjectInput) { input.Metadata = map[string]string{"owner": "Zoë"} },
		"a padded metadata value":      func(input *s3.PutObjectInput) { input.Metadata = map[string]string{"owner": " ops"} },
		"metadata above 2 KB":          func(input *s3.PutObjectInput) { input.Metadata = map[string]string{"notes": strings.Repeat("a", 1900)} },
		"a dot segment key":            func(input *s3.PutObjectInput) { input.Key = "./a.txt" },
	} {
		input := valid
		mutate(&input)
		result, err := runMutation(t, newDexContext("invalid"), client.PutObject(), input)
		require.NoError(t, err, name)
		require.Equal(t, s3.PutObjectBranchDefect, result.Branch, name)
	}
	require.Zero(t, store.RequestCount(""))
}
