// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateDocumentInput supplies a complete source for a provider-generated ID.
type CreateDocumentInput struct {
	// Index is one concrete lowercase index name.
	Index string `json:"index"`
	// Source is a complete JSON object without credentials or secrets.
	Source json.RawMessage `json:"source"`
}

// CreateDocumentOperation creates a document with a generated ID and bounded sends.
type CreateDocumentOperation struct{ client *Client }

// CreateDocument returns the typed document creation Mutation.
func (client *Client) CreateDocument() CreateDocumentOperation {
	return CreateDocumentOperation{client}
}

// Definition returns the manifest-generated creation contract.
func (CreateDocumentOperation) Definition() sdkgo.MutationDefinition { return CreateDocumentDefinition }

// IdempotencyKey records the Call ID; Elasticsearch does not deduplicate generated-ID writes.
func (CreateDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates at most one document in this attempt, bounded by the durable send cap.
func (operation CreateDocumentOperation) Invoke(call sdkgo.Call, input CreateDocumentInput) sdkgo.MutationAttempt[IndexedDocument] {
	if !validIndexName(input.Index) || len(input.Source) == 0 || int64(len(input.Source)) > operation.client.maxDocumentBytes || !validDocumentSource(input.Source) {
		return createDocumentFailure(CreateDocumentBranchDefect, sdkgo.FailureValidation, "index or source is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateDocumentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createDocumentFailure(CreateDocumentBranchDefect, sdkgo.FailureLocalDefect, "document dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createDocumentRetry(sdkgo.FailureAvailability, "document dispatch checkpoint cannot be trusted", 0)
	}
	if decision.Marker.ProviderID != "" {
		readBack := exchangeDocument(operation.client, call, http.MethodGet, input.Index, decision.Marker.ProviderID, nil)
		if readBack.requestFailure != nil {
			return createDocumentRetry(sdkgo.FailureAvailability, "generated document ID cannot be verified", 0)
		}
		if readBack.shouldRetry {
			return createDocumentRetry(readBack.retryKind, "generated document read-back is unavailable", readBack.retryAfter)
		}
		if readBack.status == http.StatusOK {
			var document struct {
				Index string `json:"_index"`
				ID    string `json:"_id"`
				Found bool   `json:"found"`
			}
			if json.Unmarshal(readBack.body, &document) == nil && document.Index == input.Index && document.ID == decision.Marker.ProviderID && document.Found {
				return sdkgo.NewMutationBranch(CreateDocumentBranchCreated, IndexedDocument{Index: input.Index, ID: document.ID}, nil, sdkgo.Receipt{})
			}
		}
		if readBack.status != http.StatusNotFound {
			return createDocumentRetry(sdkgo.FailureProtocol, "generated document read-back is inconclusive", 0)
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createDocumentUncertain("generated document ID is unavailable after the send cap")
	}
	var exchange documentExchange
	err = fence.Dispatch(func(requestContext context.Context) error {
		exchange = exchangeIndexWithContext(operation.client, call, requestContext, http.MethodPost, "/"+input.Index+"/_doc", input.Source)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return createDocumentRetry(sdkgo.FailureAvailability, "document creation was not dispatched", 0)
	}
	if err != nil {
		return createDocumentRetry(sdkgo.FailureAvailability, "document creation dispatch was not allowed", 0)
	}
	if exchange.requestFailure != nil {
		if decision.MaximumPriorSends > 0 {
			return createDocumentRetry(exchange.requestFailure.Kind, exchange.requestFailure.Message, 0)
		}
		if recordErr := fence.RecordCanResend(); recordErr != nil {
			return createDocumentRetry(sdkgo.FailureAvailability, "document retry checkpoint is unavailable", 0)
		}
		return createDocumentFailure(CreateDocumentBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		if exchange.status == http.StatusTooManyRequests || exchange.status == http.StatusServiceUnavailable {
			if recordErr := fence.RecordCanResend(); recordErr != nil {
				return createDocumentRetry(sdkgo.FailureAvailability, "document retry checkpoint is unavailable", 0)
			}
		}
		return createDocumentRetry(exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	if exchange.status != http.StatusCreated {
		if isDefiniteElasticsearchCreateRefusal(exchange.status, exchange.body) && decision.MaximumPriorSends == 0 {
			if recordErr := fence.RecordCanResend(); recordErr != nil {
				return createDocumentRetry(sdkgo.FailureAvailability, "document retry checkpoint is unavailable", 0)
			}
			return createDocumentFailure(CreateDocumentBranchProviderRejected, indexStatusFailureKind(exchange.status), "Elasticsearch rejected document creation with HTTP "+strconv.Itoa(exchange.status))
		}
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return createDocumentUncertain("document creation cannot be confirmed after the send cap")
		}
		return createDocumentRetry(sdkgo.FailureAvailability, "document creation outcome is unknown", 0)
	}
	var response struct {
		Index  string `json:"_index"`
		ID     string `json:"_id"`
		Result string `json:"result"`
	}
	if json.Unmarshal(exchange.body, &response) != nil || response.Index != input.Index || !validDocumentID(response.ID) || response.Result != "created" {
		return createDocumentRetry(sdkgo.FailureProtocol, "Elasticsearch creation confirmation is unusable", 0)
	}
	if recordErr := fence.RecordProviderID(response.ID); recordErr != nil {
		return createDocumentRetry(sdkgo.FailureAvailability, "document ID checkpoint is unavailable", 0)
	}
	return sdkgo.NewMutationBranch(CreateDocumentBranchCreated, IndexedDocument{Index: input.Index, ID: response.ID}, nil, sdkgo.Receipt{})
}

func isDefiniteElasticsearchCreateRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var response struct {
		Error struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
		ID string `json:"_id"`
	}
	return json.Unmarshal(body, &response) == nil && response.Error.Type != "" && response.Error.Reason != "" && response.ID == ""
}

func createDocumentFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[IndexedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "createDocument", Message: message}
	return sdkgo.NewMutationBranch(branch, IndexedDocument{}, &failure, sdkgo.Receipt{})
}

func createDocumentRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[IndexedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "createDocument", Message: message}
	return sdkgo.NewMutationRetry[IndexedDocument](failure, retryAfter)
}

func createDocumentUncertain(message string) sdkgo.MutationAttempt[IndexedDocument] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "elasticsearch", Operation: "createDocument", Message: message}
	return sdkgo.NewMutationUncertain(IndexedDocument{}, failure, sdkgo.Receipt{})
}
