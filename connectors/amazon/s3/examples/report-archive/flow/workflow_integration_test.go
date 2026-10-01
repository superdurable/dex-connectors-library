//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessKeyID     = "report-archive-access-key"
	integrationSecretAccessKey = "report-archive-integration-secret"
	integrationRegion          = "eu-west-1"
	integrationBucket          = "acme-reports"
	// slowWriteDelay outlasts the seven-second async local phase, so Dex dispatches the Step again.
	slowWriteDelay = 9 * time.Second
)

func TestReportArchiveExampleArchivesOnceAndReadsItBackWithRealDex(t *testing.T) {
	store := newIntegrationStore(t)
	flow, harness := newReportArchiveHarness(t, newFakeStoreClient(t, store))
	ctx := integrationContext(t)
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Input{ReportID: "ops-" + runID, Title: "Ops weekly", Highlights: []string{"Refunds above 500 need approval."}}

	archived := runReportArchive(t, ctx, harness.client, flow, "archived-"+runID, input)
	require.Equal(t, dex.FlowCompleted, archived.status)
	require.Equal(t, StatusArchived, archived.outcome.Status)
	key := "reports/ops-" + runID + ".md"
	stored, isStored := store.Object(integrationBucket, key)
	require.True(t, isStored)
	require.Equal(t, "# Ops weekly\n\nReport ID: ops-"+runID+"\n\n- Refunds above 500 need approval.\n", string(stored.Body))
	require.Equal(t, ReportContentType, stored.ContentType)
	require.Equal(t, "ops-"+runID, stored.Metadata["report-id"])
	require.Equal(t, GeneratorMetadataValue, stored.Metadata["generator"])
	require.NotEmpty(t, stored.Metadata[s3.IdempotencyMetadataKey])
	require.Equal(t, stored.ETag, archived.outcome.Stored.ETag)
	require.False(t, archived.outcome.IsFromEarlierAttempt)
	require.Equal(t, stored.ETag, archived.outcome.Report.ETag)
	require.Equal(t, int64(len(stored.Body)), archived.outcome.Report.SizeBytes)
	require.Equal(t, stored.Metadata, archived.outcome.Report.Metadata)
	require.True(t, archived.outcome.IsTextVerified)
	require.Contains(t, archived.outcome.ArchivedReportKeys, key)
	require.Equal(t, "*", putRequests(store)[0].Header.Get("If-None-Match"), "the example archives create-only")

	again := runReportArchive(t, ctx, harness.client, flow, "again-"+runID, Input{ReportID: input.ReportID, Title: "Ops weekly (revised)"})
	require.Equal(t, dex.FlowCompleted, again.status)
	require.Equal(t, StatusAlreadyArchived, again.outcome.Status, "another Flow's write is not this Flow's earlier attempt")
	require.Equal(t, stored.ETag, again.outcome.Stored.ETag)
	require.False(t, again.outcome.IsTextVerified, "the existing report differs from the revision")
	unchanged, _ := store.Object(integrationBucket, key)
	require.Equal(t, stored.Body, unchanged.Body)

	replaced := runReportArchive(t, ctx, harness.client, flow, "replaced-"+runID, Input{
		ReportID: input.ReportID, Title: "Ops weekly (revised)", ShouldReplaceExisting: true,
	})
	require.Equal(t, dex.FlowCompleted, replaced.status)
	require.Equal(t, StatusArchived, replaced.outcome.Status)
	require.True(t, replaced.outcome.IsTextVerified)
	revised, _ := store.Object(integrationBucket, key)
	require.Equal(t, "# Ops weekly (revised)\n\nReport ID: ops-"+runID+"\n", string(revised.Body))
	require.Zero(t, store.SignatureErrorCount())

	requestsBefore := store.RequestCount("")
	rejected := runReportArchive(t, ctx, harness.client, flow, "rejected-"+runID, Input{ReportID: "../escape", Title: "T"})
	require.Equal(t, dex.FlowFailed, rejected.status)
	require.Equal(t, requestsBefore, store.RequestCount(""), "an invalid request never reaches S3")
}

func TestReportArchiveUnwiredRejectionFailsTheFlowWithoutReadingBackWithRealDex(t *testing.T) {
	store := newIntegrationStore(t)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodPut {
			return &s3fake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}
		}
		return nil
	})
	flow, harness := newReportArchiveHarness(t, newFakeStoreClient(t, store))
	ctx := integrationContext(t)
	result := runReportArchive(t, ctx, harness.client, flow, "denied-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{ReportID: "denied", Title: "T"})
	require.Equal(t, dex.FlowFailed, result.status)
	require.Equal(t, 1, store.RequestCount(http.MethodPut), "a conclusive rejection is not retried")
	require.Zero(t, store.RequestCount(http.MethodHead))
}

// TestReportArchiveDuplicateCreateOnlyDispatchStoresOnceWithRealDex: the repeated PUT gets 412 and finds its marker.
func TestReportArchiveDuplicateCreateOnlyDispatchStoresOnceWithRealDex(t *testing.T) {
	store := newIntegrationStore(t)
	store.DelayPutResponses(slowWriteDelay)
	flow, harness := newReportArchiveHarness(t, newFakeStoreClient(t, store))
	ctx := integrationContext(t)
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	result := runReportArchive(t, ctx, harness.client, flow, "duplicate-create-"+runID, Input{ReportID: "dup-" + runID, Title: "Duplicate dispatch"})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusArchived, result.outcome.Status, "the repeated attempt recognized its own write")
	require.True(t, result.outcome.IsTextVerified)
	puts := putRequests(store)
	require.Len(t, puts, 2, "Dex dispatched the slow Step a second time")
	require.Equal(t, puts[0].Body, puts[1].Body)
	require.Equal(t, puts[0].Header.Get("X-Amz-Meta-Dex-Idempotency-Key"), puts[1].Header.Get("X-Amz-Meta-Dex-Idempotency-Key"))
	require.Equal(t, 1, store.ObjectCount())
	t.Logf("duplicate create-only dispatch: puts=%d heads=%d isFromEarlierAttempt=%t",
		len(puts), store.RequestCount(http.MethodHead), result.outcome.IsFromEarlierAttempt)
}

// TestReportArchiveDuplicateOverwriteDispatchConvergesWithRealDex: both PUTs send identical bytes and headers.
func TestReportArchiveDuplicateOverwriteDispatchConvergesWithRealDex(t *testing.T) {
	store := newIntegrationStore(t)
	store.DelayPutResponses(slowWriteDelay)
	flow, harness := newReportArchiveHarness(t, newFakeStoreClient(t, store))
	ctx := integrationContext(t)
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	result := runReportArchive(t, ctx, harness.client, flow, "duplicate-overwrite-"+runID, Input{
		ReportID: "dup-" + runID, Title: "Duplicate dispatch", ShouldReplaceExisting: true,
	})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusArchived, result.outcome.Status)
	require.True(t, result.outcome.IsTextVerified)
	puts := putRequests(store)
	require.Len(t, puts, 2, "Dex dispatched the slow Step a second time")
	for _, name := range []string{"Content-Type", "Content-Md5", "X-Amz-Content-Sha256", "X-Amz-Meta-Report-Id", "X-Amz-Meta-Dex-Idempotency-Key"} {
		require.Equal(t, puts[0].Header.Get(name), puts[1].Header.Get(name), name)
	}
	require.Empty(t, puts[0].Header.Get("If-None-Match"))
	require.Equal(t, puts[0].Body, puts[1].Body)
	stored, _ := store.Object(integrationBucket, "reports/dup-"+runID+".md")
	require.Equal(t, puts[0].Body, stored.Body)
	require.Equal(t, stored.ETag, result.outcome.Report.ETag)
	require.Equal(t, 1, store.ObjectCount())
}

func putRequests(store *s3fake.Server) []s3fake.Request {
	var puts []s3fake.Request
	for _, request := range store.Requests() {
		if request.Method == http.MethodPut {
			puts = append(puts, request)
		}
	}
	return puts
}

func newIntegrationStore(t *testing.T) *s3fake.Server {
	t.Helper()
	return s3fake.NewServer(t, s3fake.Config{
		AccessKeyID: integrationAccessKeyID, SecretAccessKey: integrationSecretAccessKey, Region: integrationRegion,
		Buckets: []string{integrationBucket},
	})
}

// newFakeStoreClient addresses the fake with the example's static connection and default bucket.
func newFakeStoreClient(t *testing.T, store *s3fake.Server) *s3.Client {
	t.Helper()
	client, err := s3.New(s3.Config{Endpoint: store.URL, Region: integrationRegion, DefaultBucket: integrationBucket},
		sdkgo.StaticCredentialProvider[s3.Credentials]{exampleConnection: {
			AccessKeyID: sdkgo.NewSecretString(integrationAccessKeyID), SecretAccessKey: sdkgo.NewSecretString(integrationSecretAccessKey),
		}})
	require.NoError(t, err)
	return client
}
