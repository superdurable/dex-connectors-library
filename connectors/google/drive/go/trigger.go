// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	fileCreatedTriggerName = "fileCreated"
	fileUpdatedTriggerName = "fileUpdated"
	fileTrashedTriggerName = "fileTrashed"
	// fileChangesFeedName is the stable storage identity of the shared changes.list cursor.
	fileChangesFeedName = "fileChanges"
	// classificationLookback re-examines changes this long before the checkpoint, because Drive does
	// not promise that change records arrive in timestamp order. Stable event IDs absorb the repeats.
	classificationLookback = 5 * time.Minute
	// modifiedAfterCreationThreshold separates an edit from the write that created the file.
	modifiedAfterCreationThreshold = time.Second
)

// FileTriggerConfiguration filters the files a fileCreated, fileUpdated, or fileTrashed binding
// receives. Every non-blank field must match.
type FileTriggerConfiguration struct {
	// FolderID limits events to files whose direct parent is this Drive folder or shared drive ID, as
	// the folderPicker unit saves it. Blank matches files in every folder the connection can see.
	// Subfolders are not searched, because Drive reports only a file's direct parent.
	FolderID string `json:"folderId,omitempty"`
	// FolderName is the display name the folderPicker unit saves beside FolderID. It is informational
	// and never used for matching.
	FolderName string `json:"folderName,omitempty"`
	// MimeType limits events to files of exactly this MIME type, such as application/pdf or
	// application/vnd.google-apps.document. Blank matches every type, including folders.
	MimeType string `json:"mimeType,omitempty"`
}

// FileEvent is the metadata of one Drive file that a file Trigger delivers. It never carries content.
type FileEvent struct {
	// FileID is the Drive file ID, usable with GetFile and ReadFileText.
	FileID string `json:"fileId"`
	// Name is the file name, which is not unique within a folder.
	Name string `json:"name"`
	// MimeType is the file's MIME type.
	MimeType string `json:"mimeType"`
	// Parents holds the file's direct parent folder ID; Drive allows at most one.
	Parents []string `json:"parents,omitempty"`
	// DriveID is the shared drive ID, or blank for a My Drive file.
	DriveID string `json:"driveId,omitempty"`
	// CreatedTime is when the file was created.
	CreatedTime time.Time `json:"createdTime"`
	// ModifiedTime is when anyone last modified the file.
	ModifiedTime time.Time `json:"modifiedTime"`
	// TrashedTime is when the file was trashed; Drive reports it only for shared drive files, so it is
	// zero for a My Drive file.
	TrashedTime time.Time `json:"trashedTime,omitzero"`
	// ChangeTime is the time of the Drive change record that reported the file.
	ChangeTime time.Time `json:"changeTime"`
	// IsTrashed reports that the file is in the trash, explicitly or through a trashed parent folder.
	IsTrashed bool `json:"isTrashed"`
	// SizeBytes is the size of blob and Google Workspace editor files; zero when Drive omits it.
	SizeBytes int64 `json:"sizeBytes,omitempty"`
	// MD5Checksum is the content checksum of blob files; blank for Google Workspace files.
	MD5Checksum string `json:"md5Checksum,omitempty"`
	// WebViewLink opens the file in a browser.
	WebViewLink string `json:"webViewLink,omitempty"`
}

// Validate checks that FolderID is a Drive ID and MimeType a plain lowercase type/subtype when set.
func (configuration FileTriggerConfiguration) Validate() error {
	if configuration.FolderID != "" && !isDriveID(configuration.FolderID) {
		return errors.New("Google Drive file Trigger folderId must be a Drive folder ID")
	}
	if configuration.MimeType != "" && !isPlainMediaType(configuration.MimeType) {
		return errors.New("Google Drive file Trigger mimeType must be one lowercase type/subtype without parameters")
	}
	return nil
}

func (configuration FileTriggerConfiguration) matches(file FileEvent) bool {
	if configuration.FolderID != "" && !slices.Contains(file.Parents, configuration.FolderID) {
		return false
	}
	return configuration.MimeType == "" || configuration.MimeType == file.MimeType
}

// fileChange is one usable changes.list entry with the Trigger events it can produce. The fetcher
// classifies it against the cursor checkpoint, so a refetch from the same cursor yields the same events.
type fileChange struct {
	file    FileEvent
	created *sdkgo.TriggerEvent[FileEvent]
	updated *sdkgo.TriggerEvent[FileEvent]
	trashed *sdkgo.TriggerEvent[FileEvent]
}

// classifyFileChange derives events from file state alone, because Drive changes carry no change kind.
// A trashed file produces only fileTrashed. Otherwise a file created after the lookback produces
// fileCreated, and one modified after the lookback and after its creation write produces fileUpdated.
func classifyFileChange(file FileEvent, checkpoint time.Time) fileChange {
	lookback := checkpoint.Add(-classificationLookback)
	change := fileChange{file: file}
	if file.IsTrashed {
		trashedAt := file.TrashedTime
		if trashedAt.IsZero() {
			trashedAt = file.ChangeTime
		}
		if trashedAt.After(lookback) {
			change.trashed = fileTriggerEvent(file, "trashed", trashedAt)
		}
		return change
	}
	if file.CreatedTime.After(lookback) {
		change.created = &sdkgo.TriggerEvent[FileEvent]{ID: file.FileID + "@created", OccurredAt: file.CreatedTime, Payload: file}
	}
	if file.ModifiedTime.After(lookback) && file.ModifiedTime.Sub(file.CreatedTime) >= modifiedAfterCreationThreshold {
		change.updated = fileTriggerEvent(file, "modified", file.ModifiedTime)
	}
	return change
}

// fileTriggerEvent identifies an event by file, kind, and provider time, so each edit or trash is
// one event however many times its change is refetched.
func fileTriggerEvent(file FileEvent, kind string, occurredAt time.Time) *sdkgo.TriggerEvent[FileEvent] {
	id := file.FileID + "@" + kind + "@" + strconv.FormatInt(occurredAt.UnixMilli(), 10)
	return &sdkgo.TriggerEvent[FileEvent]{ID: id, OccurredAt: occurredAt, Payload: file}
}

// fileChangeSelector returns the feed selector of one binding of triggerName.
func fileChangeSelector(triggerName string, configuration FileTriggerConfiguration) func(fileChange, *projectconfig.KeyedSnapshotEntry) []sdkgo.TriggerEvent[FileEvent] {
	return func(change fileChange, _ *projectconfig.KeyedSnapshotEntry) []sdkgo.TriggerEvent[FileEvent] {
		event := change.eventFor(triggerName)
		if event == nil || !configuration.matches(change.file) {
			return nil
		}
		return []sdkgo.TriggerEvent[FileEvent]{*event}
	}
}

func (change fileChange) eventFor(triggerName string) *sdkgo.TriggerEvent[FileEvent] {
	switch triggerName {
	case fileCreatedTriggerName:
		return change.created
	case fileUpdatedTriggerName:
		return change.updated
	case fileTrashedTriggerName:
		return change.trashed
	}
	return nil
}
