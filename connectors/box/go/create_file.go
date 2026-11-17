// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateBoxFileInput uploads one new bounded file under a parent folder.
type CreateBoxFileInput struct {
	// ParentID is a decimal Box folder ID; 0 is root.
	ParentID string `json:"parentId"`
	// Name is the new file name, unique within the parent.
	Name string `json:"name"`
	// ContentBase64 holds the complete file bytes encoded as Base64.
	ContentBase64 string `json:"contentBase64"`
}

// CreatedBoxFile identifies a newly uploaded file without retaining its bytes.
type CreatedBoxFile struct {
	// FileID is the new Box file ID.
	FileID string `json:"fileId"`
	// Name is the file name.
	Name string `json:"name"`
	// SHA1 is Box's hexadecimal digest of the content.
	SHA1 string `json:"sha1"`
	// ParentID is the destination folder ID.
	ParentID string `json:"parentId"`
}

// CreateFileOperation uploads a new file with bounded resend.
type CreateFileOperation struct{ client *Client }

// CreateFile binds new file upload to this client.
func (client *Client) CreateFile() CreateFileOperation { return CreateFileOperation{client} }

// Definition returns the generated createFile contract.
func (CreateFileOperation) Definition() sdkgo.MutationDefinition { return CreateFileDefinition }

// IdempotencyKey correlates attempts; Box does not deduplicate this upload.
func (CreateFileOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateBoxFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke uploads one new file and checks its destination after an unknown outcome.
func (operation CreateFileOperation) Invoke(call sdkgo.Call, input CreateBoxFileInput) sdkgo.MutationAttempt[CreatedBoxFile] {
	if !validBoxItemID(input.ParentID, true) || !validBoxNewName(input.Name) {
		return createFileFailure(CreateFileBranchDefect, sdkgo.FailureValidation, "parent ID or file name is invalid")
	}
	if int64(base64.StdEncoding.DecodedLen(len(input.ContentBase64))) > operation.client.maxUploadBytes {
		return createFileFailure(CreateFileBranchDefect, sdkgo.FailureValidation, "Box upload exceeds the configured byte limit")
	}
	content, err := base64.StdEncoding.Strict().DecodeString(input.ContentBase64)
	if err != nil || len(content) == 0 || int64(len(content)) > operation.client.maxUploadBytes {
		return createFileFailure(CreateFileBranchDefect, sdkgo.FailureValidation, "Box upload content must be bounded nonempty Base64")
	}
	digestBytes := sha1.Sum(content)
	request := boxLimitedUpload{client: operation.client, call: call, parentID: input.ParentID, name: input.Name, content: content, digest: strings.ToUpper(hex.EncodeToString(digestBytes[:]))}
	return request.invoke()
}

type boxLimitedUpload struct {
	client   *Client
	call     sdkgo.Call
	parentID string
	name     string
	content  []byte
	digest   string
}

func (request boxLimitedUpload) invoke() sdkgo.MutationAttempt[CreatedBoxFile] {
	fence, err := singledispatch.NewFence(request.call, CreateFileDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(request.call.MaxSends()))
	if err != nil {
		return createFileFailure(CreateFileBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return request.uncertain("upload checkpoint cannot be trusted")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		_, state := request.readBack()
		switch state {
		case boxReadBackMatched:
			return request.uncertain("file name and digest do not identify this upload")
		case boxReadBackRetry:
			return request.retry(sdkgo.FailureAvailability, "file read-back is unavailable", 0)
		case boxReadBackConflicted:
			return request.uncertain("destination name conflicts with a possible upload")
		case boxReadBackAbsent:
			if decision.Kind == singledispatch.ReadBackRequired {
				return request.uncertain("send cap reached without confirming upload")
			}
		}
	}
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	canResendUnknown := decision.MaximumPriorSends+1 < request.call.MaxSends()
	accessToken, err := request.client.resolveAccessToken(request.call)
	if err != nil {
		if hasPossibleEarlierSend {
			return request.uncertain("connection unavailable after a possible upload")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "no-send checkpoint unavailable", 0)
		}
		if errors.Is(err, errRefresh) {
			return request.retry(sdkgo.FailureAvailability, "credential refresh unavailable", 0)
		}
		return createFileFailure(CreateFileBranchDefect, sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid")
	}
	httpRequest, err := request.newMultipartRequest(accessToken)
	if err != nil {
		if hasPossibleEarlierSend {
			return request.uncertain("upload request unavailable after a possible send")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "no-send checkpoint unavailable", 0)
		}
		return createFileFailure(CreateFileBranchDefect, sdkgo.FailureLocalDefect, "upload request cannot be prepared")
	}
	if err := singledispatch.PrepareHTTPWrite(httpRequest); err != nil {
		return createFileFailure(CreateFileBranchDefect, sdkgo.FailureLocalDefect, "upload request cannot be prepared")
	}
	dispatch := boxCreateDispatch{client: request.client, request: httpRequest, accessToken: accessToken}
	err = fence.Dispatch(dispatch.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return request.retry(sdkgo.FailureAvailability, "upload dispatch did not start", 0)
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
		return request.retry(kind, "Box did not process upload", response.retryAfter)
	}
	if err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		if canResendUnknown {
			return request.retry(sdkgo.FailureAvailability, "Box upload outcome is unknown", response.retryAfter)
		}
		return request.uncertain("send cap reached with unknown Box upload outcome")
	}
	if response.status == http.StatusConflict {
		if !hasPossibleEarlierSend {
			return createFileFailure(CreateFileBranchProviderRejected, sdkgo.FailureProviderRejection, "Box destination name already exists")
		}
		_, state := request.readBack()
		if state == boxReadBackMatched {
			return request.uncertain("file name and digest do not identify this upload")
		}
		if state == boxReadBackRetry {
			return request.retry(sdkgo.FailureAvailability, "file read-back is unavailable", 0)
		}
		return request.uncertain("earlier upload may have succeeded")
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		if hasPossibleEarlierSend {
			return request.uncertain("earlier upload may have succeeded")
		}
		return createFileFailure(CreateFileBranchProviderRejected, sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status))
	}
	value, err := request.decodeUploadResponse(response.body)
	if err != nil {
		_, state := request.readBack()
		if state == boxReadBackMatched {
			return request.uncertain("file name and digest do not identify this upload")
		}
		if state == boxReadBackRetry || (state == boxReadBackAbsent && canResendUnknown) {
			return request.retry(sdkgo.FailureAvailability, "accepted upload could not be confirmed", 0)
		}
		return request.uncertain("Box accepted upload without confirmable metadata")
	}
	return sdkgo.NewMutationBranch(CreateFileBranchCreated, value, nil, sdkgo.Receipt{})
}

func (request boxLimitedUpload) newMultipartRequest(accessToken string) (*http.Request, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	attributes, err := json.Marshal(struct {
		Name   string `json:"name"`
		Parent struct {
			ID string `json:"id"`
		} `json:"parent"`
	}{Name: request.name, Parent: struct {
		ID string `json:"id"`
	}{ID: request.parentID}})
	if err != nil {
		return nil, err
	}
	if err := writer.WriteField("attributes", string(attributes)); err != nil {
		return nil, err
	}
	filePart, err := writer.CreateFormFile("file", request.name)
	if err != nil {
		return nil, err
	}
	if _, err := filePart.Write(request.content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(request.call.Context, http.MethodPost, request.client.uploadBaseURL+"/2.0/files/content", &body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	return httpRequest, nil
}

func (request boxLimitedUpload) decodeUploadResponse(body []byte) (CreatedBoxFile, error) {
	var response struct {
		Entries []fileMetadata `json:"entries"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return CreatedBoxFile{}, err
	}
	if len(response.Entries) != 1 {
		return CreatedBoxFile{}, fmt.Errorf("Box did not return one file")
	}
	metadata := response.Entries[0]
	if !validBoxItemID(metadata.ID, false) || !metadata.matches(metadata.ID, request.digest, int64(len(request.content))) || metadata.Name != request.name {
		return CreatedBoxFile{}, fmt.Errorf("Box upload metadata does not match")
	}
	return request.createdFile(metadata), nil
}

func (request boxLimitedUpload) readBack() (CreatedBoxFile, boxReadBackState) {
	for offset := 0; offset <= 10000; offset += 1000 {
		path := "/2.0/folders/" + url.PathEscape(request.parentID) + "/items?fields=id,type,name,sha1,size&limit=1000&offset=" + strconv.Itoa(offset)
		response := request.client.requestBoxItem(request.call, http.MethodGet, path)
		if response.err != nil || response.status != http.StatusOK {
			return CreatedBoxFile{}, boxReadBackRetry
		}
		var page struct {
			TotalCount int            `json:"total_count"`
			Entries    []fileMetadata `json:"entries"`
		}
		if err := json.Unmarshal(response.body, &page); err != nil || page.Entries == nil || page.TotalCount < 0 {
			return CreatedBoxFile{}, boxReadBackRetry
		}
		for _, metadata := range page.Entries {
			if metadata.Name != request.name {
				continue
			}
			if !validBoxItemID(metadata.ID, false) || !metadata.matches(metadata.ID, request.digest, int64(len(request.content))) {
				return CreatedBoxFile{}, boxReadBackConflicted
			}
			return request.createdFile(metadata), boxReadBackMatched
		}
		if offset+len(page.Entries) >= page.TotalCount {
			return CreatedBoxFile{}, boxReadBackAbsent
		}
		if len(page.Entries) == 0 {
			return CreatedBoxFile{}, boxReadBackRetry
		}
	}
	return CreatedBoxFile{}, boxReadBackRetry
}

func (request boxLimitedUpload) createdFile(metadata fileMetadata) CreatedBoxFile {
	return CreatedBoxFile{FileID: metadata.ID, Name: metadata.Name, SHA1: strings.ToUpper(metadata.SHA1), ParentID: request.parentID}
}

func (request boxLimitedUpload) retry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CreatedBoxFile] {
	return sdkgo.NewMutationRetry[CreatedBoxFile](queryFailure("createFile", kind, message), retryAfter)
}

func (request boxLimitedUpload) uncertain(message string) sdkgo.MutationAttempt[CreatedBoxFile] {
	return sdkgo.NewMutationUncertain(CreatedBoxFile{}, queryFailure("createFile", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}

func createFileFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedBoxFile] {
	failure := queryFailure("createFile", kind, message)
	return sdkgo.NewMutationBranch(branch, CreatedBoxFile{}, &failure, sdkgo.Receipt{})
}
