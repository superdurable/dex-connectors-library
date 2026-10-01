// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func seedReports(store *s3fake.Server) {
	modified := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, key := range []string{"reports/2026-08.md", "reports/2026-09.md", "reports/drafts/2026-10.md", "reports/q3 summary+notes.md", "other/readme.txt"} {
		store.SetObject(testBucket, key, s3fake.Object{Body: []byte("# " + key), ContentType: "text/markdown", LastModified: modified})
	}
}

func TestListObjectsReturnsObjectsAndCommonPrefixesInKeyOrder(t *testing.T) {
	store := newFakeStore(t)
	seedReports(store)
	result, err := runQuery(t, "list", newPathStyleClient(t, store).ListObjects(), s3.ListObjectsInput{Prefix: "reports/", Delimiter: "/"})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchFound, result.Branch)
	page := result.Value
	require.Equal(t, testBucket, page.Bucket)
	require.Equal(t, []string{"reports/2026-08.md", "reports/2026-09.md", "reports/q3 summary+notes.md"}, objectKeys(page.Objects))
	require.Equal(t, []string{"reports/drafts/"}, page.CommonPrefixes)
	require.False(t, page.IsTruncated)
	require.Empty(t, page.NextContinuationToken)
	first := page.Objects[0]
	require.Equal(t, int64(len("# reports/2026-08.md")), first.SizeBytes)
	require.Equal(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), first.LastModified)
	require.True(t, strings.HasPrefix(first.ETag, `"`), "the ETag keeps the double quotes S3 sends")
	require.Equal(t, "STANDARD", first.StorageClass)

	query := store.Requests()[0].Query
	require.Equal(t, "2", query.Get("list-type"))
	require.Equal(t, "url", query.Get("encoding-type"), "URL encoding lets any key cross the XML response")
	require.Equal(t, "100", query.Get("max-keys"), "zero maxKeys uses the connection's listPageSize")
	require.Equal(t, "reports/", query.Get("prefix"))
	require.Equal(t, "/", query.Get("delimiter"))
	require.Equal(t, "amazon-s3", result.Receipt.Provider)
	require.Equal(t, testBucket, result.Receipt.ProviderObjectID)
	require.Equal(t, "FAKEREQUEST0001", result.Receipt.ProviderRequestID)
}

func TestListObjectsPagesWithTheContinuationToken(t *testing.T) {
	store := newFakeStore(t)
	seedReports(store)
	client := newPathStyleClient(t, store)
	first, err := runQuery(t, "page-1", client.ListObjects(), s3.ListObjectsInput{Prefix: "reports/", MaxKeys: 2})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchFound, first.Branch)
	require.Equal(t, []string{"reports/2026-08.md", "reports/2026-09.md"}, objectKeys(first.Value.Objects))
	require.True(t, first.Value.IsTruncated)
	require.NotEmpty(t, first.Value.NextContinuationToken)

	second, err := runQuery(t, "page-2", client.ListObjects(), s3.ListObjectsInput{
		Prefix: "reports/", MaxKeys: 2, ContinuationToken: first.Value.NextContinuationToken,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"reports/drafts/2026-10.md", "reports/q3 summary+notes.md"}, objectKeys(second.Value.Objects))
	require.False(t, second.Value.IsTruncated)
	require.Equal(t, first.Value.NextContinuationToken, store.Requests()[1].Query.Get("continuation-token"))
}

func TestListObjectsSelectsNotFoundOnlyForAnEmptyFinalPage(t *testing.T) {
	store := newFakeStore(t)
	seedReports(store)
	result, err := runQuery(t, "empty", newPathStyleClient(t, store).ListObjects(), s3.ListObjectsInput{Prefix: "reprots/"})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Equal(t, testBucket, result.Value.Bucket)
	require.Equal(t, "reprots/", result.Value.Prefix)

	truncatedEmpty := newListServer(t, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken></ListBucketResult>`)
	result, err = runQuery(t, "truncated-empty", truncatedEmpty.ListObjects(), s3.ListObjectsInput{})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchFound, result.Branch, "an empty page with more pages is not an empty listing")
	require.Equal(t, "next", result.Value.NextContinuationToken)
}

func TestListObjectsRejectsAMissingBucketInsteadOfReportingAnEmptyListing(t *testing.T) {
	store := newFakeStore(t)
	result, err := runQuery(t, "missing-bucket", newPathStyleClient(t, store).ListObjects(), s3.ListObjectsInput{Bucket: "acme-missing"})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "HTTP 404 NoSuchBucket")
	requireNoSecrets(t, result)
}

func TestListObjectsDecodesURLEncodedKeysOnlyWhenTheStoreEncodedThem(t *testing.T) {
	encoded := newListServer(t, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated>`+
		`<Contents><Key>a+b%2Bc%2F%C3%A9.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	result, err := runQuery(t, "encoded", encoded.ListObjects(), s3.ListObjectsInput{})
	require.NoError(t, err)
	require.Equal(t, []string{"a b+c/é.txt"}, objectKeys(result.Value.Objects))

	raw := newListServer(t, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>a+b%2B.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	result, err = runQuery(t, "raw", raw.ListObjects(), s3.ListObjectsInput{})
	require.NoError(t, err)
	require.Equal(t, []string{"a+b%2B.txt"}, objectKeys(result.Value.Objects), "a store that ignores encoding-type returns raw keys")
}

func TestListObjectsRejectsInvalidAndOversizedPages(t *testing.T) {
	for name, document := range map[string]string{
		"not XML":                        `{"objects":[]}`,
		"a truncated page without token": `<ListBucketResult><IsTruncated>true</IsTruncated></ListBucketResult>`,
		"a negative size":                `<ListBucketResult><Contents><Key>a</Key><Size>-1</Size></Contents></ListBucketResult>`,
		"a missing key":                  `<ListBucketResult><Contents><Size>1</Size></Contents></ListBucketResult>`,
		"an invalid modification time":   `<ListBucketResult><Contents><Key>a</Key><Size>1</Size><LastModified>yesterday</LastModified></Contents></ListBucketResult>`,
		"a page that repeats the secret": `<ListBucketResult><Contents><Key>` + testSecretAccessKey + `</Key><Size>1</Size></Contents></ListBucketResult>`,
		"a page above maxResponseBytes":  `<ListBucketResult>` + strings.Repeat(" ", 2048) + `</ListBucketResult>`,
	} {
		client := newListServer(t, document, func(config *s3.Config) { config.MaxResponseBytes = 1024 })
		result, err := runQuery(t, "invalid", client.ListObjects(), s3.ListObjectsInput{})
		require.NoError(t, err, name)
		require.Equal(t, s3.ListObjectsBranchInvalidResponse, result.Branch, name)
		requireNoSecrets(t, result)
	}
}

func TestListObjectsRejectsInvalidInputWithoutARequest(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	for name, input := range map[string]s3.ListObjectsInput{
		"maxKeys above 1000":         {MaxKeys: 1001},
		"a negative maxKeys":         {MaxKeys: -1},
		"an invalid bucket":          {Bucket: "Acme"},
		"a prefix with a control":    {Prefix: "reports/\x01"},
		"a prefix above 1024 bytes":  {Prefix: strings.Repeat("a", 1025)},
		"a continuation with spaces": {ContinuationToken: "next page"},
	} {
		result, err := runQuery(t, "invalid-input", client.ListObjects(), input)
		require.NoError(t, err, name)
		require.Equal(t, s3.ListObjectsBranchDefect, result.Branch, name)
	}
	noDefault := newPathStyleClient(t, store, func(config *s3.Config) { config.DefaultBucket = "" })
	result, err := runQuery(t, "no-bucket", noDefault.ListObjects(), s3.ListObjectsInput{})
	require.NoError(t, err)
	require.Equal(t, s3.ListObjectsBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "defaultBucket")
	require.Zero(t, store.RequestCount(""))
}

func TestListObjectsRetriesATransportFailure(t *testing.T) {
	store := newFakeStore(t)
	client := newPathStyleClient(t, store)
	store.Close()
	_, err := runQuery(t, "closed", client.ListObjects(), s3.ListObjectsInput{})
	requireRetry(t, err, sdkgo.FailureTransport)
}

// newListServer answers every list request with document after verifying nothing.
func newListServer(t *testing.T, document string, configure ...func(*s3.Config)) *s3.Client {
	t.Helper()
	server := newStaticServer(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/xml")
		_, _ = response.Write([]byte(document)) // The client may stop reading an oversized page early.
	})
	config := s3.Config{Endpoint: server.URL, Region: testRegion, DefaultBucket: testBucket}
	for _, apply := range configure {
		apply(&config)
	}
	client, err := s3.New(config, staticCredentials(""))
	require.NoError(t, err)
	return client
}

func objectKeys(objects []s3.ObjectSummary) []string {
	keys := make([]string, 0, len(objects))
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	return keys
}
