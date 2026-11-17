// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// TransferPathInput identifies a source file or folder and a fixed destination path.
type TransferPathInput struct {
	// FromPath is an absolute Dropbox path or stable file or folder ID.
	FromPath string `json:"fromPath"`
	// ToPath is an absolute destination path; autorename is disabled.
	ToPath string `json:"toPath"`
}

// CopyPathOperation copies a file or folder to a fixed destination.
type CopyPathOperation struct{ client *Client }

// MovePathOperation moves a file or folder to a fixed destination.
type MovePathOperation struct{ client *Client }

// CopyPath binds the copy Mutation to this client.
func (client *Client) CopyPath() CopyPathOperation { return CopyPathOperation{client} }

// MovePath binds the move Mutation to this client.
func (client *Client) MovePath() MovePathOperation { return MovePathOperation{client} }

// Definition returns the generated copy contract.
func (CopyPathOperation) Definition() sdkgo.MutationDefinition { return CopyPathDefinition }

// Definition returns the generated move contract.
func (MovePathOperation) Definition() sdkgo.MutationDefinition { return MovePathDefinition }

// IdempotencyKey records the stable Call ID; Dropbox does not accept it as a deduplication key.
func (CopyPathOperation) IdempotencyKey(callID sdkgo.CallID, _ TransferPathInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the stable Call ID; Dropbox does not accept it as a deduplication key.
func (MovePathOperation) IdempotencyKey(callID sdkgo.CallID, _ TransferPathInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke copies a path with bounded resend after destination read-back.
func (operation CopyPathOperation) Invoke(call sdkgo.Call, input TransferPathInput) sdkgo.MutationAttempt[EntryMetadata] {
	return invokePathTransfer(call, operation.client, "copyPath", "/2/files/copy_v2", input, CopyPathDefinition.StepDefaults.ExecuteMethodTimeout,
		CopyPathBranchCopied, CopyPathBranchProviderRejected, CopyPathBranchDefect)
}

// Invoke moves a path with bounded resend after destination read-back.
func (operation MovePathOperation) Invoke(call sdkgo.Call, input TransferPathInput) sdkgo.MutationAttempt[EntryMetadata] {
	return invokePathTransfer(call, operation.client, "movePath", "/2/files/move_v2", input, MovePathDefinition.StepDefaults.ExecuteMethodTimeout,
		MovePathBranchMoved, MovePathBranchProviderRejected, MovePathBranchDefect)
}

func invokePathTransfer(call sdkgo.Call, client *Client, operation, endpoint string, input TransferPathInput, executeTimeout time.Duration, success, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[EntryMetadata] {
	if !validDropboxPath(input.FromPath, false) || strings.HasPrefix(input.FromPath, "rev:") || !validAbsoluteDropboxPath(input.ToPath) || strings.EqualFold(input.FromPath, input.ToPath) {
		return transferFailure(operation, defect, sdkgo.FailureValidation, "source or destination path is invalid")
	}
	requestBody, err := json.Marshal(struct {
		FromPath   string `json:"from_path"`
		ToPath     string `json:"to_path"`
		Autorename bool   `json:"autorename"`
	}{FromPath: input.FromPath, ToPath: input.ToPath})
	if err != nil {
		return transferFailure(operation, defect, sdkgo.FailureLocalDefect, "transfer request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return transferFailure(operation, defect, sdkgo.FailureLocalDefect, "transfer dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return transferUncertain(operation, "transfer checkpoint cannot be trusted")
	}
	if decision.Kind != singledispatch.SendAllowed {
		readBack := readTransferDestination(call, client, input.ToPath)
		if readBack.isPresent {
			if decision.Marker.ProviderID != "" && readBack.entry.ID == decision.Marker.ProviderID {
				return sdkgo.NewMutationBranch(success, readBack.entry, nil, sdkgo.Receipt{})
			}
			return transferUncertain(operation, "destination exists but cannot be attributed to this call")
		}
		if !readBack.isAbsent {
			return transferRetry(operation, readBack.failureKind, "destination read-back is unavailable", readBack.retryAfter)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return transferUncertain(operation, "send cap reached and destination is absent")
		}
	}
	var body []byte
	var status int
	var retryAfter time.Duration
	var requestErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		body, status, retryAfter, requestErr = client.postWithContext(call, requestContext, endpoint, requestBody)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return transferRetry(operation, sdkgo.FailureAvailability, "transfer was not dispatched", 0)
	}
	if err != nil {
		return transferUncertain(operation, "transfer dispatch was not allowed")
	}
	if errors.Is(requestErr, errCredential) {
		return transferFailure(operation, defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(requestErr, errRefresh) {
		_ = fence.RecordCanResend()
		return transferRetry(operation, sdkgo.FailureAvailability, "Dropbox credentials could not be refreshed", 0)
	}
	if errors.Is(requestErr, errRequest) {
		return transferRetry(operation, sdkgo.FailureTransport, "Dropbox transfer request failed", 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if recordErr := fence.RecordCanResend(); recordErr != nil {
			return transferRetry(operation, sdkgo.FailureAvailability, "transfer retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return transferRetry(operation, kind, "Dropbox did not process the transfer", retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return transferRetry(operation, sdkgo.FailureAvailability, "Dropbox transfer outcome is unknown", retryAfter)
	}
	if status != http.StatusOK {
		return transferFailure(operation, rejected, sdkgo.FailureProviderRejection, "Dropbox rejected transfer with HTTP "+strconv.Itoa(status))
	}
	if requestErr != nil {
		return transferRetry(operation, sdkgo.FailureTransport, "Dropbox transfer response was interrupted", 0)
	}
	var response struct {
		Metadata json.RawMessage `json:"metadata"`
	}
	if json.Unmarshal(body, &response) != nil {
		return transferRetry(operation, sdkgo.FailureProtocol, "Dropbox transfer confirmation was invalid", 0)
	}
	entry, err := decodeEntry(response.Metadata)
	if err != nil || !strings.EqualFold(entry.PathDisplay, input.ToPath) {
		return transferRetry(operation, sdkgo.FailureProtocol, "Dropbox transfer identity was invalid", 0)
	}
	_ = fence.RecordProviderID(entry.ID)
	return sdkgo.NewMutationBranch(success, entry, nil, sdkgo.Receipt{})
}

type transferReadBack struct {
	entry       EntryMetadata
	isPresent   bool
	isAbsent    bool
	failureKind sdkgo.FailureKind
	retryAfter  time.Duration
}

func readTransferDestination(call sdkgo.Call, client *Client, destination string) transferReadBack {
	payload, err := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: destination})
	if err != nil {
		return transferReadBack{failureKind: sdkgo.FailureLocalDefect}
	}
	body, status, retryAfter, err := client.post(call, "/2/files/get_metadata", payload)
	if err != nil {
		return transferReadBack{failureKind: sdkgo.FailureAvailability, retryAfter: retryAfter}
	}
	if status == http.StatusOK {
		entry, decodeErr := decodeEntry(body)
		if decodeErr == nil {
			return transferReadBack{entry: entry, isPresent: true}
		}
	}
	if status == http.StatusConflict {
		var providerError struct {
			ErrorSummary string `json:"error_summary"`
		}
		if json.Unmarshal(body, &providerError) == nil && strings.HasPrefix(providerError.ErrorSummary, "path/not_found") {
			return transferReadBack{isAbsent: true}
		}
	}
	return transferReadBack{failureKind: sdkgo.FailureAvailability, retryAfter: retryAfter}
}

func transferFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EntryMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, EntryMetadata{}, &failure, sdkgo.Receipt{})
}

func transferRetry(operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[EntryMetadata] {
	return sdkgo.NewMutationRetry[EntryMetadata](queryFailure(operation, kind, message), retryAfter)
}

func transferUncertain(operation, message string) sdkgo.MutationAttempt[EntryMetadata] {
	return sdkgo.NewMutationUncertain(EntryMetadata{}, queryFailure(operation, sdkgo.FailureAvailability, message), sdkgo.Receipt{})
}
