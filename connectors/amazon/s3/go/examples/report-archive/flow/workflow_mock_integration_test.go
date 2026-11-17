//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go/s3mock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestReportArchiveUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := s3mock.New(t, ConnectionName)
	flow, harness := newReportArchiveHarnessWithConnection(t, mock.Connection())
	result := runReportArchive(t, integrationContext(t), harness.client, flow,
		"mock-report-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{
			ReportID: "ops-weekly", Title: "Ops weekly", Highlights: []string{"Refunds above 500 need approval."},
		})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusArchived, result.outcome.Status)
	require.True(t, result.outcome.IsTextVerified, "the Flow compares the returned text with its generated report")
	require.Equal(t, []string{"reports/ops-weekly.md"}, result.outcome.ArchivedReportKeys)
	require.True(t, result.outcome.HasMoreArchivedReports)
	puts := mock.PutObject().Calls()
	require.Len(t, puts, 1)
	require.Equal(t, s3.PutObjectInput{
		Key: "reports/ops-weekly.md", ContentType: ReportContentType,
		TextContent: "# Ops weekly\n\nReport ID: ops-weekly\n\n- Refunds above 500 need approval.\n",
		Metadata:    map[string]string{"report-id": "ops-weekly", "generator": GeneratorMetadataValue}, IsCreateOnly: true,
	}, puts[0].Input)
	require.Equal(t, s3.PutObjectBranchStored, puts[0].Branch)
	heads := mock.HeadObject().Calls()
	require.Len(t, heads, 1)
	require.Equal(t, s3.HeadObjectInput{Bucket: "acme-reports", Key: "reports/ops-weekly.md"}, heads[0].Input)
	reads := mock.GetObjectText().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, s3.GetObjectTextInput{Bucket: "acme-reports", Key: "reports/ops-weekly.md"}, reads[0].Input)
	lists := mock.ListObjects().Calls()
	require.Len(t, lists, 1, "the Flow intentionally uses only the first archive page")
	require.Equal(t, s3.ListObjectsInput{Bucket: "acme-reports", Prefix: "reports/", Delimiter: "/", MaxKeys: 10}, lists[0].Input)
}

func TestReportArchiveDeletesVerifiedReportWithRealDex(t *testing.T) {
	mock := s3mock.New(t, ConnectionName)
	flow, harness := newReportArchiveHarnessWithConnection(t, mock.Connection())
	result := runReportArchive(t, integrationContext(t), harness.client, flow,
		"mock-delete-report-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{
			ReportID: "ops-weekly", Title: "Ops weekly", ShouldDeleteAfterVerification: true,
		})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.True(t, result.outcome.IsDeleted)
	require.Len(t, mock.DeleteObject().Calls(), 1)
	require.Equal(t, s3.DeleteObjectInput{Key: "reports/ops-weekly.md"}, mock.DeleteObject().Calls()[0].Input)
}

func TestBucketInventoryUsesMockWithRealDex(t *testing.T) {
	mock := s3mock.New(t, ConnectionName)
	_, harness := newReportArchiveHarnessWithConnection(t, mock.Connection())
	flow := NewBucketInventoryFlow(mock.Connection())
	flowID := "mock-buckets-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ctx := integrationContext(t)
	_, err := harness.client.StartFlow(ctx, flow, flowID, BucketInventoryInput{MaxBuckets: 10}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var page s3.ListBucketsOutput
	require.NoError(t, result.DecodeSingleOutput(&page))
	require.Equal(t, "acme-reports", page.Buckets[0].Name)
	require.Len(t, mock.ListBuckets().Calls(), 1)
}

func TestObjectCopyReadsBytesWithMockAndRealDex(t *testing.T) {
	mock := s3mock.New(t, ConnectionName)
	_, harness := newReportArchiveHarnessWithConnection(t, mock.Connection())
	flow := NewObjectCopyFlow(mock.Connection())
	flowID := "mock-copy-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ctx := integrationContext(t)
	input := ObjectCopyInput{SourceBucket: "acme-reports", SourceKey: "reports/ops-weekly.md", DestinationBucket: "acme-archive", DestinationKey: "reports/ops-weekly.md", ContentType: "text/markdown"}
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var object s3.ObjectBytes
	require.NoError(t, result.DecodeSingleOutput(&object))
	require.Equal(t, []byte("weekly"), object.Bytes)
	require.Len(t, mock.CopyObject().Calls(), 1)
	require.Len(t, mock.GetObjectBytes().Calls(), 1)
}

func TestFolderLifecycleUsesMockWithRealDex(t *testing.T) {
	mock := s3mock.New(t, ConnectionName)
	_, harness := newReportArchiveHarnessWithConnection(t, mock.Connection())
	flow := NewFolderLifecycleFlow(mock.Connection())
	flowID := "mock-folder-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ctx := integrationContext(t)
	_, err := harness.client.StartFlow(ctx, flow, flowID, FolderLifecycleInput{Prefix: "reports/2026/"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var deleted s3.DeletedFolder
	require.NoError(t, result.DecodeSingleOutput(&deleted))
	require.Equal(t, "reports/2026/", deleted.Prefix)
	require.Len(t, mock.CreateFolder().Calls(), 1)
	require.Len(t, mock.DeleteFolder().Calls(), 1)
}
