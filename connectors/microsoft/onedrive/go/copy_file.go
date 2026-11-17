// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const copyFileOperationID = "copyFile"

// CopyFileInput identifies a source file and a unique destination path.
type CopyFileInput struct {
	// DriveID is the source drive; blank selects the signed-in user's OneDrive.
	DriveID string `json:"driveId,omitempty"`
	// ItemID is the source file ID.
	ItemID string `json:"itemId"`
	// DestinationDriveID is the destination drive ID; blank selects the signed-in user's OneDrive.
	DestinationDriveID string `json:"destinationDriveId,omitempty"`
	// DestinationFolderID is the destination folder ID, or root.
	DestinationFolderID string `json:"destinationFolderId"`
	// Name is the new file name, unique in the destination folder.
	Name string `json:"name"`
}

// CopyFileOutput describes an accepted copy job or a reconciled destination.
type CopyFileOutput struct {
	// DestinationDriveID is the requested destination drive.
	DestinationDriveID string `json:"destinationDriveId,omitempty"`
	// DestinationFolderID is the requested destination folder.
	DestinationFolderID string `json:"destinationFolderId"`
	// Name is the requested destination name.
	Name string `json:"name"`
	// ItemID is set when read-back confirms a copied item; Graph's 202 response has no item ID.
	ItemID string `json:"itemId,omitempty"`
	// IsQueued reports Graph's asynchronous acceptance, which does not establish copy completion.
	IsQueued bool `json:"isQueued,omitempty"`
}

// CopyFileOperation implements the copyFile mutation.
type CopyFileOperation struct{ client *Client }

var copyFileReadBranches = readBranches{
	operationID: copyFileOperationID, notFound: CopyFileBranchNameConflict,
	providerRejected: CopyFileBranchProviderRejected, invalidResponse: CopyFileBranchInvalidResponse,
	defect: CopyFileBranchDefect,
}

// Definition returns the mutation definition.
func (CopyFileOperation) Definition() sdkgo.MutationDefinition { return CopyFileDefinition }

// IdempotencyKey derives the receipt key from the stable call ID.
func (CopyFileOperation) IdempotencyKey(callID sdkgo.CallID, _ CopyFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke queues one copy and checks the destination before any permitted resend.
func (operation CopyFileOperation) Invoke(call sdkgo.Call, input CopyFileInput) sdkgo.MutationAttempt[CopyFileOutput] {
	if validateDriveAndFolderIDs(input.DriveID, input.ItemID) != nil || input.ItemID == "" || input.ItemID == rootFolderAlias ||
		validateDriveAndFolderIDs(input.DestinationDriveID, input.DestinationFolderID) != nil || input.DestinationFolderID == "" || validateItemName(input.Name, false) != nil ||
		(input.DriveID == input.DestinationDriveID && input.ItemID == input.DestinationFolderID) {
		return sdkgo.NewMutationBranch(CopyFileBranchDefect, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureValidation, copyFileOperationID, "source or destination is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CopyFileDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CopyFileBranchDefect, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureLocalDefect, copyFileOperationID, "copy dispatch checkpoint is invalid"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationBranch(CopyFileBranchDefect, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureLocalDefect, copyFileOperationID, "copy dispatch checkpoint could not be read"), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, copyFileOperationID, input.DriveID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CopyFileBranchDefect, CopyFileOutput{}, failure, sdkgo.Receipt{})
	}
	if input.DestinationDriveID == "" && credential.AuthMethodID == MicrosoftAppOnlyAuthMethodID {
		return sdkgo.NewMutationBranch(CopyFileBranchDefect, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureValidation, copyFileOperationID, "destinationDriveId is required with app-only authorization"), sdkgo.Receipt{})
	}
	item, isFound, outcome := operation.client.readCopyDestination(call, &credential, input)
	if outcome != nil {
		return mutationAttemptFromReadOutcome[CopyFileOutput](outcome)
	}
	baseOutput := CopyFileOutput{DestinationDriveID: input.DestinationDriveID, DestinationFolderID: input.DestinationFolderID, Name: input.Name}
	if isFound {
		if decision.MaximumPriorSends > 0 {
			baseOutput.ItemID = item.ID
			return sdkgo.NewMutationBranch(CopyFileBranchQueued, baseOutput, nil, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CopyFileBranchNameConflict, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureConflict, copyFileOperationID, "destination name already exists"), sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(baseOutput, graphFailure(sdkgo.FailureTransport, copyFileOperationID, "copy destination is absent after the send cap"), sdkgo.Receipt{})
	}
	parentReference := map[string]string{"id": input.DestinationFolderID}
	if input.DestinationDriveID != "" {
		parentReference["driveId"] = input.DestinationDriveID
	}
	requestBody, err := json.Marshal(map[string]any{"name": input.Name, "parentReference": parentReference})
	if err != nil {
		return sdkgo.NewMutationBranch(CopyFileBranchDefect, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureLocalDefect, copyFileOperationID, "copy request could not be encoded"), sdkgo.Receipt{})
	}
	var response graphResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		response, sendErr = operation.client.sendRequest(call, &credential, graphRequest{
			method: http.MethodPost, target: operation.client.itemURL(input.DriveID, input.ItemID) + "/copy",
			body: requestBody, contentType: "application/json", responseLimit: operation.client.maxResponseBytes, requestContext: requestContext,
		})
		return sendErr
	})
	if err != nil {
		var requestError *graphRequestError
		if errors.As(err, &requestError) && requestError.kind == sdkgo.FailureLocalDefect {
			return sdkgo.NewMutationBranch(CopyFileBranchDefect, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureLocalDefect, copyFileOperationID, requestError.message), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[CopyFileOutput](graphFailure(sdkgo.FailureTransport, copyFileOperationID, "copy send outcome is unknown"), 0)
	}
	receipt := operation.client.receipt(call, response.requestID, input.ItemID)
	if response.status == http.StatusAccepted {
		baseOutput.IsQueued = true
		return sdkgo.NewMutationBranch(CopyFileBranchQueued, baseOutput, nil, receipt)
	}
	codes := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	kind := statusFailureKind(response.status, codes)
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CopyFileOutput](graphFailure(sdkgo.FailureAvailability, copyFileOperationID, "copy rejection checkpoint could not be recorded"), 0)
		}
		return sdkgo.NewMutationRetry[CopyFileOutput](graphFailure(kind, copyFileOperationID, "provider temporarily rejected the copy"), providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), operation.client.now()))
	}
	if isRetryableStatus(response.status, codes) {
		return sdkgo.NewMutationRetry[CopyFileOutput](graphFailure(kind, copyFileOperationID, "copy outcome is unknown"), 0)
	}
	if response.status == http.StatusConflict {
		return sdkgo.NewMutationBranch(CopyFileBranchNameConflict, CopyFileOutput{}, graphFailurePointer(sdkgo.FailureConflict, copyFileOperationID, "destination name already exists"), receipt)
	}
	return sdkgo.NewMutationBranch(CopyFileBranchProviderRejected, CopyFileOutput{}, graphFailurePointer(kind, copyFileOperationID, "provider rejected the copy"), receipt)
}

func (client *Client) readCopyDestination(call sdkgo.Call, credential *Credentials, input CopyFileInput) (DriveItem, bool, *readOutcome) {
	response, outcome := client.sendRead(call, credential, copyFileReadBranches, graphRequest{
		method: http.MethodGet, target: client.childPathURL(input.DestinationDriveID, input.DestinationFolderID, input.Name) + "?" + buildODataQuery([2]string{"$select", itemFields}),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		if outcome.branch == CopyFileBranchNameConflict {
			return DriveItem{}, false, nil
		}
		return DriveItem{}, false, outcome
	}
	item, err := decodeItemResponse(response.body)
	if err != nil || item.Name != input.Name || item.IsFolder {
		return DriveItem{}, false, &readOutcome{branch: CopyFileBranchInvalidResponse, failure: graphFailure(sdkgo.FailureProtocol, copyFileOperationID, "provider returned invalid copy destination metadata"), receipt: client.receipt(call, response.requestID, "")}
	}
	return item, true, nil
}
