//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/s3mock"
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
