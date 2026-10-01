// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// textFileDrive serves one file's metadata and its export or download content.
func textFileDrive(t *testing.T, metadata string, contentStatus int, content string) *fakeDrive {
	t.Helper()
	return newFakeDrive(t, func(response http.ResponseWriter, request *http.Request, _ []byte) {
		isContentRequest := strings.HasSuffix(request.URL.Path, "/export") || request.URL.Query().Get("alt") == "media"
		if !isContentRequest {
			writeJSON(t, response, http.StatusOK, metadata)
			return
		}
		response.WriteHeader(contentStatus)
		_, err := response.Write([]byte(content))
		require.NoError(t, err)
	})
}

func TestReadFileTextExportsGoogleWorkspaceFiles(t *testing.T) {
	for _, test := range []struct {
		name           string
		mimeType       string
		exportMimeType string
		content        string
		wantText       string
	}{
		{name: "document", mimeType: "application/vnd.google-apps.document", exportMimeType: "text/plain", content: "\xef\xbb\xbfOps Policy\r\nRule 1", wantText: "Ops Policy\r\nRule 1"},
		{name: "spreadsheet", mimeType: "application/vnd.google-apps.spreadsheet", exportMimeType: "text/csv", content: "rule,owner\nrefunds,finance\n", wantText: "rule,owner\nrefunds,finance\n"},
		{name: "presentation", mimeType: "application/vnd.google-apps.presentation", exportMimeType: "text/plain", content: "Slide one", wantText: "Slide one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := textFileDrive(t, `{"id":"file_policy","name":"Ops Policy","mimeType":"`+test.mimeType+`"}`, http.StatusOK, test.content)
			client := newDriveClient(t, fake.URL)
			result, err := sdkgo.RunQuery(newDexContext("read-export-"+test.name), client.ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "file_policy"})
			require.NoError(t, err)
			require.Equal(t, drive.ReadFileTextBranchRead, result.Branch)
			require.Equal(t, drive.ReadFileTextOutput{
				FileID: "file_policy", Name: "Ops Policy", MimeType: test.mimeType, TextMimeType: test.exportMimeType,
				Text: test.wantText, ByteCount: int64(len(test.wantText)),
			}, result.Value)
			requests := fake.recorded()
			require.Len(t, requests, 2)
			require.Equal(t, "/drive/v3/files/file_policy/export", requests[1].path)
			require.Equal(t, []string{test.exportMimeType}, requests[1].query["mimeType"])
		})
	}
}

func TestReadFileTextDownloadsSmallTextFiles(t *testing.T) {
	for _, mimeType := range []string{"text/markdown", "application/json"} {
		t.Run(mimeType, func(t *testing.T) {
			fake := textFileDrive(t, `{"id":"notes","name":"notes","mimeType":"`+mimeType+`","size":"12"}`, http.StatusOK, `{"ok": true}`)
			client := newDriveClient(t, fake.URL)
			result, err := sdkgo.RunQuery(newDexContext("read-download"), client.ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "notes"})
			require.NoError(t, err)
			require.Equal(t, drive.ReadFileTextBranchRead, result.Branch)
			require.Equal(t, `{"ok": true}`, result.Value.Text)
			require.Equal(t, mimeType, result.Value.TextMimeType)
			require.Equal(t, []string{"media"}, fake.recorded()[1].query["alt"])
		})
	}
}

func TestReadFileTextRejectsNonTextFilesWithoutDownloadingThem(t *testing.T) {
	for _, mimeType := range []string{
		"application/pdf", "image/png", "application/vnd.google-apps.folder",
		"application/vnd.google-apps.shortcut", "application/vnd.google-apps.form",
	} {
		t.Run(mimeType, func(t *testing.T) {
			fake := textFileDrive(t, `{"id":"file_contract","name":"Contract","mimeType":"`+mimeType+`","size":"10"}`, http.StatusOK, "binary")
			client := newDriveClient(t, fake.URL)
			result, err := sdkgo.RunQuery(newDexContext("read-binary"), client.ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "file_contract"})
			require.NoError(t, err)
			require.Equal(t, drive.ReadFileTextBranchUnsupportedContent, result.Branch)
			require.Equal(t, drive.ReadFileTextOutput{FileID: "file_contract", Name: "Contract", MimeType: mimeType}, result.Value)
			require.Len(t, fake.recorded(), 1)
		})
	}
}

func TestReadFileTextSelectsTooLargeForOversizedText(t *testing.T) {
	limitedConfig := drive.Config{MaxTextBytes: 8}
	for _, test := range []struct {
		name          string
		metadata      string
		contentStatus int
		content       string
		wantRequests  int
	}{
		{name: "reported size above limit", metadata: `{"id":"big","name":"big.txt","mimeType":"text/plain","size":"9"}`, contentStatus: http.StatusOK, content: "123456789", wantRequests: 1},
		{name: "download above limit", metadata: `{"id":"big","name":"big.txt","mimeType":"text/plain"}`, contentStatus: http.StatusOK, content: "123456789", wantRequests: 2},
		{name: "export above limit", metadata: `{"id":"big","name":"Big doc","mimeType":"application/vnd.google-apps.document"}`, contentStatus: http.StatusOK, content: "123456789", wantRequests: 2},
		{name: "Google export limit", metadata: `{"id":"big","name":"Big doc","mimeType":"application/vnd.google-apps.document"}`, contentStatus: http.StatusForbidden, content: `{"error":{"errors":[{"reason":"exportSizeLimitExceeded"}]}}`, wantRequests: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := textFileDrive(t, test.metadata, test.contentStatus, test.content)
			client := newDriveClient(t, fake.URL, limitedConfig)
			result, err := sdkgo.RunQuery(newDexContext("read-too-large"), client.ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "big"})
			require.NoError(t, err)
			require.Equal(t, drive.ReadFileTextBranchTooLarge, result.Branch)
			require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
			require.Equal(t, "big", result.Value.FileID)
			require.Empty(t, result.Value.Text)
			require.Len(t, fake.recorded(), test.wantRequests)
		})
	}
}

func TestReadFileTextClassifiesContentErrorsByStatusEvenAboveTheTextLimit(t *testing.T) {
	largeErrorBody := `{"error":{"code":0,"message":"` + strings.Repeat("x", 256) + `"}}`
	metadata := `{"id":"notes","name":"notes.txt","mimeType":"text/plain","size":"4"}`
	missing := textFileDrive(t, metadata, http.StatusNotFound, largeErrorBody)
	result, err := sdkgo.RunQuery(newDexContext("read-content-missing"), newDriveClient(t, missing.URL, drive.Config{MaxTextBytes: 8}).ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "notes"})
	require.NoError(t, err)
	require.Equal(t, drive.ReadFileTextBranchNotFound, result.Branch)

	unavailable := textFileDrive(t, metadata, http.StatusServiceUnavailable, largeErrorBody)
	_, err = sdkgo.RunQuery(newDexContext("read-content-unavailable"), newDriveClient(t, unavailable.URL, drive.Config{MaxTextBytes: 8}).ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "notes"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestReadFileTextRejectsContentThatIsNotUTF8Text(t *testing.T) {
	for name, content := range map[string]string{"invalid UTF-8": "caf\xe9", "NUL byte": "a\x00b"} {
		t.Run(name, func(t *testing.T) {
			fake := textFileDrive(t, `{"id":"latin","name":"latin.txt","mimeType":"text/plain"}`, http.StatusOK, content)
			client := newDriveClient(t, fake.URL)
			result, err := sdkgo.RunQuery(newDexContext("read-not-utf8"), client.ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "latin"})
			require.NoError(t, err)
			require.Equal(t, drive.ReadFileTextBranchUnsupportedContent, result.Branch)
			require.Empty(t, result.Value.Text)
		})
	}
}

func TestReadFileTextClassifiesMissingFilesAndInvalidIDs(t *testing.T) {
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusNotFound, `{"error":{"errors":[{"reason":"notFound"}]}}`)
	})
	client := newDriveClient(t, fake.URL)
	missing, err := sdkgo.RunQuery(newDexContext("read-missing"), client.ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "file_nope"})
	require.NoError(t, err)
	require.Equal(t, drive.ReadFileTextBranchNotFound, missing.Branch)

	invalid, err := sdkgo.RunQuery(newDexContext("read-invalid"), client.ReadFileText(), driveConnection, drive.ReadFileTextInput{FileID: "a/b"})
	require.NoError(t, err)
	require.Equal(t, drive.ReadFileTextBranchDefect, invalid.Branch)
	require.Len(t, fake.recorded(), 1)
}
