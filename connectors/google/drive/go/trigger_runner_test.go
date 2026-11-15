// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive/go/internal/drivechangetest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

// recordingFileTarget is a non-durable target that consumes every event.
type recordingFileTarget struct {
	mutex    sync.Mutex
	eventIDs []string
}

func (target *recordingFileTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[drive.FileEvent]) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	target.eventIDs = append(target.eventIDs, event.ID)
	return nil
}

func (target *recordingFileTarget) received() []string {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	return slices.Clone(target.eventIDs)
}

func newTriggerTestConnection(t *testing.T, changeLog *drivechangetest.ChangeLog) drive.Connection {
	t.Helper()
	client := newDriveClient(t, changeLog.URL, drive.Config{PollInterval: time.Second})
	connection, err := drive.NewConnection(client, driveConnection)
	require.NoError(t, err)
	return connection
}

// fileTriggerRunner is the Run method shared by FileTriggerRunner and the generated Trigger runners.
type fileTriggerRunner interface {
	Run(ctx context.Context) error
}

func startFileTriggerRunner(t *testing.T, runner fileTriggerRunner) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-result, context.Canceled)
	})
}

func TestFileTriggerRunnerSharesOneFeedAndRoutesEachBindingItsEvents(t *testing.T) {
	changeLog := drivechangetest.New(driveTestToken)
	t.Cleanup(changeLog.Close)
	hourAgo := drivechangetest.Now().Add(-time.Hour)
	for _, id := range []string{"old_report", "old_notes", "old_draft", "backlog_upload"} {
		changeLog.Put(drivechangetest.File{ID: id, Name: id, MimeType: "application/pdf", Parent: "folder_a", CreatedTime: hourAgo, ModifiedTime: hourAgo})
	}
	createdEverywhere, createdPDFInFolderA := &recordingFileTarget{}, &recordingFileTarget{}
	updatedEverywhere, trashedInFolderA := &recordingFileTarget{}, &recordingFileTarget{}
	runner, err := drive.NewFileTriggerRunner(drive.FileTriggerRunnerConfig{
		Connection: newTriggerTestConnection(t, changeLog), Cursor: pollingtrigger.NewMemoryCursorStore(),
		FileCreatedRoutes: []drive.FileTriggerRoute{
			{BindingName: "every-new-file", Target: createdEverywhere},
			{BindingName: "new-pdf-in-a", Configuration: drive.FileTriggerConfiguration{FolderID: "folder_a", FolderName: "A", MimeType: "application/pdf"}, Target: createdPDFInFolderA},
		},
		FileUpdatedRoutes: []drive.FileTriggerRoute{{BindingName: "every-edit", Target: updatedEverywhere}},
		FileTrashedRoutes: []drive.FileTriggerRoute{{BindingName: "trash-in-a", Configuration: drive.FileTriggerConfiguration{FolderID: "folder_a"}, Target: trashedInFolderA}},
	})
	require.NoError(t, err)
	startFileTriggerRunner(t, runner)
	require.Eventually(t, func() bool { return changeLog.ListPages() >= 1 }, 10*time.Second, 20*time.Millisecond)

	changeLog.CreateFile("new_pdf_a", "Invoice.pdf", "application/pdf", "folder_a")
	changeLog.CreateFile("new_doc_b", "Notes", "application/vnd.google-apps.document", "folder_b")
	changeLog.CreateFile("new_pdf_b", "Other.pdf", "application/pdf", "folder_b")
	editedAt := drivechangetest.Now()
	changeLog.ModifyFile("old_report", editedAt)
	changeLog.TouchFile("old_notes")
	changeLog.TrashFile("old_draft")

	wantCreated := []string{"new_pdf_a@created", "new_doc_b@created", "new_pdf_b@created"}
	require.Eventually(t, func() bool {
		return slices.Equal(createdEverywhere.received(), wantCreated) && len(updatedEverywhere.received()) == 1 && len(trashedInFolderA.received()) == 1
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, []string{"new_pdf_a@created"}, createdPDFInFolderA.received())
	require.Equal(t, []string{"old_report@modified@" + millis(editedAt)}, updatedEverywhere.received())
	require.Regexp(t, `^old_draft@trashed@\d+$`, trashedInFolderA.received()[0])

	// Later polls deliver nothing again, and every binding shares one seed and one list per poll.
	pages := changeLog.ListPages()
	require.Eventually(t, func() bool { return changeLog.ListPages() >= pages+2 }, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, wantCreated, createdEverywhere.received())
	require.Len(t, updatedEverywhere.received(), 1)
	require.Equal(t, 1, changeLog.Requests("/drive/v3/changes/startPageToken"))
}

func TestFileTrashedUsesSharedDriveTrashTimeAndMyDriveChangeTime(t *testing.T) {
	changeLog := drivechangetest.New(driveTestToken)
	t.Cleanup(changeLog.Close)
	hourAgo := drivechangetest.Now().Add(-time.Hour)
	changeLog.Put(drivechangetest.File{ID: "shared_file", Name: "Shared", MimeType: "application/pdf", Parent: "folder_a", DriveID: "drive_1", CreatedTime: hourAgo, ModifiedTime: hourAgo})
	changeLog.Put(drivechangetest.File{ID: "my_file", Name: "Mine", MimeType: "application/pdf", Parent: "folder_a", CreatedTime: hourAgo, ModifiedTime: hourAgo})
	trashed := &recordingFileTarget{}
	runner, err := drive.NewFileTriggerRunner(drive.FileTriggerRunnerConfig{
		Connection: newTriggerTestConnection(t, changeLog), Cursor: pollingtrigger.NewMemoryCursorStore(),
		FileTrashedRoutes: []drive.FileTriggerRoute{{BindingName: "trash", Target: trashed}},
	})
	require.NoError(t, err)
	startFileTriggerRunner(t, runner)
	require.Eventually(t, func() bool { return changeLog.ListPages() >= 1 }, 10*time.Second, 20*time.Millisecond)

	changeLog.TrashFile("shared_file")
	changeLog.TrashFile("my_file")
	require.Eventually(t, func() bool { return len(uniqueIDs(trashed.received())) == 2 }, 10*time.Second, 20*time.Millisecond)
	// A later change to a shared drive file in the trash keeps its trashedTime, so it repeats the same
	// event ID, which a Dex Flow target deduplicates. Drive reports no trashedTime in My Drive, so a
	// later change there carries a new change time and is a new event.
	changeLog.TouchFile("shared_file")
	changeLog.TouchFile("my_file")
	require.Eventually(t, func() bool { return len(uniqueIDs(trashed.received())) == 3 }, 10*time.Second, 20*time.Millisecond)
	pages := changeLog.ListPages()
	require.Eventually(t, func() bool { return changeLog.ListPages() >= pages+2 }, 10*time.Second, 20*time.Millisecond)
	ids := uniqueIDs(trashed.received())
	require.Len(t, ids, 3)
	var shared, mine int
	for _, id := range ids {
		switch {
		case regexp.MustCompile(`^shared_file@trashed@\d+$`).MatchString(id):
			shared++
		case regexp.MustCompile(`^my_file@trashed@\d+$`).MatchString(id):
			mine++
		}
	}
	require.Equal(t, []int{1, 2}, []int{shared, mine})
}

func uniqueIDs(ids []string) []string {
	unique := slices.Clone(ids)
	slices.Sort(unique)
	return slices.Compact(unique)
}

func TestFileTriggerRunnerReseedsAfterDriveRejectsTheStoredPageToken(t *testing.T) {
	changeLog := drivechangetest.New(driveTestToken)
	t.Cleanup(changeLog.Close)
	created := &recordingFileTarget{}
	runner, err := drive.NewFileTriggerRunner(drive.FileTriggerRunnerConfig{
		Connection: newTriggerTestConnection(t, changeLog), Cursor: pollingtrigger.NewMemoryCursorStore(),
		FileCreatedRoutes: []drive.FileTriggerRoute{{BindingName: "every-new-file", Target: created}},
	})
	require.NoError(t, err)
	changeLog.Script(drivechangetest.ScriptedResponse{Status: http.StatusBadRequest})
	startFileTriggerRunner(t, runner)
	require.Eventually(t, func() bool { return changeLog.Requests("/drive/v3/changes/startPageToken") == 2 }, 10*time.Second, 20*time.Millisecond)
	changeLog.CreateFile("after_reset", "After.pdf", "application/pdf", "folder_a")
	require.Eventually(t, func() bool { return slices.Equal(created.received(), []string{"after_reset@created"}) }, 10*time.Second, 20*time.Millisecond)
}

func TestGeneratedFileCreatedTriggerPollsWithItsOwnCursor(t *testing.T) {
	changeLog := drivechangetest.New(driveTestToken)
	t.Cleanup(changeLog.Close)
	connection := newTriggerTestConnection(t, changeLog)
	created := &recordingFileTarget{}
	runner := drive.NewFileCreatedTrigger(drive.FileCreatedTriggerConfig{
		Connection: connection, ConnectionName: driveConnection.Name, BindingName: "new-docs",
		Configuration: drive.FileTriggerConfiguration{MimeType: "application/vnd.google-apps.document"}, Target: created,
	})
	startFileTriggerRunner(t, runner)
	require.Eventually(t, func() bool { return changeLog.ListPages() >= 1 }, 10*time.Second, 20*time.Millisecond)
	changeLog.CreateFile("sheet", "Budget", "application/vnd.google-apps.spreadsheet", "folder_a")
	changeLog.CreateFile("doc", "Plan", "application/vnd.google-apps.document", "folder_a")
	require.Eventually(t, func() bool { return slices.Equal(created.received(), []string{"doc@created"}) }, 10*time.Second, 20*time.Millisecond)
	require.Panics(t, func() {
		drive.NewFileTrashedTrigger(drive.FileTrashedTriggerConfig{
			Connection: connection, ConnectionName: driveConnection.Name, BindingName: "bad",
			Configuration: drive.FileTriggerConfiguration{MimeType: "PDF"}, Target: created,
		})
	})
}

func TestNewFileTriggerRunnerRejectsIncompleteConfiguration(t *testing.T) {
	changeLog := drivechangetest.New(driveTestToken)
	t.Cleanup(changeLog.Close)
	connection := newTriggerTestConnection(t, changeLog)
	cursor := pollingtrigger.NewMemoryCursorStore()
	target := &recordingFileTarget{}
	route := drive.FileTriggerRoute{BindingName: "new", Target: target}
	for name, config := range map[string]drive.FileTriggerRunnerConfig{
		"no connection":     {Cursor: cursor, FileCreatedRoutes: []drive.FileTriggerRoute{route}},
		"no cursor":         {Connection: connection, FileCreatedRoutes: []drive.FileTriggerRoute{route}},
		"no routes":         {Connection: connection, Cursor: cursor},
		"no target":         {Connection: connection, Cursor: cursor, FileCreatedRoutes: []drive.FileTriggerRoute{{BindingName: "new"}}},
		"invalid folder":    {Connection: connection, Cursor: cursor, FileCreatedRoutes: []drive.FileTriggerRoute{{BindingName: "new", Target: target, Configuration: drive.FileTriggerConfiguration{FolderID: "a b"}}}},
		"duplicate binding": {Connection: connection, Cursor: cursor, FileCreatedRoutes: []drive.FileTriggerRoute{route, route}},
	} {
		_, err := drive.NewFileTriggerRunner(config)
		require.Error(t, err, name)
	}
	_, err := drive.NewFileTriggerRunner(drive.FileTriggerRunnerConfig{
		Connection: connection, Cursor: cursor, FileCreatedRoutes: []drive.FileTriggerRoute{route}, FileTrashedRoutes: []drive.FileTriggerRoute{route},
	})
	require.NoError(t, err, "one binding name may serve different Triggers")
}

func millis(value time.Time) string { return strconv.FormatInt(value.UnixMilli(), 10) }
