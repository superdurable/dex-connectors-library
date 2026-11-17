// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateBoxFileInput selects a file and absolute fields to replace.
type UpdateBoxFileInput struct {
	// FileID appears in Box Web's /files/<ID> URL.
	FileID string `json:"fileId"`
	// Name is the desired file name; blank leaves it unchanged.
	Name string `json:"name,omitempty"`
	// ParentID is the destination folder ID; blank leaves the parent unchanged and 0 selects root.
	ParentID string `json:"parentId,omitempty"`
	// SharedLinkAccess is open, company, or collaborators; blank leaves sharing unchanged.
	SharedLinkAccess string `json:"sharedLinkAccess,omitempty"`
}

// UpdateBoxFolderInput selects a folder and absolute fields to replace.
type UpdateBoxFolderInput struct {
	// FolderID appears in Box Web's /folders/<ID> URL; root cannot be updated.
	FolderID string `json:"folderId"`
	// Name is the desired folder name; blank leaves it unchanged.
	Name string `json:"name,omitempty"`
	// ParentID is the destination folder ID; blank leaves the parent unchanged and 0 selects root.
	ParentID string `json:"parentId,omitempty"`
	// SharedLinkAccess is open, company, or collaborators; blank leaves sharing unchanged.
	SharedLinkAccess string `json:"sharedLinkAccess,omitempty"`
}

// UpdatedBoxItem contains the selected Box metadata after an update.
type UpdatedBoxItem struct {
	// ID is the stable file or folder ID.
	ID string `json:"id"`
	// Type is file or folder.
	Type string `json:"type"`
	// Name is the current item name.
	Name string `json:"name"`
	// ParentID is the current parent folder ID, when returned by Box.
	ParentID string `json:"parentId,omitempty"`
	// SharedLinkURL is the shareable URL, when one exists.
	SharedLinkURL string `json:"sharedLinkUrl,omitempty"`
}

// UpdateFileOperation changes a file's name, parent, or shared link.
type UpdateFileOperation struct{ client *Client }

// UpdateFolderOperation changes a folder's name, parent, or shared link.
type UpdateFolderOperation struct{ client *Client }

// UpdateFile binds the file update to this client.
func (client *Client) UpdateFile() UpdateFileOperation { return UpdateFileOperation{client} }

// UpdateFolder binds the folder update to this client.
func (client *Client) UpdateFolder() UpdateFolderOperation { return UpdateFolderOperation{client} }

// Definition returns the generated updateFile contract.
func (UpdateFileOperation) Definition() sdkgo.MutationDefinition { return UpdateFileDefinition }

// Definition returns the generated updateFolder contract.
func (UpdateFolderOperation) Definition() sdkgo.MutationDefinition { return UpdateFolderDefinition }

// IdempotencyKey derives a stable call key for an absolute update.
func (UpdateFileOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateBoxFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key for an absolute update.
func (UpdateFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateBoxFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected file metadata and sharing.
func (operation UpdateFileOperation) Invoke(call sdkgo.Call, input UpdateBoxFileInput) sdkgo.MutationAttempt[UpdatedBoxItem] {
	if !validBoxItemID(input.FileID, false) || !validBoxUpdate(input.Name, input.ParentID, input.SharedLinkAccess) {
		return boxUpdateFailure("updateFile", UpdateFileBranchDefect, sdkgo.FailureValidation, "Box file ID or update fields are invalid")
	}
	return runBoxUpdate(operation.client, call, "updateFile", "file", input.FileID, input.Name, input.ParentID, input.SharedLinkAccess, UpdateFileBranchUpdated, UpdateFileBranchNotFound, UpdateFileBranchProviderRejected, UpdateFileBranchInvalidResponse, UpdateFileBranchDefect)
}

// Invoke replaces selected folder metadata and sharing.
func (operation UpdateFolderOperation) Invoke(call sdkgo.Call, input UpdateBoxFolderInput) sdkgo.MutationAttempt[UpdatedBoxItem] {
	if !validBoxItemID(input.FolderID, false) || !validBoxUpdate(input.Name, input.ParentID, input.SharedLinkAccess) {
		return boxUpdateFailure("updateFolder", UpdateFolderBranchDefect, sdkgo.FailureValidation, "Box folder ID or update fields are invalid")
	}
	return runBoxUpdate(operation.client, call, "updateFolder", "folder", input.FolderID, input.Name, input.ParentID, input.SharedLinkAccess, UpdateFolderBranchUpdated, UpdateFolderBranchNotFound, UpdateFolderBranchProviderRejected, UpdateFolderBranchInvalidResponse, UpdateFolderBranchDefect)
}

func validBoxUpdate(name, parentID, sharedLinkAccess string) bool {
	if name == "" && parentID == "" && sharedLinkAccess == "" {
		return false
	}
	if name != "" && strings.TrimSpace(name) == "" {
		return false
	}
	if parentID != "" && !validBoxItemID(parentID, true) {
		return false
	}
	switch sharedLinkAccess {
	case "", "open", "company", "collaborators":
		return true
	default:
		return false
	}
}

func runBoxUpdate(client *Client, call sdkgo.Call, operation, itemType, itemID, name, parentID, sharedLinkAccess string, success, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[UpdatedBoxItem] {
	body := map[string]any{}
	if name != "" {
		body["name"] = name
	}
	if parentID != "" {
		body["parent"] = map[string]string{"id": parentID}
	}
	if sharedLinkAccess != "" {
		body["shared_link"] = map[string]string{"access": sharedLinkAccess}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return boxUpdateFailure(operation, defect, sdkgo.FailureValidation, "Box update cannot be encoded")
	}
	accessToken, err := client.resolveAccessToken(call)
	if errors.Is(err, errCredential) {
		return boxUpdateFailure(operation, defect, sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[UpdatedBoxItem](queryFailure(operation, sdkgo.FailureAvailability, "Box credential refresh failed"), 0)
	}
	path := "/2.0/" + itemType + "s/" + url.PathEscape(itemID) + "?fields=id,type,name,parent,shared_link"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPut, client.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return boxUpdateFailure(operation, defect, sdkgo.FailureValidation, "Box update request cannot be built")
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response := client.sendMetadataRequest(request, accessToken)
	if errors.Is(response.err, errCredential) {
		return boxUpdateFailure(operation, defect, sdkgo.FailureAuthentication, "Box credentials are invalid")
	}
	if errors.Is(response.err, errRequest) || errors.Is(response.err, errRefresh) {
		return sdkgo.NewMutationRetry[UpdatedBoxItem](queryFailure(operation, sdkgo.FailureAvailability, "Box provider request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[UpdatedBoxItem](queryFailure(operation, kind, "Box provider is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return boxUpdateFailure(operation, notFound, sdkgo.FailureNotFound, "Box item was not found")
	}
	if response.status != http.StatusOK {
		return boxUpdateFailure(operation, rejected, sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return boxUpdateFailure(operation, invalid, sdkgo.FailureResponseTooLarge, "Box response exceeds the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[UpdatedBoxItem](queryFailure(operation, sdkgo.FailureTransport, "Box response was interrupted"), 0)
	}
	var wire struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Name   string `json:"name"`
		Parent struct {
			ID string `json:"id"`
		} `json:"parent"`
		SharedLink struct {
			URL    string `json:"url"`
			Access string `json:"access"`
		} `json:"shared_link"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.ID != itemID || wire.Type != itemType || wire.Name == "" || (name != "" && wire.Name != name) || (parentID != "" && wire.Parent.ID != parentID) || (sharedLinkAccess != "" && (wire.SharedLink.URL == "" || wire.SharedLink.Access != sharedLinkAccess)) {
		return boxUpdateFailure(operation, invalid, sdkgo.FailureProtocol, "Box update response is incomplete or malformed")
	}
	return sdkgo.NewMutationBranch(success, UpdatedBoxItem{ID: wire.ID, Type: wire.Type, Name: wire.Name, ParentID: wire.Parent.ID, SharedLinkURL: wire.SharedLink.URL}, nil, sdkgo.Receipt{})
}

func boxUpdateFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UpdatedBoxItem] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, UpdatedBoxItem{}, &failure, sdkgo.Receipt{})
}
