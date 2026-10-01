// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewArchiveRequestGeneratesADeterministicReportAndKey(t *testing.T) {
	input := Input{ReportID: "2026-09-ops-weekly", Title: " Ops weekly ", Highlights: []string{"Refunds above 500 need approval.", " Two incidents closed. "}}
	request, err := NewArchiveRequest(input)
	require.NoError(t, err)
	require.Equal(t, ArchiveRequest{
		ReportID: "2026-09-ops-weekly", Key: "reports/2026-09-ops-weekly.md", IsCreateOnly: true,
		Text: "# Ops weekly\n\nReport ID: 2026-09-ops-weekly\n\n- Refunds above 500 need approval.\n- Two incidents closed.\n",
	}, request)
	again, err := NewArchiveRequest(input)
	require.NoError(t, err)
	require.Equal(t, request, again, "a retried start Step regenerates identical bytes")

	replace, err := NewArchiveRequest(Input{ReportID: "r1", Title: "T", Bucket: "acme-archive", ShouldReplaceExisting: true})
	require.NoError(t, err)
	require.False(t, replace.IsCreateOnly)
	require.Equal(t, "acme-archive", replace.Bucket)
	require.Equal(t, "# T\n\nReport ID: r1\n", replace.Text)
}

func TestNewArchiveRequestRejectsUnsafeInput(t *testing.T) {
	for name, input := range map[string]Input{
		"a blank report ID":        {Title: "T"},
		"a report ID with a slash": {ReportID: "../secret", Title: "T"},
		"an uppercase report ID":   {ReportID: "Q3", Title: "T"},
		"a blank title":            {ReportID: "r1", Title: " "},
		"a multi-line title":       {ReportID: "r1", Title: "T\n# injected"},
		"a blank highlight":        {ReportID: "r1", Title: "T", Highlights: []string{" "}},
		"too many highlights":      {ReportID: "r1", Title: "T", Highlights: make([]string, 21)},
		"an overlong highlight":    {ReportID: "r1", Title: "T", Highlights: []string{strings.Repeat("a", 501)}},
	} {
		_, err := NewArchiveRequest(input)
		require.Error(t, err, name)
	}
}

func TestConnectorMappersArchiveOnceAndReadBackTheSameKey(t *testing.T) {
	request := ArchiveRequest{ReportID: "r1", Key: "reports/r1.md", Text: "# T\n", IsCreateOnly: true}
	require.Equal(t, s3.PutObjectInput{
		Key: "reports/r1.md", ContentType: ReportContentType, TextContent: "# T\n", IsCreateOnly: true,
		Metadata: map[string]string{"report-id": "r1", "generator": GeneratorMetadataValue},
	}, MapToPutObjectInput(request))
	location := ObjectLocation{Bucket: "acme-reports", Key: "reports/r1.md"}
	require.Equal(t, s3.HeadObjectInput{Bucket: "acme-reports", Key: "reports/r1.md"}, MapToHeadObjectInput(location))
	require.Equal(t, s3.GetObjectTextInput{Bucket: "acme-reports", Key: "reports/r1.md"}, MapToGetObjectTextInput(location))
	require.Equal(t, s3.ListObjectsInput{Bucket: "acme-reports", Prefix: ReportPrefix, Delimiter: "/", MaxKeys: archiveListingPageSize},
		MapToListObjectsInput(location))
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(s3.Connection{})))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordArchiveRequest{}))
	require.Equal(t, recordArchiveStepType, dex.GetFinalStepType[s3.PutObjectResult](recordArchiveOutcome{}))
	require.Equal(t, recordMetadataStepType, dex.GetFinalStepType[s3.HeadObjectResult](recordReportMetadata{}))
	require.Equal(t, verifyTextStepType, dex.GetFinalStepType[s3.GetObjectTextResult](verifyReportText{}))
	require.Equal(t, completeArchiveStepType, dex.GetFinalStepType[s3.ListObjectsResult](completeReportArchive{}))
	wait, err := recordArchiveRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := s3.New(s3.Config{}, sdkgo.StaticCredentialProvider[s3.Credentials]{})
	require.NoError(t, err)
	connection, err := s3.NewConnection(client, sdkgo.ConnectionRef{Provider: "amazon-s3", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := s3.NewConnection(client, sdkgo.ConnectionRef{Provider: "amazon-s3", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
