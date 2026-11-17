// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateDocumentInput merges absolute field values into an existing document.
type UpdateDocumentInput struct {
	// Index is one concrete lowercase index name.
	Index string `json:"index"`
	// DocumentID is the stable ID of the document to update.
	DocumentID string `json:"documentId"`
	// Fields is a nonempty JSON object of absolute values to merge.
	Fields json.RawMessage `json:"fields"`
}

// UpdateDocumentOperation merges fields without a script or increment.
type UpdateDocumentOperation struct{ client *Client }

// UpdateDocument returns the typed partial update Mutation.
func (client *Client) UpdateDocument() UpdateDocumentOperation {
	return UpdateDocumentOperation{client}
}

// Definition returns the manifest-generated partial update contract.
func (UpdateDocumentOperation) Definition() sdkgo.MutationDefinition { return UpdateDocumentDefinition }

// IdempotencyKey records the call identity; repeating absolute field values converges.
func (UpdateDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke merges absolute field values and retries an unknown provider outcome.
func (operation UpdateDocumentOperation) Invoke(call sdkgo.Call, input UpdateDocumentInput) sdkgo.MutationAttempt[IndexedDocument] {
	if !validIndexName(input.Index) || !validDocumentID(input.DocumentID) || len(input.Fields) == 0 || int64(len(input.Fields)) > operation.client.maxDocumentBytes || !validDocumentSource(input.Fields) {
		return updateDocumentFailure(UpdateDocumentBranchDefect, sdkgo.FailureValidation, "index, documentId, or fields is invalid")
	}
	body, err := json.Marshal(struct {
		Doc json.RawMessage `json:"doc"`
	}{Doc: input.Fields})
	if err != nil {
		return updateDocumentFailure(UpdateDocumentBranchDefect, sdkgo.FailureLocalDefect, "document update request could not be encoded")
	}
	exchange := exchangeIndex(operation.client, call, http.MethodPost, "/"+input.Index+"/_update/"+url.PathEscape(input.DocumentID), body)
	if exchange.requestFailure != nil {
		return updateDocumentFailure(UpdateDocumentBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		return updateDocumentRetry(exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	switch exchange.status {
	case http.StatusOK:
		var wire struct {
			Index  string `json:"_index"`
			ID     string `json:"_id"`
			Result string `json:"result"`
		}
		if json.Unmarshal(exchange.body, &wire) != nil || wire.Index != input.Index || wire.ID != input.DocumentID || wire.Result != "updated" && wire.Result != "noop" {
			return updateDocumentRetry(sdkgo.FailureProtocol, "Elasticsearch update confirmation is unusable", 0)
		}
		return sdkgo.NewMutationBranch(UpdateDocumentBranchUpdated, IndexedDocument{Index: input.Index, ID: input.DocumentID}, nil, sdkgo.Receipt{})
	case http.StatusNotFound:
		return updateDocumentFailure(UpdateDocumentBranchNotFound, sdkgo.FailureNotFound, "Elasticsearch document or index was not found")
	default:
		return updateDocumentFailure(UpdateDocumentBranchProviderRejected, indexStatusFailureKind(exchange.status), "Elasticsearch rejected document update with HTTP "+strconv.Itoa(exchange.status))
	}
}

func updateDocumentFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[IndexedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "updateDocument", Message: message}
	return sdkgo.NewMutationBranch(branch, IndexedDocument{}, &failure, sdkgo.Receipt{})
}

func updateDocumentRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[IndexedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "updateDocument", Message: message}
	return sdkgo.NewMutationRetry[IndexedDocument](failure, retryAfter)
}
