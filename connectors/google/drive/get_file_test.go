// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const contractFileResource = `{"id":"file_contract","name":"Meridian MSA signed.pdf","mimeType":"application/pdf",` +
	`"description":"Signed master agreement","parents":["fld_sales"],"createdTime":"2025-11-02T12:00:00Z",` +
	`"modifiedTime":"2026-01-20T09:00:00.123Z","size":"184320","md5Checksum":"abc123","version":"7",` +
	`"webViewLink":"https://drive.google.com/file/d/file_contract/view","trashed":false,` +
	`"owners":[{"displayName":"Alice","emailAddress":"alice@example.com"}],` +
	`"lastModifyingUser":{"displayName":"Bob","emailAddress":"bob@example.com"}}`

func TestGetFileReturnsTypedMetadata(t *testing.T) {
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, contractFileResource)
	})
	client := newDriveClient(t, fake.URL)

	result, err := sdkgo.RunQuery(newDexContext("get-file"), client.GetFile(), driveConnection, drive.GetFileInput{FileID: "file_contract"})
	require.NoError(t, err)
	require.Equal(t, drive.GetFileBranchFound, result.Branch)
	require.Equal(t, drive.File{
		ID: "file_contract", Name: "Meridian MSA signed.pdf", MimeType: "application/pdf", Description: "Signed master agreement",
		Parents: []string{"fld_sales"}, CreatedTime: time.Date(2025, time.November, 2, 12, 0, 0, 0, time.UTC),
		ModifiedTime: time.Date(2026, time.January, 20, 9, 0, 0, 123000000, time.UTC), SizeBytes: 184320,
		MD5Checksum: "abc123", Version: 7, WebViewLink: "https://drive.google.com/file/d/file_contract/view",
		Owners:            []drive.FileUser{{DisplayName: "Alice", EmailAddress: "alice@example.com"}},
		LastModifyingUser: &drive.FileUser{DisplayName: "Bob", EmailAddress: "bob@example.com"},
	}, result.Value)
	require.Equal(t, "file_contract", result.Receipt.ProviderObjectID)

	request := fake.recorded()[0]
	require.Equal(t, "/drive/v3/files/file_contract", request.path)
	require.Equal(t, []string{"true"}, request.query["supportsAllDrives"])
	require.Contains(t, request.query["fields"][0], "shortcutDetails(targetId,targetMimeType)")
}

func TestGetFileExposesShortcutTargets(t *testing.T) {
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, `{"id":"shortcut","name":"Policy shortcut","mimeType":"application/vnd.google-apps.shortcut",`+
			`"shortcutDetails":{"targetId":"file_policy","targetMimeType":"application/vnd.google-apps.spreadsheet"}}`)
	})
	client := newDriveClient(t, fake.URL)
	result, err := sdkgo.RunQuery(newDexContext("get-shortcut"), client.GetFile(), driveConnection, drive.GetFileInput{FileID: "shortcut"})
	require.NoError(t, err)
	require.Equal(t, &drive.ShortcutTarget{TargetFileID: "file_policy", TargetMimeType: "application/vnd.google-apps.spreadsheet"}, result.Value.Shortcut)
}

func TestGetFileClassifiesMissingInvalidAndMalformedFiles(t *testing.T) {
	for _, test := range []struct {
		name       string
		fileID     string
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
		wantCalls  int
	}{
		{name: "missing file", fileID: "file_nope", status: http.StatusNotFound, body: `{"error":{"errors":[{"reason":"notFound"}]}}`, wantBranch: drive.GetFileBranchNotFound, wantKind: sdkgo.FailureNotFound, wantCalls: 1},
		{name: "invalid size", fileID: "file_contract", status: http.StatusOK, body: `{"id":"file_contract","name":"A","mimeType":"text/plain","size":"many"}`, wantBranch: drive.GetFileBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "different file ID", fileID: "file_contract", status: http.StatusOK, body: `{"id":"other","name":"A","mimeType":"text/plain"}`, wantBranch: drive.GetFileBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "path traversal ID", fileID: "../about", wantBranch: drive.GetFileBranchDefect, wantKind: sdkgo.FailureValidation},
		{name: "blank ID", fileID: "", wantBranch: drive.GetFileBranchDefect, wantKind: sdkgo.FailureValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newDriveClient(t, fake.URL)
			result, err := sdkgo.RunQuery(newDexContext("get-file-"+test.name), client.GetFile(), driveConnection, drive.GetFileInput{FileID: test.fileID})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.Len(t, fake.recorded(), test.wantCalls)
		})
	}
}
