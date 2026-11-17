// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateBoxFolderInput creates one named folder under a parent.
type CreateBoxFolderInput struct {
	// ParentID is a decimal Box folder ID; 0 is root.
	ParentID string `json:"parentId"`
	// Name is the new folder name, unique within the parent.
	Name string `json:"name"`
}

// CopyBoxFileInput copies one file under an explicit destination name.
type CopyBoxFileInput struct {
	// FileID is the source file's decimal ID.
	FileID string `json:"fileId"`
	// ParentID is the destination folder ID; 0 is root.
	ParentID string `json:"parentId"`
	// Name is the destination file name, unique within the parent.
	Name string `json:"name"`
}

// CopyBoxFolderInput copies one folder under an explicit destination name.
type CopyBoxFolderInput struct {
	// FolderID is the source folder's decimal ID; root cannot be copied.
	FolderID string `json:"folderId"`
	// ParentID is the destination folder ID; 0 is root.
	ParentID string `json:"parentId"`
	// Name is the destination folder name, unique within the parent.
	Name string `json:"name"`
}

// CreatedBoxItem identifies a created or copied item.
type CreatedBoxItem struct {
	// ID is the stable Box item ID.
	ID string `json:"id"`
	// Type is file or folder.
	Type string `json:"type"`
	// Name is the created item's name.
	Name string `json:"name"`
	// ParentID is the destination folder ID.
	ParentID string `json:"parentId"`
}

// CreateFolderOperation creates a folder with bounded resend.
type CreateFolderOperation struct{ client *Client }

// CopyFileOperation copies a file with bounded resend.
type CopyFileOperation struct{ client *Client }

// CopyFolderOperation copies a folder with bounded resend.
type CopyFolderOperation struct{ client *Client }

// CreateFolder binds folder creation to this client.
func (client *Client) CreateFolder() CreateFolderOperation { return CreateFolderOperation{client} }

// CopyFile binds file copying to this client.
func (client *Client) CopyFile() CopyFileOperation { return CopyFileOperation{client} }

// CopyFolder binds folder copying to this client.
func (client *Client) CopyFolder() CopyFolderOperation { return CopyFolderOperation{client} }

// Definition returns the generated createFolder contract.
func (CreateFolderOperation) Definition() sdkgo.MutationDefinition { return CreateFolderDefinition }

// Definition returns the generated copyFile contract.
func (CopyFileOperation) Definition() sdkgo.MutationDefinition { return CopyFileDefinition }

// Definition returns the generated copyFolder contract.
func (CopyFolderOperation) Definition() sdkgo.MutationDefinition { return CopyFolderDefinition }

// IdempotencyKey correlates attempts; Box does not document POST deduplication.
func (CreateFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateBoxFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts; Box does not document POST deduplication.
func (CopyFileOperation) IdempotencyKey(callID sdkgo.CallID, _ CopyBoxFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts; Box does not document POST deduplication.
func (CopyFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ CopyBoxFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a named folder under ParentID.
func (operation CreateFolderOperation) Invoke(call sdkgo.Call, input CreateBoxFolderInput) sdkgo.MutationAttempt[CreatedBoxItem] {
	if !validBoxItemID(input.ParentID, true) || !validBoxNewName(input.Name) {
		return boxCreateFailure("createFolder", CreateFolderBranchDefect, sdkgo.FailureValidation, "parent ID or folder name is invalid")
	}
	request := boxLimitedCreate{client: operation.client, call: call, operation: "createFolder", path: "/2.0/folders", parentID: input.ParentID, name: input.Name, itemType: "folder", timeout: CreateFolderDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateFolderBranchCreated, rejected: CreateFolderBranchProviderRejected, defect: CreateFolderBranchDefect}
	return request.invoke()
}

// Invoke copies a named file under ParentID.
func (operation CopyFileOperation) Invoke(call sdkgo.Call, input CopyBoxFileInput) sdkgo.MutationAttempt[CreatedBoxItem] {
	if !validBoxItemID(input.FileID, false) || !validBoxItemID(input.ParentID, true) || !validBoxNewName(input.Name) {
		return boxCreateFailure("copyFile", CopyFileBranchDefect, sdkgo.FailureValidation, "source ID, parent ID, or file name is invalid")
	}
	request := boxLimitedCreate{client: operation.client, call: call, operation: "copyFile", path: "/2.0/files/" + url.PathEscape(input.FileID) + "/copy", parentID: input.ParentID, name: input.Name, itemType: "file", timeout: CopyFileDefinition.StepDefaults.ExecuteMethodTimeout, created: CopyFileBranchCopied, rejected: CopyFileBranchProviderRejected, defect: CopyFileBranchDefect}
	return request.invoke()
}

// Invoke copies a named folder under ParentID.
func (operation CopyFolderOperation) Invoke(call sdkgo.Call, input CopyBoxFolderInput) sdkgo.MutationAttempt[CreatedBoxItem] {
	if !validBoxItemID(input.FolderID, false) || !validBoxItemID(input.ParentID, true) || !validBoxNewName(input.Name) {
		return boxCreateFailure("copyFolder", CopyFolderBranchDefect, sdkgo.FailureValidation, "source ID, parent ID, or folder name is invalid")
	}
	request := boxLimitedCreate{client: operation.client, call: call, operation: "copyFolder", path: "/2.0/folders/" + url.PathEscape(input.FolderID) + "/copy", parentID: input.ParentID, name: input.Name, itemType: "folder", timeout: CopyFolderDefinition.StepDefaults.ExecuteMethodTimeout, created: CopyFolderBranchCopied, rejected: CopyFolderBranchProviderRejected, defect: CopyFolderBranchDefect}
	return request.invoke()
}

func validBoxNewName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 255 && !strings.Contains(name, "/")
}

type boxReadBackState uint8

const (
	boxReadBackRetry boxReadBackState = iota
	boxReadBackAbsent
	boxReadBackMatched
	boxReadBackConflicted
)

type boxLimitedCreate struct {
	client    *Client
	call      sdkgo.Call
	operation string
	path      string
	parentID  string
	name      string
	itemType  string
	timeout   time.Duration
	created   sdkgo.BranchID
	rejected  sdkgo.BranchID
	defect    sdkgo.BranchID
}

func (request boxLimitedCreate) invoke() sdkgo.MutationAttempt[CreatedBoxItem] {
	fence, err := singledispatch.NewFence(request.call, request.timeout, singledispatch.WithMaxSends(request.call.MaxSends()))
	if err != nil {
		return boxCreateFailure(request.operation, request.defect, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return request.uncertain("create checkpoint cannot be trusted")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		value, state := request.readBack()
		switch state {
		case boxReadBackMatched:
			return sdkgo.NewMutationBranch(request.created, value, nil, sdkgo.Receipt{})
		case boxReadBackRetry:
			return request.retry(sdkgo.FailureAvailability, "destination read-back is unavailable", 0)
		case boxReadBackConflicted:
			return request.uncertain("destination name conflicts with a possible create")
		case boxReadBackAbsent:
			if decision.Kind == singledispatch.ReadBackRequired {
				return request.uncertain("send cap reached without confirming creation")
			}
		}
	}
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	canResendUnknown := decision.MaximumPriorSends+1 < request.call.MaxSends()
	accessToken, err := request.client.resolveAccessToken(request.call)
	if err != nil {
		if hasPossibleEarlierSend {
			return request.uncertain("connection unavailable after a possible create")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "no-send checkpoint unavailable", 0)
		}
		if errors.Is(err, errRefresh) {
			return request.retry(sdkgo.FailureAvailability, "credential refresh unavailable", 0)
		}
		return boxCreateFailure(request.operation, request.defect, sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		Name   string `json:"name"`
		Parent struct {
			ID string `json:"id"`
		} `json:"parent"`
	}{Name: request.name, Parent: struct {
		ID string `json:"id"`
	}{ID: request.parentID}})
	if err != nil {
		return boxCreateFailure(request.operation, request.defect, sdkgo.FailureValidation, "Box create cannot be encoded")
	}
	httpRequest, err := http.NewRequestWithContext(request.call.Context, http.MethodPost, request.client.baseURL+request.path, bytes.NewReader(body))
	if err != nil {
		return boxCreateFailure(request.operation, request.defect, sdkgo.FailureLocalDefect, "Box create request cannot be built")
	}
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if err := singledispatch.PrepareHTTPWrite(httpRequest); err != nil {
		return boxCreateFailure(request.operation, request.defect, sdkgo.FailureLocalDefect, "Box create request cannot be prepared")
	}
	dispatch := boxCreateDispatch{client: request.client, request: httpRequest, accessToken: accessToken}
	err = fence.Dispatch(dispatch.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return request.retry(sdkgo.FailureAvailability, "create dispatch did not start", 0)
	}
	response := dispatch.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "retry checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return request.retry(kind, "Box did not process create", response.retryAfter)
	}
	if err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		if canResendUnknown {
			return request.retry(sdkgo.FailureAvailability, "Box create outcome is unknown", response.retryAfter)
		}
		return request.uncertain("send cap reached with unknown Box create outcome")
	}
	if response.status == http.StatusConflict {
		if !hasPossibleEarlierSend {
			return boxCreateFailure(request.operation, request.rejected, sdkgo.FailureProviderRejection, "Box destination name already exists")
		}
		value, state := request.readBack()
		if state == boxReadBackMatched {
			return sdkgo.NewMutationBranch(request.created, value, nil, sdkgo.Receipt{})
		}
		if state == boxReadBackRetry {
			return request.retry(sdkgo.FailureAvailability, "destination read-back is unavailable", 0)
		}
		if hasPossibleEarlierSend {
			return request.uncertain("earlier Box create may have succeeded")
		}
		return boxCreateFailure(request.operation, request.rejected, sdkgo.FailureProviderRejection, "Box destination name already exists")
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		if hasPossibleEarlierSend {
			return request.uncertain("earlier Box create may have succeeded")
		}
		return boxCreateFailure(request.operation, request.rejected, sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status))
	}
	value, err := request.decodeResponse(response.body)
	if err != nil {
		confirmed, state := request.readBack()
		if state == boxReadBackMatched {
			return sdkgo.NewMutationBranch(request.created, confirmed, nil, sdkgo.Receipt{})
		}
		if state == boxReadBackRetry || (state == boxReadBackAbsent && canResendUnknown) {
			return request.retry(sdkgo.FailureAvailability, "accepted Box create could not be confirmed", 0)
		}
		return request.uncertain("Box accepted create without a confirmable ID")
	}
	return sdkgo.NewMutationBranch(request.created, value, nil, sdkgo.Receipt{})
}

func (request boxLimitedCreate) decodeResponse(body []byte) (CreatedBoxItem, error) {
	var wire struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Name   string `json:"name"`
		Parent struct {
			ID string `json:"id"`
		} `json:"parent"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CreatedBoxItem{}, err
	}
	if !validBoxItemID(wire.ID, false) || wire.Type != request.itemType || wire.Name != request.name || wire.Parent.ID != request.parentID {
		return CreatedBoxItem{}, fmt.Errorf("Box created item response is invalid")
	}
	return CreatedBoxItem{ID: wire.ID, Type: wire.Type, Name: wire.Name, ParentID: wire.Parent.ID}, nil
}

func (request boxLimitedCreate) readBack() (CreatedBoxItem, boxReadBackState) {
	for offset := 0; offset <= 10000; offset += 1000 {
		path := "/2.0/folders/" + url.PathEscape(request.parentID) + "/items?fields=id,type,name&limit=1000&offset=" + strconv.Itoa(offset)
		response := request.client.requestBoxItem(request.call, http.MethodGet, path)
		if response.err != nil || response.status != http.StatusOK {
			return CreatedBoxItem{}, boxReadBackRetry
		}
		page, err := decodeBoxItemPage(response.body)
		if err != nil {
			return CreatedBoxItem{}, boxReadBackRetry
		}
		for _, item := range page.Entries {
			if item.Name != request.name {
				continue
			}
			if item.Type != request.itemType {
				return CreatedBoxItem{}, boxReadBackConflicted
			}
			return CreatedBoxItem{ID: item.ID, Type: item.Type, Name: item.Name, ParentID: request.parentID}, boxReadBackMatched
		}
		if offset+len(page.Entries) >= page.TotalCount {
			return CreatedBoxItem{}, boxReadBackAbsent
		}
		if len(page.Entries) == 0 {
			return CreatedBoxItem{}, boxReadBackRetry
		}
	}
	return CreatedBoxItem{}, boxReadBackRetry
}

func (request boxLimitedCreate) retry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CreatedBoxItem] {
	return sdkgo.NewMutationRetry[CreatedBoxItem](queryFailure(request.operation, kind, message), retryAfter)
}
func (request boxLimitedCreate) uncertain(message string) sdkgo.MutationAttempt[CreatedBoxItem] {
	return sdkgo.NewMutationUncertain(CreatedBoxItem{}, queryFailure(request.operation, sdkgo.FailureTransport, message), sdkgo.Receipt{})
}

type boxCreateDispatch struct {
	client      *Client
	request     *http.Request
	accessToken string
	response    fileResponse
}

func (dispatch *boxCreateDispatch) send(ctx context.Context) error {
	dispatch.response = dispatch.client.sendMetadataRequest(dispatch.request.WithContext(ctx), dispatch.accessToken)
	return dispatch.response.err
}

func boxCreateFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedBoxItem] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, CreatedBoxItem{}, &failure, sdkgo.Receipt{})
}
