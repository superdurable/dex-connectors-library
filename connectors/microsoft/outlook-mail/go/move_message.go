// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const moveMessageOperationID = "moveMessage"

// MoveMessageInput moves one message to another folder of the same mailbox.
type MoveMessageInput struct {
	// MessageID is the message's id from searchMessages or getMessage.
	MessageID string `json:"messageId"`
	// DestinationFolder is a folder ID from the mail folder picker or a well-known name such as archive or
	// deleteditems. The folder must exist; the connector never creates folders.
	DestinationFolder string `json:"destinationFolder"`
}

// MovedMessage is a message after the move.
type MovedMessage struct {
	// MessageID is the message's immutable ID, the same before and after the move.
	MessageID string `json:"messageId"`
	// DestinationFolderID is the ID of the folder that now holds the message.
	DestinationFolderID string `json:"destinationFolderId"`
	// DestinationFolderName is that folder's display name.
	DestinationFolderName string `json:"destinationFolderName,omitempty"`
	// WasAlreadyMoved reports that the message was already in the folder, so nothing was moved.
	WasAlreadyMoved bool `json:"wasAlreadyMoved,omitempty"`
}

// MoveMessageOperation implements the moveMessage Mutation.
type MoveMessageOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (MoveMessageOperation) Definition() sdkgo.MutationDefinition { return MoveMessageDefinition }

// IdempotencyKey uses the stable connector Call ID. A move is safe to repeat after a fresh read, so Graph
// receives no key.
func (MoveMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ MoveMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the message's folder and the destination, and moves the message only when they differ.
func (operation MoveMessageOperation) Invoke(call sdkgo.Call, input MoveMessageInput) sdkgo.MutationAttempt[MovedMessage] {
	messageID := strings.TrimSpace(input.MessageID)
	destination, err := resolveFolderSegment("destinationFolder", input.DestinationFolder, "")
	if err == nil {
		err = validateGraphID("messageId", messageID)
	}
	if err != nil {
		return sdkgo.NewMutationBranch(MoveMessageBranchDefect, MovedMessage{}, graphFailurePointer(sdkgo.FailureValidation, moveMessageOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, failed := operation.client.openSession(call, moveMessageOperationID)
	if failed != nil {
		attempt, _ := updateAttemptForExchange[MovedMessage](*failed, sdkgo.Receipt{}, moveMessageBranches)
		return attempt
	}
	messagePath := "/messages/" + url.PathEscape(messageID)
	read := session.exchange(graphRequest{method: http.MethodGet, path: messagePath, query: graphQuery{{name: "$select", value: "id,parentFolderId"}}})
	if attempt, isTerminal := updateAttemptForExchange[MovedMessage](read, session.receipt(read, messageID), moveMessageBranches); isTerminal {
		return attempt
	}
	message, err := decodeGraphMessage(read.body)
	if err != nil {
		return sdkgo.NewMutationRetry[MovedMessage](graphFailure(sdkgo.FailureProtocol, moveMessageOperationID, "Microsoft Graph returned an invalid message: "+err.Error()), 0)
	}
	folder, folderResult := readDestinationFolder(session, destination)
	if folderResult.outcome == graphNotFound {
		failure := folderResult.failure
		failure.Message = "the destination folder does not exist in the mailbox; the connector never creates folders"
		return sdkgo.NewMutationBranch(MoveMessageBranchProviderRejected, MovedMessage{}, &failure, session.receipt(folderResult, messageID))
	}
	if attempt, isTerminal := updateAttemptForExchange[MovedMessage](folderResult, session.receipt(folderResult, messageID), moveMessageBranches); isTerminal {
		return attempt
	}
	if message.ParentFolderID == folder.ID {
		return sdkgo.NewMutationBranch(MoveMessageBranchMoved, MovedMessage{
			MessageID: message.ID, DestinationFolderID: folder.ID, DestinationFolderName: folder.DisplayName, WasAlreadyMoved: true,
		}, nil, session.receipt(read, message.ID))
	}
	moved := session.exchange(graphRequest{method: http.MethodPost, path: messagePath + "/move", payload: map[string]string{"destinationId": folder.ID}})
	receipt := session.receipt(moved, messageID)
	if moved.outcome == graphNotFound {
		// A message that vanished during the move is re-read by the next attempt.
		return sdkgo.NewMutationRetry[MovedMessage](moved.failure, 0)
	}
	if attempt, isTerminal := updateAttemptForExchange[MovedMessage](moved, receipt, moveMessageBranches); isTerminal {
		return attempt
	}
	result, err := decodeGraphMessage(moved.body)
	if err != nil {
		return sdkgo.NewMutationRetry[MovedMessage](graphFailure(sdkgo.FailureProtocol, moveMessageOperationID, "Microsoft Graph returned an invalid moved message: "+err.Error()), 0)
	}
	return sdkgo.NewMutationBranch(MoveMessageBranchMoved, MovedMessage{
		MessageID: result.ID, DestinationFolderID: folder.ID, DestinationFolderName: folder.DisplayName,
	}, nil, session.receipt(moved, result.ID))
}

var moveMessageBranches = updateBranches{
	notFound: MoveMessageBranchNotFound, providerRejected: MoveMessageBranchProviderRejected, defect: MoveMessageBranchDefect,
}

// mailFolderWire holds the folder properties the connector selects.
type mailFolderWire struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// readDestinationFolder resolves a well-known name or folder ID to the folder's ID, which parentFolderId uses.
func readDestinationFolder(session *graphSession, destination string) (mailFolderWire, graphExchange) {
	result := session.exchange(graphRequest{
		method: http.MethodGet, path: "/mailFolders/" + url.PathEscape(destination),
		query: graphQuery{{name: "$select", value: "id,displayName"}},
	})
	if result.outcome != graphSucceeded {
		return mailFolderWire{}, result
	}
	var folder mailFolderWire
	if err := json.Unmarshal(result.body, &folder); err != nil || validateGraphID("folder id", folder.ID) != nil {
		result.outcome = graphInvalid
		result.failure = graphFailure(sdkgo.FailureProtocol, session.operation, "Microsoft Graph returned a mail folder without a usable id")
		return mailFolderWire{}, result
	}
	return folder, result
}
