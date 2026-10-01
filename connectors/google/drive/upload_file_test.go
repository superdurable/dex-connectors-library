// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var idempotencyQueryPattern = regexp.MustCompile(`^appProperties has \{ key='dexIdempotencyKey' and value='([^'\\]+)' \}$`)

// uploadOutcome selects how the stateful fake answers one upload request.
type uploadOutcome int

const (
	uploadCreates uploadOutcome = iota
	uploadCreatesThenAnswers500
	uploadRejectsWith429
	uploadRejectsMissingParent
	uploadCreatesThenDropsConnection
	uploadCreatesThenAnswersInvalidJSON
)

// multipartUpload is one decoded Drive multipart upload.
type multipartUpload struct {
	metadata         map[string]any
	mediaContentType string
	media            []byte
}

// uploadDrive is a stateful fake that stores created files with their app properties.
type uploadDrive struct {
	t        *testing.T
	fake     *fakeDrive
	mutex    sync.Mutex
	outcomes []uploadOutcome
	files    []map[string]any
	uploads  []multipartUpload
}

func newUploadDrive(t *testing.T, outcomes ...uploadOutcome) *uploadDrive {
	t.Helper()
	provider := &uploadDrive{t: t, outcomes: outcomes}
	provider.fake = newFakeDrive(t, provider.serveHTTP)
	return provider
}

func (provider *uploadDrive) serveHTTP(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/drive/v3/files":
		match := idempotencyQueryPattern.FindStringSubmatch(request.URL.Query().Get("q"))
		require.NotNil(provider.t, match, "lookup must query only the idempotency app property")
		matches := []map[string]any{}
		for _, file := range provider.files {
			if file["appProperties"].(map[string]any)["dexIdempotencyKey"] == match[1] {
				matches = append(matches, file)
			}
		}
		provider.writeJSONLocked(response, http.StatusOK, map[string]any{"files": matches})
	case request.Method == http.MethodPost && request.URL.Path == "/upload/drive/v3/files":
		require.Equal(provider.t, "multipart", request.URL.Query().Get("uploadType"))
		upload := decodeMultipartUpload(provider.t, request.Header.Get("Content-Type"), body)
		provider.uploads = append(provider.uploads, upload)
		outcome := uploadCreates
		if len(provider.outcomes) > 0 {
			outcome, provider.outcomes = provider.outcomes[0], provider.outcomes[1:]
		}
		switch outcome {
		case uploadRejectsWith429:
			provider.writeJSONLocked(response, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": 429}})
			return
		case uploadRejectsMissingParent:
			provider.writeJSONLocked(response, http.StatusNotFound, map[string]any{"error": map[string]any{"errors": []any{map[string]any{"reason": "notFound", "message": "File not found: SENTINEL"}}}})
			return
		}
		file := map[string]any{
			"id": fmt.Sprintf("created-%d", len(provider.files)+1), "name": upload.metadata["name"], "mimeType": upload.metadata["mimeType"],
			"parents": upload.metadata["parents"], "appProperties": upload.metadata["appProperties"],
			"createdTime": "2026-09-30T12:00:00Z", "size": fmt.Sprint(len(upload.media)),
		}
		provider.files = append(provider.files, file)
		switch outcome {
		case uploadCreatesThenAnswers500:
			provider.writeJSONLocked(response, http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": 500}})
		case uploadCreatesThenDropsConnection:
			connection, _, err := response.(http.Hijacker).Hijack()
			require.NoError(provider.t, err)
			require.NoError(provider.t, connection.Close())
		case uploadCreatesThenAnswersInvalidJSON:
			response.WriteHeader(http.StatusOK)
			_, err := response.Write([]byte(`{"id":`))
			require.NoError(provider.t, err)
		default:
			provider.writeJSONLocked(response, http.StatusOK, file)
		}
	default:
		http.NotFound(response, request)
	}
}

func (provider *uploadDrive) writeJSONLocked(response http.ResponseWriter, status int, body any) {
	contents, err := json.Marshal(body)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err = response.Write(contents)
	require.NoError(provider.t, err)
}

func (provider *uploadDrive) fileCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.files)
}

func (provider *uploadDrive) uploadCount() int {
	return len(provider.recordedUploads())
}

func (provider *uploadDrive) recordedUploads() []multipartUpload {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]multipartUpload(nil), provider.uploads...)
}

func decodeMultipartUpload(t *testing.T, contentType string, body []byte) multipartUpload {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/related", mediaType)
	reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
	metadataPart, err := reader.NextPart()
	require.NoError(t, err)
	require.Equal(t, "application/json; charset=UTF-8", metadataPart.Header.Get("Content-Type"))
	var upload multipartUpload
	require.NoError(t, json.NewDecoder(metadataPart).Decode(&upload.metadata))
	mediaPart, err := reader.NextPart()
	require.NoError(t, err)
	upload.mediaContentType = mediaPart.Header.Get("Content-Type")
	upload.media, err = io.ReadAll(mediaPart)
	require.NoError(t, err)
	_, err = reader.NextPart()
	require.ErrorIs(t, err, io.EOF)
	return upload
}

func TestUploadFileSendsMultipartMetadataWithTheIdempotencyKey(t *testing.T) {
	provider := newUploadDrive(t)
	client := newDriveClient(t, provider.fake.URL)

	result, err := sdkgo.RunMutation(newDexContext("upload-created"), client.UploadFile(), driveConnection, drive.UploadFileInput{
		Name: "Jan-2026 reconciliation.csv", ParentFolderID: "fld_finance", MimeType: "text/csv",
		TextContent: "date,amount\n2026-01-28,250.00\n",
	})
	require.NoError(t, err)
	require.Equal(t, drive.UploadFileBranchUploaded, result.Branch)
	require.False(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, "created-1", result.Value.File.ID)
	require.Equal(t, []string{"fld_finance"}, result.Value.File.Parents)
	require.Equal(t, int64(len("date,amount\n2026-01-28,250.00\n")), result.Value.File.SizeBytes)
	require.Equal(t, "created-1", result.Receipt.ProviderObjectID)
	require.NotEmpty(t, result.Receipt.IdempotencyKey)

	upload := provider.recordedUploads()[0]
	require.Equal(t, map[string]any{
		"name": "Jan-2026 reconciliation.csv", "mimeType": "text/csv", "parents": []any{"fld_finance"},
		"appProperties": map[string]any{"dexIdempotencyKey": string(result.Receipt.IdempotencyKey)},
	}, upload.metadata)
	require.Equal(t, "text/csv", upload.mediaContentType)
	require.Equal(t, "date,amount\n2026-01-28,250.00\n", string(upload.media))
	requests := provider.fake.recorded()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, []string{"true"}, requests[1].query["supportsAllDrives"])
}

func TestUploadFileUploadsBytesToTheMyDriveRootWithoutParents(t *testing.T) {
	provider := newUploadDrive(t)
	client := newDriveClient(t, provider.fake.URL)
	content := []byte{0x89, 'P', 'N', 'G', 0x00}
	result, err := sdkgo.RunMutation(newDexContext("upload-bytes"), client.UploadFile(), driveConnection, drive.UploadFileInput{
		Name: "pixel.png", MimeType: "image/png", ByteContent: content,
	})
	require.NoError(t, err)
	require.Equal(t, drive.UploadFileBranchUploaded, result.Branch)
	upload := provider.recordedUploads()[0]
	require.NotContains(t, upload.metadata, "parents")
	require.Equal(t, content, upload.media)
}

func TestUploadFileRetryOfTheSameStepExecutionReusesTheEarlierFile(t *testing.T) {
	provider := newUploadDrive(t, uploadCreatesThenAnswers500)
	client := newDriveClient(t, provider.fake.URL)
	input := drive.UploadFileInput{Name: "report.txt", MimeType: "text/plain", TextContent: "report"}
	ctx := newDexContext("upload-retried")

	unknown, err := sdkgo.RunMutation(ctx, client.UploadFile(), driveConnection, input)
	require.NoError(t, err)
	require.Equal(t, drive.UploadFileBranchUncertain, unknown.Branch)
	require.Equal(t, sdkgo.FailureAvailability, unknown.Failure.Kind)

	recovered, err := sdkgo.RunMutation(ctx, client.UploadFile(), driveConnection, input)
	require.NoError(t, err)
	require.Equal(t, drive.UploadFileBranchUploaded, recovered.Branch)
	require.True(t, recovered.Value.IsFromEarlierAttempt)
	require.Equal(t, "created-1", recovered.Value.File.ID)
	require.Equal(t, unknown.Receipt.IdempotencyKey, recovered.Receipt.IdempotencyKey)
	require.Equal(t, 1, provider.fileCount())
	require.Equal(t, 1, provider.uploadCount())

	another, err := sdkgo.RunMutation(newDexContext("upload-new-step-execution"), client.UploadFile(), driveConnection, input)
	require.NoError(t, err)
	require.False(t, another.Value.IsFromEarlierAttempt)
	require.Equal(t, 2, provider.fileCount())
}

func TestUploadFileAmbiguousOutcomesAreUncertainAndNeverResent(t *testing.T) {
	for _, test := range []struct {
		name     string
		outcome  uploadOutcome
		wantKind sdkgo.FailureKind
	}{
		{name: "server error after dispatch", outcome: uploadCreatesThenAnswers500, wantKind: sdkgo.FailureAvailability},
		{name: "dropped connection", outcome: uploadCreatesThenDropsConnection, wantKind: sdkgo.FailureTransport},
		{name: "invalid success response", outcome: uploadCreatesThenAnswersInvalidJSON, wantKind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newUploadDrive(t, test.outcome)
			client := newDriveClient(t, provider.fake.URL)
			result, err := sdkgo.RunMutation(newDexContext("upload-ambiguous"), client.UploadFile(), driveConnection, drive.UploadFileInput{
				Name: "report.txt", MimeType: "text/plain", TextContent: "report",
			})
			require.NoError(t, err)
			require.Equal(t, drive.UploadFileBranchUncertain, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.Equal(t, 1, provider.uploadCount())
		})
	}
}

func TestUploadFileRetriesARateLimitedUploadAfterLookingUpAgain(t *testing.T) {
	provider := newUploadDrive(t, uploadRejectsWith429)
	client := newDriveClient(t, provider.fake.URL)
	ctx := newDexContext("upload-rate-limited")
	input := drive.UploadFileInput{Name: "report.txt", MimeType: "text/plain", TextContent: "report"}

	_, err := sdkgo.RunMutation(ctx, client.UploadFile(), driveConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	require.Equal(t, 0, provider.fileCount())

	result, err := sdkgo.RunMutation(ctx, client.UploadFile(), driveConnection, input)
	require.NoError(t, err)
	require.Equal(t, drive.UploadFileBranchUploaded, result.Branch)
	require.False(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, 1, provider.fileCount())
	require.Equal(t, 2, provider.fake.countRequests(http.MethodGet, "/drive/v3/files"))
}

func TestUploadFileRejectsAMissingParentFolderWithoutProviderText(t *testing.T) {
	provider := newUploadDrive(t, uploadRejectsMissingParent)
	client := newDriveClient(t, provider.fake.URL)
	result, err := sdkgo.RunMutation(newDexContext("upload-missing-parent"), client.UploadFile(), driveConnection, drive.UploadFileInput{
		Name: "report.txt", ParentFolderID: "fld_missing", MimeType: "text/plain", TextContent: "report",
	})
	require.NoError(t, err)
	require.Equal(t, drive.UploadFileBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "SENTINEL")
	require.Equal(t, 0, provider.fileCount())
}

func TestUploadFileRejectsInvalidInputWithoutProviderRequest(t *testing.T) {
	provider := newUploadDrive(t)
	client := newDriveClient(t, provider.fake.URL, drive.Config{MaxUploadBytes: 4})
	for name, input := range map[string]drive.UploadFileInput{
		"blank name":                 {Name: " ", MimeType: "text/plain"},
		"missing MIME type":          {Name: "a.txt"},
		"MIME type parameters":       {Name: "a.txt", MimeType: "text/plain; charset=utf-8"},
		"Google Workspace type":      {Name: "a", MimeType: "application/vnd.google-apps.document"},
		"text and bytes":             {Name: "a.txt", MimeType: "text/plain", TextContent: "a", ByteContent: []byte("b")},
		"content above upload limit": {Name: "a.txt", MimeType: "text/plain", TextContent: "12345"},
		"invalid parent folder":      {Name: "a.txt", MimeType: "text/plain", ParentFolderID: "a' in parents"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newDexContext("upload-invalid-"+name), client.UploadFile(), driveConnection, input)
			require.NoError(t, err)
			require.Equal(t, drive.UploadFileBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, provider.fake.recorded())
}

func TestUploadFileLookupFailuresNeverUpload(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantBranch sdkgo.BranchID
	}{
		{name: "lookup outage retries", status: http.StatusServiceUnavailable, body: `{}`},
		{name: "incomplete lookup retries", status: http.StatusOK, body: `{"files":[],"incompleteSearch":true}`},
		{name: "lookup rejection", status: http.StatusForbidden, body: `{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`, wantBranch: drive.UploadFileBranchProviderRejected},
		{name: "malformed lookup", status: http.StatusOK, body: `{"files":[{"id":""}]}`, wantBranch: drive.UploadFileBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newDriveClient(t, fake.URL)
			result, err := sdkgo.RunMutation(newDexContext("upload-lookup"), client.UploadFile(), driveConnection, drive.UploadFileInput{
				Name: "a.txt", MimeType: "text/plain", TextContent: "a",
			})
			if test.wantBranch == "" {
				var retry *sdkgo.RetryError
				require.True(t, errors.As(err, &retry))
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantBranch, result.Branch)
			}
			require.Equal(t, 0, fake.countRequests(http.MethodPost, "/upload/drive/v3/files"))
		})
	}
}

func TestUploadFileFailuresNeverRevealTheAccessToken(t *testing.T) {
	provider := newUploadDrive(t, uploadCreatesThenAnswers500)
	client := newDriveClient(t, provider.fake.URL)
	result, err := sdkgo.RunMutation(newDexContext("upload-secret-safe"), client.UploadFile(), driveConnection, drive.UploadFileInput{
		Name: "a.txt", MimeType: "text/plain", TextContent: "a",
	})
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), driveTestToken))
}
