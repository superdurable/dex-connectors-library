//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/internal/s3live"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestReportArchiveExampleRunsAgainstALiveStoreWithRealDex uses the S3 API that S3_CONNECTOR_TEST_* names.
func TestReportArchiveExampleRunsAgainstALiveStoreWithRealDex(t *testing.T) {
	store := s3live.RequireStore(t)
	providerClient, err := s3.New(s3.Config{
		Endpoint: store.Endpoint, Region: store.Region, AddressingStyle: s3.AddressingStyle(store.AddressingStyle), DefaultBucket: store.Bucket,
	}, sdkgo.StaticCredentialProvider[s3.Credentials]{exampleConnection: {
		AccessKeyID: sdkgo.NewSecretString(store.AccessKeyID), SecretAccessKey: sdkgo.NewSecretString(store.SecretAccessKey),
		SessionToken: sdkgo.NewSecretString(store.SessionToken),
	}})
	require.NoError(t, err)
	flow, harness := newReportArchiveHarness(t, providerClient)
	ctx := integrationContext(t)
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	reportID := "live-" + runID
	store.DeleteAfterTest(t, ReportPrefix+reportID+".md")

	archived := runReportArchive(t, ctx, harness.client, flow, "live-archived-"+runID, Input{
		ReportID: reportID, Title: "Live archive", Highlights: []string{"Archived by the Amazon S3 connector live test."},
	})
	require.Equal(t, dex.FlowCompleted, archived.status)
	require.Equal(t, StatusArchived, archived.outcome.Status)
	require.True(t, archived.outcome.IsTextVerified)
	require.Equal(t, archived.outcome.Stored.ETag, archived.outcome.Report.ETag)
	require.Equal(t, reportID, archived.outcome.Report.Metadata["report-id"])
	require.Contains(t, archived.outcome.ArchivedReportKeys, ReportPrefix+reportID+".md")

	again := runReportArchive(t, ctx, harness.client, flow, "live-again-"+runID, Input{ReportID: reportID, Title: "Live archive (revised)"})
	require.Equal(t, dex.FlowCompleted, again.status)
	require.Equal(t, StatusAlreadyArchived, again.outcome.Status)
	require.Equal(t, archived.outcome.Stored.ETag, again.outcome.Stored.ETag)
	t.Logf("live archive: etag=%s alreadyArchived etag=%s", archived.outcome.Stored.ETag, again.outcome.Stored.ETag)
}
