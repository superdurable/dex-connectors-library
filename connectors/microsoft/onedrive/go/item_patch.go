// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

type itemPatchBranches struct {
	completed        sdkgo.BranchID
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

func invokeItemPatch[Output any](client *Client, call sdkgo.Call, driveID, itemID, operationID string, requestBody any, branches itemPatchBranches, makeOutput func(DriveItem) Output) sdkgo.MutationAttempt[Output] {
	credential, failure := client.resolveCredential(call, operationID, driveID)
	if failure != nil {
		return sdkgo.NewMutationBranch(branches.defect, *new(Output), failure, sdkgo.Receipt{})
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return sdkgo.NewMutationBranch(branches.defect, *new(Output), graphFailurePointer(sdkgo.FailureLocalDefect, operationID, "item update could not be encoded"), sdkgo.Receipt{})
	}
	response, err := client.sendRequest(call, &credential, graphRequest{
		method: http.MethodPatch, target: client.itemURL(driveID, itemID), body: body,
		contentType: "application/json", responseLimit: client.maxResponseBytes,
	})
	receipt := client.receipt(call, response.requestID, itemID)
	var requestError *graphRequestError
	if errors.As(err, &requestError) && requestError.kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewMutationBranch(branches.defect, *new(Output), graphFailurePointer(sdkgo.FailureLocalDefect, operationID, requestError.message), receipt)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Output](graphFailure(sdkgo.FailureTransport, operationID, "item update outcome is unknown"), 0)
	}
	if response.status == http.StatusOK {
		item, err := decodeItemResponse(response.body)
		if err != nil || item.ID != itemID {
			return sdkgo.NewMutationRetry[Output](graphFailure(sdkgo.FailureProtocol, operationID, "item update response is invalid; repeat the absolute update"), 0)
		}
		receipt.ProviderObjectID = item.ID
		return sdkgo.NewMutationBranch(branches.completed, makeOutput(item), nil, receipt)
	}
	codes := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	if isRetryableStatus(response.status, codes) {
		return sdkgo.NewMutationRetry[Output](graphFailure(statusFailureKind(response.status, codes), operationID, "provider temporarily rejected item update"), providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now()))
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(branches.notFound, *new(Output), graphFailurePointer(sdkgo.FailureNotFound, operationID, "item or destination folder was not found"), receipt)
	}
	return sdkgo.NewMutationBranch(branches.providerRejected, *new(Output), graphFailurePointer(statusFailureKind(response.status, codes), operationID, "provider rejected item update"), receipt)
}
