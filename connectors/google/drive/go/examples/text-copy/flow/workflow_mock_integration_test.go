//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive/drivemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTextCopyUsesDriveMockWithRealDex(t *testing.T) {
	mock := drivemock.New(t, ConnectionName)
	mock.SearchFiles().Respond(drivemock.SearchFilesFound(drive.SearchFilesOutput{
		Files: []drive.FileSummary{{ID: "file_policy", Name: "Ops Policy", MimeType: "application/vnd.google-apps.document"}},
	}, nil))
	flow, harness := newDriveIntegrationHarnessForConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "mock-copy-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	result := runTextCopy(t, ctx, harness.client, flow, flowID, Input{SourceName: "Ops Policy", CopyName: "Ops Policy copy.txt"})
	require.Equal(t, dex.FlowCompleted, result.status, result.failureReason)
	require.Equal(t, StatusCopied, result.outcome.Status)

	searchCalls := mock.SearchFiles().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, drive.SearchFilesInput{Name: "Ops Policy", NameMatch: drive.NameMatchExact, ParentFolderID: sourceFolderID, PageSize: 10}, searchCalls[0].Input)
	readCalls := mock.ReadFileText().Calls()
	require.Len(t, readCalls, 1)
	require.Equal(t, drive.ReadFileTextInput{FileID: "file_policy"}, readCalls[0].Input)
	uploadCalls := mock.UploadFile().Calls()
	require.Len(t, uploadCalls, 1)
	require.Equal(t, drive.UploadFileInput{Name: "Ops Policy copy.txt", ParentFolderID: destinationFolderID, MimeType: "text/plain", TextContent: "Ops Policy\nRefunds above 500 need approval."}, uploadCalls[0].Input)
	require.Equal(t, drive.UploadFileBranchUploaded, uploadCalls[0].Branch)
	getCalls := mock.GetFile().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, drive.GetFileInput{FileID: "copied_policy"}, getCalls[0].Input)
}
