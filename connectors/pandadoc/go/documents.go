// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pandadoc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListDocumentsInput selects a page and optional name search.
type ListDocumentsInput struct {
	// Query matches a document name or reference number; blank lists all.
	Query string `json:"query,omitempty"`
	// Count is the number of documents from 1 to 100; zero selects 50.
	Count int `json:"count,omitempty"`
	// Page starts at 1; zero selects the first page.
	Page int `json:"page,omitempty"`
}

// DocumentPage contains one search result page.
type DocumentPage struct {
	// Results is the returned document status list.
	Results []DocumentStatus `json:"results"`
	// Count is the total matching document count.
	Count int `json:"count"`
}

// UpdateDocumentInput renames one draft document.
type UpdateDocumentInput struct {
	// DocumentID identifies the draft document.
	DocumentID string `json:"documentId"`
	// Name is its replacement name.
	Name string `json:"name"`
}

// DeleteDocumentInput identifies one document to remove.
type DeleteDocumentInput struct {
	// DocumentID is the provider document ID.
	DocumentID string `json:"documentId"`
}

// DocumentAction confirms a document action by its ID.
type DocumentAction struct {
	// DocumentID identifies the affected document.
	DocumentID string `json:"documentId"`
}

// DocumentRecipient assigns a recipient to a template role.
type DocumentRecipient struct {
	// Email is the recipient's address.
	Email string `json:"email"`
	// Role matches a role in the selected template.
	Role string `json:"role"`
}

// CreateDocumentInput creates a document from a workspace template.
type CreateDocumentInput struct {
	// TemplateID is the ID in a PandaDoc template URL.
	TemplateID string `json:"templateId"`
	// Name is the new document name.
	Name string `json:"name"`
	// Recipients assigns at least one email to the template's roles.
	Recipients []DocumentRecipient `json:"recipients"`
}

// SendDocumentInput sends one draft to its assigned recipients.
type SendDocumentInput struct {
	// DocumentID identifies a draft document.
	DocumentID string `json:"documentId"`
	// Subject is the optional recipient email subject; blank uses PandaDoc's default.
	Subject string `json:"subject,omitempty"`
	// Message is the optional recipient email message; blank uses PandaDoc's default.
	Message string `json:"message,omitempty"`
	// Silent disables recipient notifications when true.
	Silent bool `json:"silent,omitempty"`
}

// ListDocumentsOperation searches document status pages.
type ListDocumentsOperation struct{ client *Client }

// Definition returns the generated listDocuments contract.
func (ListDocumentsOperation) Definition() sdkgo.QueryDefinition { return ListDocumentsDefinition }

// UpdateDocumentOperation replaces a draft name.
type UpdateDocumentOperation struct{ client *Client }

// Definition returns the generated updateDocument contract.
func (UpdateDocumentOperation) Definition() sdkgo.MutationDefinition { return UpdateDocumentDefinition }

// IdempotencyKey derives a stable call key.
func (UpdateDocumentOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// DeleteDocumentOperation deletes a document.
type DeleteDocumentOperation struct{ client *Client }

// Definition returns the generated deleteDocument contract.
func (DeleteDocumentOperation) Definition() sdkgo.MutationDefinition { return DeleteDocumentDefinition }

// IdempotencyKey derives a stable call key.
func (DeleteDocumentOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// CreateDocumentOperation creates a template-based document.
type CreateDocumentOperation struct{ client *Client }

// Definition returns the generated createDocument contract.
func (CreateDocumentOperation) Definition() sdkgo.MutationDefinition { return CreateDocumentDefinition }

// IdempotencyKey derives the searchable creation marker.
func (CreateDocumentOperation) IdempotencyKey(id sdkgo.CallID, _ CreateDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// SendDocumentOperation sends an existing draft.
type SendDocumentOperation struct{ client *Client }

// Definition returns the generated sendDocument contract.
func (SendDocumentOperation) Definition() sdkgo.MutationDefinition { return SendDocumentDefinition }

// IdempotencyKey derives a stable local call identity; PandaDoc does not promise send deduplication.
func (SendDocumentOperation) IdempotencyKey(id sdkgo.CallID, _ SendDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// ListDocuments binds the document search Query.
func (client *Client) ListDocuments() ListDocumentsOperation { return ListDocumentsOperation{client} }

// UpdateDocument binds the draft name update.
func (client *Client) UpdateDocument() UpdateDocumentOperation {
	return UpdateDocumentOperation{client}
}

// DeleteDocument binds the document delete action.
func (client *Client) DeleteDocument() DeleteDocumentOperation {
	return DeleteDocumentOperation{client}
}

// CreateDocument binds template-based document creation.
func (client *Client) CreateDocument() CreateDocumentOperation {
	return CreateDocumentOperation{client}
}

// SendDocument binds draft sending.
func (client *Client) SendDocument() SendDocumentOperation { return SendDocumentOperation{client} }

// Invoke searches one bounded document page.
func (operation ListDocumentsOperation) Invoke(call sdkgo.Call, input ListDocumentsInput) sdkgo.QueryAttempt[DocumentPage] {
	if input.Count == 0 {
		input.Count = 50
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.Count < 1 || input.Count > 100 || input.Page < 1 || len(input.Query) > 256 {
		return queryDocumentFailure[DocumentPage]("listDocuments", ListDocumentsBranchDefect, sdkgo.FailureValidation, "count, page, or search query is invalid")
	}
	query := url.Values{"count": {strconv.Itoa(input.Count)}, "page": {strconv.Itoa(input.Page)}}
	if input.Query != "" {
		query.Set("q", input.Query)
	}
	response := operation.client.get(call, "/documents?"+query.Encode())
	if result, done := classifyDocumentQuery[DocumentPage](response, "listDocuments", ListDocumentsBranchProviderRejected, ListDocumentsBranchInvalidResponse, ListDocumentsBranchDefect); done {
		return result
	}
	var page DocumentPage
	if json.Unmarshal(response.body, &page) != nil || page.Results == nil {
		return queryDocumentFailure[DocumentPage]("listDocuments", ListDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "document page was malformed")
	}
	return sdkgo.NewQueryBranch(ListDocumentsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke replaces a draft document's name.
func (operation UpdateDocumentOperation) Invoke(call sdkgo.Call, input UpdateDocumentInput) sdkgo.MutationAttempt[DocumentAction] {
	if !validDocumentID(input.DocumentID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 256 {
		return mutationDocumentFailure[DocumentAction]("updateDocument", UpdateDocumentBranchDefect, sdkgo.FailureValidation, "document ID and bounded name are required")
	}
	body, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return mutationDocumentFailure[DocumentAction]("updateDocument", UpdateDocumentBranchDefect, sdkgo.FailureLocalDefect, "document update could not be encoded")
	}
	return runSafeDocumentMutation(operation.client, call, "updateDocument", http.MethodPatch, "/documents/"+url.PathEscape(input.DocumentID), body, input.DocumentID, UpdateDocumentBranchUpdated, UpdateDocumentBranchNotFound, UpdateDocumentBranchProviderRejected, false)
}

// Invoke deletes one document; an already absent document is complete.
func (operation DeleteDocumentOperation) Invoke(call sdkgo.Call, input DeleteDocumentInput) sdkgo.MutationAttempt[DocumentAction] {
	if !validDocumentID(input.DocumentID) {
		return mutationDocumentFailure[DocumentAction]("deleteDocument", DeleteDocumentBranchDefect, sdkgo.FailureValidation, "document ID is required")
	}
	return runSafeDocumentMutation(operation.client, call, "deleteDocument", http.MethodDelete, "/documents/"+url.PathEscape(input.DocumentID), nil, input.DocumentID, DeleteDocumentBranchDeleted, "", DeleteDocumentBranchProviderRejected, true)
}

// Invoke creates a document with a searchable Dex call marker.
func (operation CreateDocumentOperation) Invoke(call sdkgo.Call, input CreateDocumentInput) sdkgo.MutationAttempt[DocumentStatus] {
	if !validDocumentID(input.TemplateID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 256 || len(input.Recipients) == 0 {
		return mutationDocumentFailure[DocumentStatus]("createDocument", CreateDocumentBranchDefect, sdkgo.FailureValidation, "template ID, name, and recipients are required")
	}
	for _, recipient := range input.Recipients {
		address, err := mail.ParseAddress(recipient.Email)
		if err != nil || address.Address != recipient.Email || address.Name != "" || strings.TrimSpace(recipient.Role) == "" {
			return mutationDocumentFailure[DocumentStatus]("createDocument", CreateDocumentBranchDefect, sdkgo.FailureValidation, "each recipient needs an email and template role")
		}
	}
	body, err := json.Marshal(map[string]any{"template_uuid": input.TemplateID, "name": input.Name, "recipients": input.Recipients, "metadata": map[string]string{"dex_call_id": string(call.IdempotencyKey)}})
	if err != nil {
		return mutationDocumentFailure[DocumentStatus]("createDocument", CreateDocumentBranchDefect, sdkgo.FailureLocalDefect, "document creation could not be encoded")
	}
	return runLimitedDocumentMutation(operation.client, call, "createDocument", "/documents", body, CreateDocumentBranchCreated, "", CreateDocumentBranchProviderRejected, CreateDocumentBranchDefect, func(response providerResponse) (DocumentStatus, error) {
		var document DocumentStatus
		err := json.Unmarshal(response.body, &document)
		if err == nil && document.ID == "" {
			err = fmt.Errorf("document ID missing")
		}
		return document, err
	}, func() (DocumentStatus, bool) {
		return operation.client.readBackCreatedDocument(call, string(call.IdempotencyKey))
	})
}

// Invoke sends one draft, reading status before any possible resend.
func (operation SendDocumentOperation) Invoke(call sdkgo.Call, input SendDocumentInput) sdkgo.MutationAttempt[DocumentAction] {
	if !validDocumentID(input.DocumentID) || len(input.Subject) > 256 || len(input.Message) > 4096 {
		return mutationDocumentFailure[DocumentAction]("sendDocument", SendDocumentBranchDefect, sdkgo.FailureValidation, "document ID, subject, or message is invalid")
	}
	body, err := json.Marshal(map[string]any{"subject": input.Subject, "message": input.Message, "silent": input.Silent})
	if err != nil {
		return mutationDocumentFailure[DocumentAction]("sendDocument", SendDocumentBranchDefect, sdkgo.FailureLocalDefect, "send request could not be encoded")
	}
	return runLimitedDocumentMutation(operation.client, call, "sendDocument", "/documents/"+url.PathEscape(input.DocumentID)+"/send", body, SendDocumentBranchSent, SendDocumentBranchNotFound, SendDocumentBranchProviderRejected, SendDocumentBranchDefect, func(providerResponse) (DocumentAction, error) {
		return DocumentAction{DocumentID: input.DocumentID}, nil
	}, func() (DocumentAction, bool) { return operation.client.readBackSentDocument(call, input.DocumentID) })
}

func classifyDocumentQuery[T any](response providerResponse, operation string, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return queryDocumentFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials are invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](documentOperationFailure(operation, sdkgo.FailureTransport, "provider request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](documentOperationFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](documentOperationFailure(operation, sdkgo.FailureAvailability, "provider is unavailable"), response.retryAfter), true
	}
	if response.status != http.StatusOK {
		return queryDocumentFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request"), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryDocumentFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response was too large"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](documentOperationFailure(operation, sdkgo.FailureTransport, "provider response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func (client *Client) newDocumentRequest(call sdkgo.Call, method, path string, body []byte) (*http.Request, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, errCredential
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, errRequest
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "API-Key "+credentials.APIKey.Reveal())
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

func (client *Client) performDocumentRequest(request *http.Request) providerResponse {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: content, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func runSafeDocumentMutation(client *Client, call sdkgo.Call, operation, method, path string, body []byte, documentID string, success, notFound, rejected sdkgo.BranchID, isDelete bool) sdkgo.MutationAttempt[DocumentAction] {
	request, err := client.newDocumentRequest(call, method, path, body)
	if err != nil {
		return mutationDocumentFailure[DocumentAction](operation, sdkgo.BranchID("defect"), sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	response := client.performDocumentRequest(request)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DocumentAction](documentOperationFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DocumentAction](documentOperationFailure(operation, sdkgo.FailureAvailability, "provider update outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		if isDelete {
			return sdkgo.NewMutationBranch(success, DocumentAction{DocumentID: documentID}, nil, sdkgo.Receipt{})
		}
		return mutationDocumentFailure[DocumentAction](operation, notFound, sdkgo.FailureNotFound, "document was not found")
	}
	if response.status != http.StatusNoContent {
		return mutationDocumentFailure[DocumentAction](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the document action")
	}
	return sdkgo.NewMutationBranch(success, DocumentAction{DocumentID: documentID}, nil, sdkgo.Receipt{})
}

func (client *Client) readBackCreatedDocument(call sdkgo.Call, key string) (DocumentStatus, bool) {
	query := url.Values{"metadata_dex_call_id": {key}, "count": {"2"}, "page": {"1"}}
	response := client.get(call, "/documents?"+query.Encode())
	if response.status != http.StatusOK || response.err != nil {
		return DocumentStatus{}, false
	}
	var page DocumentPage
	if json.Unmarshal(response.body, &page) != nil || len(page.Results) != 1 || page.Results[0].ID == "" {
		return DocumentStatus{}, false
	}
	return page.Results[0], true
}

func (client *Client) readBackSentDocument(call sdkgo.Call, documentID string) (DocumentAction, bool) {
	response := client.get(call, "/documents/"+url.PathEscape(documentID))
	if response.status != http.StatusOK || response.err != nil {
		return DocumentAction{}, false
	}
	document, err := decodeDocument(response.body, documentID)
	if err != nil {
		return DocumentAction{}, false
	}
	switch document.Status {
	case "document.sent", "document.viewed", "document.completed", "document.waiting_approval", "document.approved", "document.waiting_pay", "document.paid", "document.declined", "document.voided":
		return DocumentAction{DocumentID: documentID}, true
	default:
		return DocumentAction{}, false
	}
}

func runLimitedDocumentMutation[T any](client *Client, call sdkgo.Call, operation, path string, body []byte, success, notFound, rejected, defect sdkgo.BranchID, decode func(providerResponse) (T, error), readBack func() (T, bool)) sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationDocumentFailure[T](operation, defect, sdkgo.FailureLocalDefect, "dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return uncertainDocumentMutation[T](operation)
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		if value, found := readBack(); found {
			return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return uncertainDocumentMutation[T](operation)
		}
	}
	request, err := client.newDocumentRequest(call, http.MethodPost, path, body)
	if err != nil {
		if decision.MaximumPriorSends > 0 {
			return uncertainDocumentMutation[T](operation)
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[T](documentOperationFailure(operation, sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return mutationDocumentFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	var response providerResponse
	err = fence.Dispatch(func(ctx context.Context) error {
		response = client.performDocumentRequest(request.WithContext(ctx))
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[T](documentOperationFailure(operation, sdkgo.FailureAvailability, "request was not sent before the dispatch checkpoint"), 0)
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	if errors.Is(err, errRequest) || response.status == 0 {
		if !canResend {
			return uncertainDocumentMutation[T](operation)
		}
		return sdkgo.NewMutationRetry[T](documentOperationFailure(operation, sdkgo.FailureAvailability, "provider write outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[T](documentOperationFailure(operation, sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[T](documentOperationFailure(operation, kind, "provider asked to retry the write"), response.retryAfter)
	}
	if response.status >= 500 || response.status == http.StatusRequestTimeout {
		if !canResend {
			return uncertainDocumentMutation[T](operation)
		}
		return sdkgo.NewMutationRetry[T](documentOperationFailure(operation, sdkgo.FailureAvailability, "provider write outcome is unknown"), 0)
	}
	if response.status == http.StatusNotFound && notFound != "" {
		if decision.MaximumPriorSends > 0 {
			return uncertainDocumentMutation[T](operation)
		}
		return mutationDocumentFailure[T](operation, notFound, sdkgo.FailureNotFound, "document was not found")
	}
	if response.status < 200 || response.status >= 300 {
		if decision.MaximumPriorSends > 0 {
			return uncertainDocumentMutation[T](operation)
		}
		return mutationDocumentFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the write")
	}
	if response.err != nil {
		return uncertainDocumentMutation[T](operation)
	}
	value, err := decode(response)
	if err != nil {
		return uncertainDocumentMutation[T](operation)
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

func queryDocumentFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := documentOperationFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func mutationDocumentFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := documentOperationFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func uncertainDocumentMutation[T any](operation string) sdkgo.MutationAttempt[T] {
	var zero T
	return sdkgo.NewMutationUncertain(zero, documentOperationFailure(operation, sdkgo.FailureTransport, "provider write may have succeeded"), sdkgo.Receipt{})
}

func documentOperationFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "pandadoc", Operation: operation, Message: message}
}
