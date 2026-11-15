// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package drivechangetest is an in-process fake of the Google Drive changes and files APIs for the
// connector's Trigger tests. Like Drive, it reports only each file's latest change after a page token.
package drivechangetest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// File is the fake's state of one Drive file.
type File struct {
	ID       string
	Name     string
	MimeType string
	Parent   string
	// DriveID is the shared drive ID, or blank for a My Drive file.
	DriveID      string
	CreatedTime  time.Time
	ModifiedTime time.Time
	IsTrashed    bool
	TrashedTime  time.Time
}

// ScriptedResponse is one failure the fake returns to the next changes.list request.
type ScriptedResponse struct {
	Status     int
	RetryAfter string
}

// ChangeLog is the fake Drive. It is safe for concurrent use.
type ChangeLog struct {
	*httptest.Server
	accessToken string
	mutex       sync.Mutex
	files       map[string]*File
	// changes holds file IDs in change order; position i has sequence i+1.
	changes   []changeRecord
	scripted  []ScriptedResponse
	requests  map[string]int
	listPages int
}

type changeRecord struct {
	fileID string
	time   time.Time
}

// New starts a fake that accepts only accessToken.
func New(accessToken string) *ChangeLog {
	changeLog := &ChangeLog{accessToken: accessToken, files: map[string]*File{}, requests: map[string]int{}}
	changeLog.Server = httptest.NewServer(http.HandlerFunc(changeLog.serve))
	return changeLog
}

// Now returns the current time at Drive's millisecond precision.
func Now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// CreateFile adds a file and records its change.
func (changeLog *ChangeLog) CreateFile(id, name, mimeType, parent string) {
	now := Now()
	changeLog.Put(File{ID: id, Name: name, MimeType: mimeType, Parent: parent, CreatedTime: now, ModifiedTime: now})
}

// Put stores file as is, such as one with an older creation time, and records its change.
func (changeLog *ChangeLog) Put(file File) {
	changeLog.mutex.Lock()
	defer changeLog.mutex.Unlock()
	stored := file
	changeLog.files[file.ID] = &stored
	changeLog.changes = append(changeLog.changes, changeRecord{fileID: file.ID, time: Now()})
}

// ModifyFile sets a file's modified time to modifiedAt and records its change.
func (changeLog *ChangeLog) ModifyFile(id string, modifiedAt time.Time) {
	changeLog.edit(id, func(file *File) { file.ModifiedTime = modifiedAt })
}

// TrashFile moves a file to the trash and records its change. Like Drive, it sets trashedTime only
// for a shared drive file.
func (changeLog *ChangeLog) TrashFile(id string) {
	changeLog.edit(id, func(file *File) {
		file.IsTrashed = true
		if file.DriveID != "" {
			file.TrashedTime = Now()
		}
	})
}

// TouchFile records a change that leaves the file's times unchanged, such as a permission change.
func (changeLog *ChangeLog) TouchFile(id string) { changeLog.edit(id, func(*File) {}) }

func (changeLog *ChangeLog) edit(id string, change func(*File)) {
	changeLog.mutex.Lock()
	defer changeLog.mutex.Unlock()
	change(changeLog.files[id])
	changeLog.changes = append(changeLog.changes, changeRecord{fileID: id, time: Now()})
}

// Script queues failures for the next changes.list requests.
func (changeLog *ChangeLog) Script(responses ...ScriptedResponse) {
	changeLog.mutex.Lock()
	defer changeLog.mutex.Unlock()
	changeLog.scripted = append(changeLog.scripted, responses...)
}

// Requests counts the requests made to a path, such as /drive/v3/changes.
func (changeLog *ChangeLog) Requests(path string) int {
	changeLog.mutex.Lock()
	defer changeLog.mutex.Unlock()
	return changeLog.requests[path]
}

// ListPages counts the successful changes.list pages served.
func (changeLog *ChangeLog) ListPages() int {
	changeLog.mutex.Lock()
	defer changeLog.mutex.Unlock()
	return changeLog.listPages
}

func (changeLog *ChangeLog) serve(writer http.ResponseWriter, request *http.Request) {
	changeLog.mutex.Lock()
	defer changeLog.mutex.Unlock()
	changeLog.requests[request.URL.Path]++
	if request.Header.Get("Authorization") != "Bearer "+changeLog.accessToken {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case request.URL.Path == "/drive/v3/changes/startPageToken":
		writeJSON(writer, map[string]any{"startPageToken": strconv.Itoa(len(changeLog.changes) + 1)})
	case request.URL.Path == "/drive/v3/changes":
		if len(changeLog.scripted) > 0 {
			scripted := changeLog.scripted[0]
			changeLog.scripted = changeLog.scripted[1:]
			if scripted.RetryAfter != "" {
				writer.Header().Set("Retry-After", scripted.RetryAfter)
			}
			writer.WriteHeader(scripted.Status)
			return
		}
		changeLog.serveChanges(writer, request)
	case strings.HasPrefix(request.URL.Path, "/drive/v3/files/"):
		file, ok := changeLog.files[strings.TrimPrefix(request.URL.Path, "/drive/v3/files/")]
		if !ok {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(writer, fileResource(*file))
	default:
		writer.WriteHeader(http.StatusNotFound)
	}
}

// serveChanges lists each file's latest change at or after pageToken, oldest first.
func (changeLog *ChangeLog) serveChanges(writer http.ResponseWriter, request *http.Request) {
	from, err := strconv.Atoi(request.URL.Query().Get("pageToken"))
	pageSize, sizeErr := strconv.Atoi(request.URL.Query().Get("pageSize"))
	if err != nil || from < 1 || from > len(changeLog.changes)+1 || sizeErr != nil || pageSize < 1 {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	latest := map[string]int{}
	for index := from - 1; index < len(changeLog.changes); index++ {
		latest[changeLog.changes[index].fileID] = index
	}
	positions := make([]int, 0, len(latest))
	for _, index := range latest {
		positions = append(positions, index)
	}
	slices.Sort(positions)
	response := map[string]any{}
	if len(positions) > pageSize {
		positions = positions[:pageSize]
		response["nextPageToken"] = strconv.Itoa(positions[len(positions)-1] + 2)
	} else {
		response["newStartPageToken"] = strconv.Itoa(len(changeLog.changes) + 1)
	}
	changes := make([]map[string]any, 0, len(positions))
	for _, index := range positions {
		record := changeLog.changes[index]
		changes = append(changes, map[string]any{
			"changeType": "file", "fileId": record.fileID, "time": record.time.Format(time.RFC3339Nano),
			"file": fileResource(*changeLog.files[record.fileID]),
		})
	}
	response["changes"] = changes
	changeLog.listPages++
	writeJSON(writer, response)
}

func fileResource(file File) map[string]any {
	resource := map[string]any{
		"id": file.ID, "name": file.Name, "mimeType": file.MimeType, "parents": []string{file.Parent},
		"createdTime": file.CreatedTime.Format(time.RFC3339Nano), "modifiedTime": file.ModifiedTime.Format(time.RFC3339Nano),
		"trashed": file.IsTrashed, "size": "42",
	}
	if file.DriveID != "" {
		resource["driveId"] = file.DriveID
	}
	if !file.TrashedTime.IsZero() && file.DriveID != "" {
		resource["trashedTime"] = file.TrashedTime.Format(time.RFC3339Nano)
	}
	return resource
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	// A write failure only means the test client went away.
	_ = json.NewEncoder(writer).Encode(value)
}
