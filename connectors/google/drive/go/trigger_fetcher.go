// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// changeFileFields selects the change and file fields FileEvent exposes.
const changeFileFields = "nextPageToken,newStartPageToken,changes(changeType,removed,fileId,time," +
	"file(id,name,mimeType,parents,driveId,createdTime,modifiedTime,trashed,trashedTime,size,md5Checksum,webViewLink))"

// fileChangeCursor is the stored feed position. Checkpoint is the latest change time of the previous
// complete pass; RoundLatest accumulates the current pass while Drive reports more pages.
type fileChangeCursor struct {
	PageToken   string    `json:"pageToken"`
	Checkpoint  time.Time `json:"checkpoint"`
	RoundLatest time.Time `json:"roundLatest,omitzero"`
}

type driveChangeList struct {
	NextPageToken     string                `json:"nextPageToken"`
	NewStartPageToken string                `json:"newStartPageToken"`
	Changes           []driveChangeResource `json:"changes"`
}

type driveChangeResource struct {
	ChangeType string           `json:"changeType"`
	Removed    bool             `json:"removed"`
	FileID     string           `json:"fileId"`
	Time       time.Time        `json:"time"`
	File       *driveChangeFile `json:"file"`
}

type driveChangeFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType"`
	Parents      []string  `json:"parents"`
	DriveID      string    `json:"driveId"`
	CreatedTime  time.Time `json:"createdTime"`
	ModifiedTime time.Time `json:"modifiedTime"`
	Trashed      bool      `json:"trashed"`
	TrashedTime  time.Time `json:"trashedTime"`
	Size         string    `json:"size"`
	MD5Checksum  string    `json:"md5Checksum"`
	WebViewLink  string    `json:"webViewLink"`
}

// fileChangeFetcher reads the connection's Drive change log for every file Trigger binding.
type fileChangeFetcher struct {
	client *Client
	call   sdkgo.Call
}

func newFileChangeFetcher(client *Client, connection sdkgo.ConnectionRef, triggerName string) *fileChangeFetcher {
	trigger := sdkgo.TriggerRef{ConnectorID: ConnectorID, TriggerName: triggerName}
	return &fileChangeFetcher{client: client, call: pollingtrigger.NewTriggerCredentialCall(connection, trigger)}
}

// SeedCursor stores Drive's current start page token, so existing files are never reported.
func (fetcher *fileChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	query := url.Values{"supportsAllDrives": {"true"}}
	var token struct {
		StartPageToken string `json:"startPageToken"`
	}
	if err := fetcher.getJSON(ctx, "/changes/startPageToken?"+query.Encode(), false, &token); err != nil {
		return pollingtrigger.Cursor{}, err
	}
	if token.StartPageToken == "" {
		return pollingtrigger.Cursor{}, errors.New("Google Drive returned no start page token")
	}
	return encodeFileChangeCursor(fileChangeCursor{PageToken: token.StartPageToken, Checkpoint: fetcher.client.now().UTC()})
}

// FetchChanges reads one changes.list page after cursor and classifies every usable file change.
func (fetcher *fileChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[fileChange], error) {
	var page pollingtrigger.ChangePage[fileChange]
	position, err := decodeFileChangeCursor(cursor.Token)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	query := url.Values{
		"pageToken": {position.PageToken}, "pageSize": {strconv.Itoa(fetcher.client.pollPageSize)},
		"fields": {changeFileFields}, "spaces": {"drive"}, "includeRemoved": {"false"},
		"includeItemsFromAllDrives": {"true"}, "supportsAllDrives": {"true"},
	}
	var listed driveChangeList
	if err := fetcher.getJSON(ctx, "/changes?"+query.Encode(), true, &listed); err != nil {
		return page, err
	}
	if len(listed.Changes) > pollingtrigger.MaxChangesPerPage {
		return page, fmt.Errorf("Google Drive returned %d changes, more than the requested page size", len(listed.Changes))
	}
	latest := position.RoundLatest
	for _, resource := range listed.Changes {
		if resource.Time.After(latest) {
			latest = resource.Time
		}
		if file, ok := fileEventFromChange(resource); ok {
			page.Changes = append(page.Changes, classifyFileChange(file, position.Checkpoint))
		}
	}
	next := fileChangeCursor{PageToken: listed.NextPageToken, Checkpoint: position.Checkpoint, RoundLatest: latest}
	switch {
	case listed.NextPageToken != "":
		page.HasMorePages = true
	case listed.NewStartPageToken != "":
		next = fileChangeCursor{PageToken: listed.NewStartPageToken, Checkpoint: position.Checkpoint}
		if latest.After(next.Checkpoint) {
			next.Checkpoint = latest
		}
	default:
		return page, errors.New("Google Drive returned neither a next page token nor a new start page token")
	}
	page.Next, err = encodeFileChangeCursor(next)
	return page, err
}

// fileEventFromChange skips drive changes, removals, and entries without valid file metadata.
func fileEventFromChange(resource driveChangeResource) (FileEvent, bool) {
	file := resource.File
	if resource.ChangeType != "file" || resource.Removed || file == nil || file.ID != resource.FileID ||
		!isDriveID(file.ID) || file.Name == "" || file.MimeType == "" || file.CreatedTime.IsZero() || file.ModifiedTime.IsZero() {
		return FileEvent{}, false
	}
	sizeBytes, err := parseOptionalInt64(file.Size)
	if err != nil {
		return FileEvent{}, false
	}
	return FileEvent{
		FileID: file.ID, Name: file.Name, MimeType: file.MimeType, Parents: file.Parents, DriveID: file.DriveID,
		CreatedTime: file.CreatedTime, ModifiedTime: file.ModifiedTime, TrashedTime: file.TrashedTime,
		ChangeTime: resource.Time, IsTrashed: file.Trashed, SizeBytes: sizeBytes, MD5Checksum: file.MD5Checksum,
		WebViewLink: file.WebViewLink,
	}, true
}

// getJSON sends one authenticated read and maps failures to the poll kit's signals. Only a rejected
// stored page token is a cursor fault; a rejected seed request is retried with backoff.
func (fetcher *fileChangeFetcher) getJSON(ctx context.Context, pathAndQuery string, isCursorRequest bool, decoded any) error {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return err
	}
	credential, err := sdkgo.ResolveCredential(ctx, fetcher.client.credentials, fetcher.call, fetcher.client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil {
		return errors.New("Google Drive Trigger credentials are unavailable")
	}
	response, err := fetcher.client.sendRequestWithContext(ctx, fetcher.call, &credential, driveRequest{
		method: http.MethodGet, target: fetcher.client.apiBaseURL + pathAndQuery, responseLimit: fetcher.client.maxResponseBytes,
	})
	var requestErr *driveRequestError
	switch {
	case errors.As(err, &requestErr) && requestErr.kind == sdkgo.FailureResponseTooLarge:
		return errors.New("Google Drive changes page exceeds maxResponseBytes; lower pollPageSize")
	case err != nil:
		return fmt.Errorf("Google Drive change request failed: %w", err)
	case response.status >= 200 && response.status < 300:
		if err := json.Unmarshal(response.body, decoded); err != nil {
			return errors.New("Google Drive returned an invalid change response")
		}
		return nil
	}
	reasons := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	failure := fmt.Errorf("Google Drive rejected the change request with HTTP %d", response.status)
	switch {
	case isRateLimitStatus(response.status, reasons):
		return pollingtrigger.MarkRateLimited(failure, providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), fetcher.client.now()))
	case isCursorRequest && isRejectedPageToken(response.status, response.body):
		return pollingtrigger.MarkCursorInvalid(failure)
	}
	return failure
}

// isRejectedPageToken reports a changes.list rejection of the stored page token: a 404 or 410, or a
// 400 unless Google names another parameter as the error location. A 400 for another parameter, such
// as fields, would recur after every reseed and silently drop every change, so it is retried instead.
func isRejectedPageToken(status int, body []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return true
	case http.StatusBadRequest:
		location := providerhttp.ReadErrorTokens(body, []string{"/error/errors/0/location"})
		return len(location) == 0 || location[0] == "pageToken"
	}
	return false
}

func encodeFileChangeCursor(position fileChangeCursor) (pollingtrigger.Cursor, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(position); err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return pollingtrigger.Cursor{Token: string(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))}, nil
}

func decodeFileChangeCursor(token string) (fileChangeCursor, error) {
	var position fileChangeCursor
	decoder := json.NewDecoder(bytes.NewReader([]byte(token)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&position); err != nil || position.PageToken == "" || position.Checkpoint.IsZero() {
		return fileChangeCursor{}, errors.New("stored Google Drive change cursor is invalid")
	}
	return position, nil
}
