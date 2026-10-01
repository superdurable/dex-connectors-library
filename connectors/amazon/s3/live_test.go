//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3live"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// liveStore is a real S3 API configured through S3_CONNECTOR_TEST_* variables, such as a local MinIO server.
type liveStore struct {
	s3live.Store
	config      s3.Config
	credentials s3.Credentials
	prefix      string
}

func requireLiveStore(t *testing.T) liveStore {
	t.Helper()
	store := s3live.RequireStore(t)
	return liveStore{
		Store: store,
		config: s3.Config{
			Endpoint: store.Endpoint, Region: store.Region, AddressingStyle: s3.AddressingStyle(store.AddressingStyle), DefaultBucket: store.Bucket,
		},
		credentials: s3.Credentials{
			AccessKeyID: sdkgo.NewSecretString(store.AccessKeyID), SecretAccessKey: sdkgo.NewSecretString(store.SecretAccessKey),
			SessionToken: sdkgo.NewSecretString(store.SessionToken),
		},
		prefix: fmt.Sprintf("dex-s3-live/%d/", time.Now().UnixNano()),
	}
}

func (store liveStore) client(t *testing.T, configure ...func(*s3.Config)) *s3.Client {
	t.Helper()
	config := store.config
	for _, apply := range configure {
		apply(&config)
	}
	client, err := s3.New(config, sdkgo.StaticCredentialProvider[s3.Credentials]{testConnection: store.credentials})
	require.NoError(t, err)
	return client
}

func TestLiveStoreRoundTripsTextMetadataAndEncodedKeys(t *testing.T) {
	store := requireLiveStore(t)
	client := store.client(t)
	key := store.prefix + "reports/q3 summary+notes é.md"
	store.DeleteAfterTest(t, key)

	put, err := runMutation(t, newDexContext("live-put-"+store.prefix), client.PutObject(), s3.PutObjectInput{
		Key: key, ContentType: "text/markdown; charset=utf-8", TextContent: reportText, Metadata: map[string]string{"report-id": "2026-09"},
	})
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, put.Branch, "%+v", put.Failure)
	require.NotEmpty(t, put.Value.ETag)

	head, err := runQuery(t, "live-head", client.HeadObject(), s3.HeadObjectInput{Key: key})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchFound, head.Branch, "%+v", head.Failure)
	require.Equal(t, put.Value.ETag, head.Value.ETag)
	require.Equal(t, int64(len(reportText)), head.Value.SizeBytes)
	require.Equal(t, "text/markdown; charset=utf-8", head.Value.ContentType)
	require.Equal(t, "2026-09", head.Value.Metadata["report-id"])
	require.Equal(t, string(put.Receipt.IdempotencyKey), head.Value.Metadata[s3.IdempotencyMetadataKey])

	text, err := runQuery(t, "live-get", client.GetObjectText(), s3.GetObjectTextInput{Key: key})
	require.NoError(t, err)
	require.Equal(t, s3.GetObjectTextBranchRead, text.Branch, "%+v", text.Failure)
	require.Equal(t, reportText, text.Value.Text)

	listed, err := runQuery(t, "live-list", client.ListObjects(), s3.ListObjectsInput{Prefix: store.prefix, Delimiter: "/"})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchFound, listed.Branch, "%+v", listed.Failure)
	require.Equal(t, []string{store.prefix + "reports/"}, listed.Value.CommonPrefixes)
	listed, err = runQuery(t, "live-list-reports", client.ListObjects(), s3.ListObjectsInput{Prefix: store.prefix + "reports/"})
	require.NoError(t, err)
	require.Equal(t, []string{key}, objectKeys(listed.Value.Objects), "a space, a plus, and non-ASCII survive URL encoding")
	requireNoSecrets(t, []any{put, head, text, listed})
}

func TestLiveStoreRepeatedAttemptsConvergeInBothWriteModes(t *testing.T) {
	store := requireLiveStore(t)
	client := store.client(t)
	overwriteKey, createKey := store.prefix+"overwrite.md", store.prefix+"create-only.md"
	store.DeleteAfterTest(t, overwriteKey)
	store.DeleteAfterTest(t, createKey)

	overwrite := newDexContext("live-overwrite-" + store.prefix)
	first, err := runMutation(t, overwrite, client.PutObject(), s3.PutObjectInput{Key: overwriteKey, ContentType: "text/plain", TextContent: reportText})
	require.NoError(t, err)
	second, err := runMutation(t, overwrite.nextAttempt(), client.PutObject(), s3.PutObjectInput{Key: overwriteKey, ContentType: "text/plain", TextContent: reportText})
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, second.Branch, "%+v", second.Failure)
	require.Equal(t, first.Value.ETag, second.Value.ETag, "the same bytes leave the same object")

	create := newDexContext("live-create-" + store.prefix)
	created, err := runMutation(t, create, client.PutObject(), s3.PutObjectInput{Key: createKey, ContentType: "text/plain", TextContent: reportText, IsCreateOnly: true})
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, created.Branch, "%+v", created.Failure)
	retried, err := runMutation(t, create.nextAttempt(), client.PutObject(), s3.PutObjectInput{Key: createKey, ContentType: "text/plain", TextContent: reportText, IsCreateOnly: true})
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchStored, retried.Branch, "%+v", retried.Failure)
	require.True(t, retried.Value.IsFromEarlierAttempt, "the store answered 412 and the marker identified the earlier attempt")

	other, err := runMutation(t, newDexContext("live-other-"+store.prefix), client.PutObject(), s3.PutObjectInput{
		Key: createKey, ContentType: "text/plain", TextContent: "another writer", IsCreateOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, s3.PutObjectBranchAlreadyExists, other.Branch, "%+v", other.Failure)
	require.Equal(t, created.Value.ETag, other.Value.ETag)
	text, err := runQuery(t, "live-create-read", client.GetObjectText(), s3.GetObjectTextInput{Key: createKey})
	require.NoError(t, err)
	require.Equal(t, reportText, text.Value.Text, "create-only left the first object in place")
}

func TestLiveStoreClassifiesMissingObjectsBoundsAndRejections(t *testing.T) {
	store := requireLiveStore(t)
	client := store.client(t, func(config *s3.Config) { config.MaxTextBytes = 16 })
	largeKey, binaryKey := store.prefix+"large.txt", store.prefix+"image.png"
	store.DeleteAfterTest(t, largeKey)
	store.DeleteAfterTest(t, binaryKey)
	_, err := runMutation(t, newDexContext("live-large-"+store.prefix), client.PutObject(), s3.PutObjectInput{Key: largeKey, ContentType: "text/plain", TextContent: strings.Repeat("a", 17)})
	require.NoError(t, err)
	_, err = runMutation(t, newDexContext("live-binary-"+store.prefix), client.PutObject(), s3.PutObjectInput{Key: binaryKey, ContentType: "image/png", ByteContent: []byte{0x89, 'P', 'N', 'G'}})
	require.NoError(t, err)

	for name, test := range map[string]struct {
		key    string
		branch sdkgo.BranchID
	}{
		"missing": {store.prefix + "missing.txt", s3.GetObjectTextBranchNotFound},
		"large":   {largeKey, s3.GetObjectTextBranchTooLarge},
		"binary":  {binaryKey, s3.GetObjectTextBranchUnsupportedContent},
	} {
		result, err := runQuery(t, "live-text", client.GetObjectText(), s3.GetObjectTextInput{Key: test.key})
		require.NoError(t, err, name)
		require.Equal(t, test.branch, result.Branch, "%s: %+v", name, result.Failure)
	}
	head, err := runQuery(t, "live-head-missing", client.HeadObject(), s3.HeadObjectInput{Key: store.prefix + "missing.txt"})
	require.NoError(t, err)
	require.Equal(t, s3.HeadObjectBranchNotFound, head.Branch, "%+v", head.Failure)
	empty, err := runQuery(t, "live-list-empty", client.ListObjects(), s3.ListObjectsInput{Prefix: store.prefix + "nothing/"})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchNotFound, empty.Branch, "%+v", empty.Failure)
	page, err := runQuery(t, "live-list-page", client.ListObjects(), s3.ListObjectsInput{Prefix: store.prefix, MaxKeys: 1})
	require.NoError(t, err)
	require.True(t, page.Value.IsTruncated)
	next, err := runQuery(t, "live-list-next", client.ListObjects(), s3.ListObjectsInput{Prefix: store.prefix, MaxKeys: 1, ContinuationToken: page.Value.NextContinuationToken})
	require.NoError(t, err)
	require.NotEqual(t, objectKeys(page.Value.Objects), objectKeys(next.Value.Objects))

	missingBucket, err := runQuery(t, "live-missing-bucket", client.ListObjects(), s3.ListObjectsInput{Bucket: "dex-s3-live-missing-bucket"})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchProviderRejected, missingBucket.Branch, "%+v", missingBucket.Failure)
	t.Logf("missing bucket: %s", missingBucket.Failure.Message)

	badSecret := store
	badSecret.credentials.SecretAccessKey = sdkgo.NewSecretString("not-the-secret-access-key")
	rejected, err := runQuery(t, "live-bad-secret", badSecret.client(t).ListObjects(), s3.ListObjectsInput{Prefix: store.prefix})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, rejected.Failure.Kind)
	t.Logf("bad secret: %s", rejected.Failure.Message)
	wrongRegion := "eu-west-1"
	if store.config.Region == wrongRegion {
		wrongRegion = "us-west-2"
	}
	misregioned, err := runQuery(t, "live-wrong-region", store.client(t, func(config *s3.Config) { config.Region = wrongRegion }).ListObjects(),
		s3.ListObjectsInput{Prefix: store.prefix})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchProviderRejected, misregioned.Branch)
	t.Logf("wrong region: %s", misregioned.Failure.Message)
	requireNoSecrets(t, []any{missingBucket, rejected, misregioned})
}
