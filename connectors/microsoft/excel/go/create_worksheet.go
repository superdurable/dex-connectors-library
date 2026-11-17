// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createWorksheetOperationID = "createWorksheet"

// CreateWorksheetInput names a new worksheet in one workbook.
type CreateWorksheetInput struct {
	// DriveID is the Graph drive that holds the workbook.
	DriveID string `json:"driveId"`
	// WorkbookID is the workbook's drive item ID.
	WorkbookID string `json:"workbookId"`
	// Name is a unique Excel worksheet name of 1 to 31 characters.
	Name string `json:"name"`
}

// CreateWorksheetOutput identifies the created worksheet.
type CreateWorksheetOutput struct {
	// Worksheet is the created or reconciled sheet.
	Worksheet Worksheet `json:"worksheet"`
}

// CreateWorksheetOperation implements the createWorksheet mutation.
type CreateWorksheetOperation struct{ client *Client }

var createWorksheetReadBranches = readBranches{
	operationID:      createWorksheetOperationID,
	notFound:         CreateWorksheetBranchNameConflict,
	tooLarge:         CreateWorksheetBranchInvalidResponse,
	providerRejected: CreateWorksheetBranchProviderRejected,
	invalidResponse:  CreateWorksheetBranchInvalidResponse,
	defect:           CreateWorksheetBranchDefect,
}

// Definition returns the mutation definition.
func (CreateWorksheetOperation) Definition() sdkgo.MutationDefinition {
	return CreateWorksheetDefinition
}

// IdempotencyKey identifies the durable Step call used for bounded sends.
func (CreateWorksheetOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateWorksheetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke checks the unique name before one fenced POST and reconciles later attempts by name.
func (operation CreateWorksheetOperation) Invoke(call sdkgo.Call, input CreateWorksheetInput) sdkgo.MutationAttempt[CreateWorksheetOutput] {
	if err := validateWorkbookLocation(input.DriveID, input.WorkbookID); err != nil || validateWorksheetReference(input.Name) != nil || worksheetIDPattern.MatchString(input.Name) {
		return sdkgo.NewMutationBranch(CreateWorksheetBranchDefect, CreateWorksheetOutput{}, excelFailurePointer(sdkgo.FailureValidation, createWorksheetOperationID, "workbook or worksheet name is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateWorksheetDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateWorksheetBranchDefect, CreateWorksheetOutput{}, excelFailurePointer(sdkgo.FailureLocalDefect, createWorksheetOperationID, "worksheet dispatch checkpoint is invalid"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationBranch(CreateWorksheetBranchDefect, CreateWorksheetOutput{}, excelFailurePointer(sdkgo.FailureLocalDefect, createWorksheetOperationID, "worksheet dispatch checkpoint could not be read"), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, createWorksheetOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateWorksheetBranchDefect, CreateWorksheetOutput{}, failure, sdkgo.Receipt{})
	}
	worksheet, isFound, outcome := operation.client.readWorksheetByName(call, &credential, input)
	if outcome != nil {
		return mutationAttemptFromReadOutcome[CreateWorksheetOutput](outcome)
	}
	if isFound {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationBranch(CreateWorksheetBranchCreated, CreateWorksheetOutput{Worksheet: worksheet}, nil, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreateWorksheetBranchNameConflict, CreateWorksheetOutput{}, excelFailurePointer(sdkgo.FailureConflict, createWorksheetOperationID, "worksheet name already exists"), sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CreateWorksheetOutput{}, excelFailure(sdkgo.FailureTransport, createWorksheetOperationID, "worksheet creation could not be confirmed after the send cap"), sdkgo.Receipt{})
	}
	requestBody, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return sdkgo.NewMutationBranch(CreateWorksheetBranchDefect, CreateWorksheetOutput{}, excelFailurePointer(sdkgo.FailureLocalDefect, createWorksheetOperationID, "worksheet name could not be encoded"), sdkgo.Receipt{})
	}
	var response graphResponse
	var sendErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		response, sendErr = operation.client.sendRequest(call, &credential, graphRequest{
			method: http.MethodPost, target: workbookURL(input.DriveID, input.WorkbookID) + "/worksheets/add",
			body: requestBody, responseLimit: operation.client.maxResponseBytes, requestContext: requestContext,
		})
		return sendErr
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CreateWorksheetOutput](excelFailure(sdkgo.FailureTransport, createWorksheetOperationID, "worksheet send outcome is unknown"), 0)
	}
	receipt := operation.client.receipt(call, response.requestID)
	if response.status == http.StatusCreated {
		var created Worksheet
		if response.isBodyTooLarge || json.Unmarshal(response.body, &created) != nil || created.ID == "" || created.Name != input.Name {
			return sdkgo.NewMutationRetry[CreateWorksheetOutput](excelFailure(sdkgo.FailureProtocol, createWorksheetOperationID, "worksheet response is invalid; read back before another send"), 0)
		}
		return sdkgo.NewMutationBranch(CreateWorksheetBranchCreated, CreateWorksheetOutput{Worksheet: created}, nil, receipt)
	}
	tokens := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	kind := statusFailureKind(response.status, tokens)
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CreateWorksheetOutput](excelFailure(sdkgo.FailureAvailability, createWorksheetOperationID, "worksheet rejection checkpoint could not be recorded"), 0)
		}
		return sdkgo.NewMutationRetry[CreateWorksheetOutput](excelFailure(kind, createWorksheetOperationID, "provider temporarily rejected the worksheet creation"), providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), operation.client.now()))
	}
	if classifyGraphError(response.status, tokens) == graphErrorRetryable {
		return sdkgo.NewMutationRetry[CreateWorksheetOutput](excelFailure(kind, createWorksheetOperationID, "worksheet send outcome is unknown"), 0)
	}
	if response.status == http.StatusConflict {
		return sdkgo.NewMutationBranch(CreateWorksheetBranchNameConflict, CreateWorksheetOutput{}, excelFailurePointer(sdkgo.FailureConflict, createWorksheetOperationID, "worksheet name already exists"), receipt)
	}
	return sdkgo.NewMutationBranch(CreateWorksheetBranchProviderRejected, CreateWorksheetOutput{}, excelFailurePointer(kind, createWorksheetOperationID, "provider rejected worksheet creation"), receipt)
}

func (client *Client) readWorksheetByName(call sdkgo.Call, credential *Credentials, input CreateWorksheetInput) (Worksheet, bool, *readOutcome) {
	response, outcome := client.sendRead(call, credential, createWorksheetReadBranches, graphRequest{
		method: http.MethodGet, target: workbookURL(input.DriveID, input.WorkbookID) + "/worksheets/" + url.PathEscape(input.Name) + "?$select=id,name,position,visibility",
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		if outcome.branch == CreateWorksheetBranchNameConflict {
			return Worksheet{}, false, nil
		}
		return Worksheet{}, false, outcome
	}
	var worksheet Worksheet
	if err := json.Unmarshal(response.body, &worksheet); err != nil || worksheet.ID == "" || worksheet.Name != input.Name {
		return Worksheet{}, false, &readOutcome{branch: CreateWorksheetBranchInvalidResponse, failure: excelFailure(sdkgo.FailureProtocol, createWorksheetOperationID, "provider returned invalid worksheet metadata"), receipt: client.receipt(call, response.requestID)}
	}
	return worksheet, true, nil
}
