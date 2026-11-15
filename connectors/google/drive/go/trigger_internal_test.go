// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var triggerTestCheckpoint = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func TestClassifyFileChangeDerivesKindFromFileTimes(t *testing.T) {
	old := triggerTestCheckpoint.Add(-time.Hour)
	recent := triggerTestCheckpoint.Add(time.Minute)
	withinLookback := triggerTestCheckpoint.Add(-time.Minute)
	for _, test := range []struct {
		name                      string
		file                      FileEvent
		created, updated, trashed string
	}{
		{name: "new upload", file: FileEvent{FileID: "f1", CreatedTime: recent, ModifiedTime: recent}, created: "f1@created"},
		{name: "upload keeps older local mtime", file: FileEvent{FileID: "f1", CreatedTime: recent, ModifiedTime: old}, created: "f1@created"},
		{name: "created then edited", file: FileEvent{FileID: "f1", CreatedTime: recent, ModifiedTime: recent.Add(time.Minute)},
			created: "f1@created", updated: "f1@modified@" + millis(recent.Add(time.Minute))},
		{name: "edit of an old file", file: FileEvent{FileID: "f1", CreatedTime: old, ModifiedTime: recent}, updated: "f1@modified@" + millis(recent)},
		{name: "late change record inside the lookback", file: FileEvent{FileID: "f1", CreatedTime: withinLookback, ModifiedTime: withinLookback}, created: "f1@created"},
		{name: "permission or view change", file: FileEvent{FileID: "f1", CreatedTime: old, ModifiedTime: old}},
		{name: "trashed with trashed time", file: FileEvent{FileID: "f1", CreatedTime: old, ModifiedTime: recent, IsTrashed: true, TrashedTime: recent},
			trashed: "f1@trashed@" + millis(recent)},
		{name: "trashed without trashed time uses the change time", file: FileEvent{FileID: "f1", CreatedTime: old, ModifiedTime: old, IsTrashed: true, ChangeTime: recent},
			trashed: "f1@trashed@" + millis(recent)},
		{name: "change to a long-trashed file", file: FileEvent{FileID: "f1", CreatedTime: old, ModifiedTime: old, IsTrashed: true, TrashedTime: old, ChangeTime: recent}},
	} {
		t.Run(test.name, func(t *testing.T) {
			change := classifyFileChange(test.file, triggerTestCheckpoint)
			require.Equal(t, test.created, eventID(change.created))
			require.Equal(t, test.updated, eventID(change.updated))
			require.Equal(t, test.trashed, eventID(change.trashed))
		})
	}
}

func TestFileTriggerConfigurationFiltersByDirectParentAndExactMimeType(t *testing.T) {
	file := FileEvent{FileID: "f1", MimeType: "application/pdf", Parents: []string{"folder_a"}}
	require.True(t, FileTriggerConfiguration{}.matches(file))
	require.True(t, FileTriggerConfiguration{FolderID: "folder_a", MimeType: "application/pdf"}.matches(file))
	require.False(t, FileTriggerConfiguration{FolderID: "folder_b"}.matches(file))
	require.False(t, FileTriggerConfiguration{MimeType: "text/plain"}.matches(file))
	require.NoError(t, FileTriggerConfiguration{FolderID: "folder_a", FolderName: "Invoices", MimeType: "application/vnd.google-apps.document"}.Validate())
	require.Error(t, FileTriggerConfiguration{FolderID: "https://drive.google.com/drive/folders/x"}.Validate())
	require.Error(t, FileTriggerConfiguration{MimeType: "Application/PDF"}.Validate())
	require.Error(t, FileTriggerConfiguration{MimeType: "text/plain; charset=utf-8"}.Validate())
}

func TestFileChangeFetcherSeedsAndKeepsCheckpointAcrossPagesOfOnePass(t *testing.T) {
	firstChange := triggerTestCheckpoint.Add(time.Minute)
	secondChange := triggerTestCheckpoint.Add(2 * time.Minute)
	var queries []map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		queries = append(queries, request.URL.Query())
		require.Equal(t, "Bearer token-SENTINEL", request.Header.Get("Authorization"))
		switch request.URL.Query().Get("pageToken") {
		case "":
			require.Equal(t, "/drive/v3/changes/startPageToken", request.URL.Path)
			_, _ = response.Write([]byte(`{"startPageToken":"100"}`))
		case "100":
			_, _ = response.Write([]byte(`{"nextPageToken":"101","changes":[` + changeJSON("f1", firstChange, firstChange) + `,{"changeType":"drive","driveId":"d1"},{"changeType":"file","fileId":"gone","removed":true}]}`))
		case "101":
			_, _ = response.Write([]byte(`{"newStartPageToken":"102","changes":[` + changeJSON("f2", secondChange, secondChange) + `]}`))
		}
	}))
	t.Cleanup(server.Close)
	fetcher := newTestFileChangeFetcher(t, server.URL)
	seeded, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Equal(t, `{"pageToken":"100","checkpoint":"2026-11-01T12:00:00Z"}`, seeded.Token)
	first, err := fetcher.FetchChanges(context.Background(), seeded)
	require.NoError(t, err)
	require.True(t, first.HasMorePages)
	require.Len(t, first.Changes, 1)
	require.Equal(t, "f1@created", eventID(first.Changes[0].created))
	require.Equal(t, `{"pageToken":"101","checkpoint":"2026-11-01T12:00:00Z","roundLatest":"2026-11-01T12:01:00Z"}`, first.Next.Token)
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.False(t, second.HasMorePages)
	require.Equal(t, "f2@created", eventID(second.Changes[0].created))
	require.Equal(t, `{"pageToken":"102","checkpoint":"2026-11-01T12:02:00Z"}`, second.Next.Token)
	listQuery := queries[1]
	for name, value := range map[string]string{
		"pageSize": "100", "spaces": "drive", "includeRemoved": "false", "includeItemsFromAllDrives": "true", "supportsAllDrives": "true",
	} {
		require.Equal(t, value, listQuery[name][0], name)
	}
	require.Contains(t, listQuery["fields"][0], "newStartPageToken")
	require.Equal(t, "true", queries[0]["supportsAllDrives"][0])
}

func TestFileChangeFetcherMapsProviderFailuresToPollSignals(t *testing.T) {
	status, body, retryAfter := http.StatusOK, "", ""
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if retryAfter != "" {
			response.Header().Set("Retry-After", retryAfter)
		}
		response.WriteHeader(status)
		_, _ = response.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	fetcher := newTestFileChangeFetcher(t, server.URL)
	cursor := pollingtrigger.Cursor{Token: `{"pageToken":"100","checkpoint":"2026-11-01T12:00:00Z"}`}

	status, retryAfter = http.StatusTooManyRequests, "7"
	_, err := fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	status, retryAfter, body = http.StatusForbidden, "", `{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}`
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.ErrorAs(t, err, &rateLimited)

	status, body = http.StatusBadRequest, `{"error":{"errors":[{"reason":"invalid"}]}}`
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err))
	body = `{"error":{"errors":[{"reason":"invalid","locationType":"parameter","location":"pageToken"}]}}`
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err))
	for _, rejected := range []int{http.StatusNotFound, http.StatusGone} {
		status, body = rejected, ""
		_, err = fetcher.FetchChanges(context.Background(), cursor)
		require.True(t, pollingtrigger.IsCursorInvalid(err), rejected)
	}
	status, body = http.StatusBadRequest, `{"error":{"errors":[{"reason":"invalidParameter","locationType":"parameter","location":"fields"}]}}`
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a 400 for another parameter would recur after every reseed")
	_, err = fetcher.SeedCursor(context.Background())
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a rejected seed is retried, not reseeded")

	status, body = http.StatusServiceUnavailable, ""
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
	require.NotErrorAs(t, err, &rateLimited)

	status, body = http.StatusOK, `{"changes":[]}`
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.ErrorContains(t, err, "neither a next page token nor a new start page token")

	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"pageToken":"100"}`})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
}

func newTestFileChangeFetcher(t *testing.T, endpoint string) *fileChangeFetcher {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "google", Name: "drive-files"}
	client, err := New(Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		connection: {AccessToken: sdkgo.NewSecretString("token-SENTINEL")},
	}, withTriggerTestClock(func() time.Time { return triggerTestCheckpoint }))
	require.NoError(t, err)
	return newFileChangeFetcher(client, connection, fileChangesFeedName)
}

func withTriggerTestClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

func changeJSON(fileID string, created, changed time.Time) string {
	return `{"changeType":"file","fileId":"` + fileID + `","time":"` + changed.Format(time.RFC3339Nano) + `","file":{"id":"` + fileID +
		`","name":"Invoice.pdf","mimeType":"application/pdf","parents":["folder_a"],"createdTime":"` + created.Format(time.RFC3339Nano) +
		`","modifiedTime":"` + created.Format(time.RFC3339Nano) + `","size":"42"}}`
}

func eventID(event *sdkgo.TriggerEvent[FileEvent]) string {
	if event == nil {
		return ""
	}
	return event.ID
}

func millis(value time.Time) string { return strconv.FormatInt(value.UnixMilli(), 10) }
