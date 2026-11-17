// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package firestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// GetDocumentInput selects a document in the default database.
type GetDocumentInput struct {
	// ProjectID is the project ID from Firestore Databases.
	ProjectID string `json:"projectId"`
	// CollectionID is the top-level collection ID.
	CollectionID string `json:"collectionId"`
	// DocumentID is the document ID within that collection.
	DocumentID string `json:"documentId"`
}

// WriteDocumentInput supplies Firestore REST Value JSON for a chosen document ID.
type WriteDocumentInput struct {
	// ProjectID is the project ID from Firestore Databases.
	ProjectID string `json:"projectId"`
	// CollectionID is the top-level collection ID.
	CollectionID string `json:"collectionId"`
	// DocumentID is a chosen document ID within that collection.
	DocumentID string `json:"documentId"`
	// FieldsJSON is a JSON object of field names to Firestore REST Value objects; never include secrets.
	FieldsJSON string `json:"fieldsJson"`
}

// FirestoreDocument contains a document identity and its Firestore REST Value fields.
type FirestoreDocument struct {
	// Name is the full document resource name.
	Name string `json:"name"`
	// FieldsJSON is the document fields as canonical JSON; never persist secret fields.
	FieldsJSON string `json:"fieldsJson,omitempty"`
}

// RunDocumentQueryInput supplies a Firestore structuredQuery JSON object.
type RunDocumentQueryInput struct {
	// ProjectID is the project ID from Firestore Databases.
	ProjectID string `json:"projectId"`
	// StructuredQueryJSON is a Firestore REST structuredQuery object with an explicit limit.
	StructuredQueryJSON string `json:"structuredQueryJson"`
}

// QueriedDocuments contains documents returned by one bounded structured query.
type QueriedDocuments struct {
	// Documents are the query matches.
	Documents []FirestoreDocument `json:"documents"`
}

// DocumentDetailsPage contains one page of full document values and a next cursor.
type DocumentDetailsPage struct {
	// Documents are the returned document values.
	Documents []FirestoreDocument `json:"documents"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListRootCollectionsInput selects a page of top-level collection IDs.
type ListRootCollectionsInput struct {
	// ProjectID is the project ID from Firestore Databases.
	ProjectID string `json:"projectId"`
	// PageSize is 1 through 100; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the opaque cursor returned by the prior page.
	PageToken string `json:"pageToken,omitempty"`
}

// CollectionsPage contains root collection IDs and the next cursor.
type CollectionsPage struct {
	// CollectionIDs are top-level collections in the default database.
	CollectionIDs []string `json:"collectionIds"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// GetDocumentOperation reads one document.
type GetDocumentOperation struct{ client *Client }

// ListDocumentDetailsOperation reads a page of document contents.
type ListDocumentDetailsOperation struct{ client *Client }

// CreateDocumentOperation creates a document at an unused ID.
type CreateDocumentOperation struct{ client *Client }

// PutDocumentOperation replaces or creates one document.
type PutDocumentOperation struct{ client *Client }

// DeleteDocumentOperation removes one document.
type DeleteDocumentOperation struct{ client *Client }

// RunDocumentQueryOperation runs a structured query.
type RunDocumentQueryOperation struct{ client *Client }

// ListRootCollectionsOperation lists top-level collections.
type ListRootCollectionsOperation struct{ client *Client }

// GetDocument binds the document read Query.
func (client *Client) GetDocument() GetDocumentOperation { return GetDocumentOperation{client} }

// ListDocumentDetails binds the document content page Query.
func (client *Client) ListDocumentDetails() ListDocumentDetailsOperation {
	return ListDocumentDetailsOperation{client}
}

// CreateDocument binds the document create Mutation.
func (client *Client) CreateDocument() CreateDocumentOperation {
	return CreateDocumentOperation{client}
}

// PutDocument binds the document replacement Mutation.
func (client *Client) PutDocument() PutDocumentOperation { return PutDocumentOperation{client} }

// DeleteDocument binds the document delete Mutation.
func (client *Client) DeleteDocument() DeleteDocumentOperation {
	return DeleteDocumentOperation{client}
}

// RunDocumentQuery binds the structured Query.
func (client *Client) RunDocumentQuery() RunDocumentQueryOperation {
	return RunDocumentQueryOperation{client}
}

// ListRootCollections binds the root collection Query.
func (client *Client) ListRootCollections() ListRootCollectionsOperation {
	return ListRootCollectionsOperation{client}
}

// Definition returns the generated document read contract.
func (GetDocumentOperation) Definition() sdkgo.QueryDefinition { return GetDocumentDefinition }

// Definition returns the generated document content page contract.
func (ListDocumentDetailsOperation) Definition() sdkgo.QueryDefinition {
	return ListDocumentDetailsDefinition
}

// Definition returns the generated document create contract.
func (CreateDocumentOperation) Definition() sdkgo.MutationDefinition { return CreateDocumentDefinition }

// Definition returns the generated document replacement contract.
func (PutDocumentOperation) Definition() sdkgo.MutationDefinition { return PutDocumentDefinition }

// Definition returns the generated document delete contract.
func (DeleteDocumentOperation) Definition() sdkgo.MutationDefinition { return DeleteDocumentDefinition }

// Definition returns the generated structured query contract.
func (RunDocumentQueryOperation) Definition() sdkgo.QueryDefinition {
	return RunDocumentQueryDefinition
}

// Definition returns the generated collection listing contract.
func (ListRootCollectionsOperation) Definition() sdkgo.QueryDefinition {
	return ListRootCollectionsDefinition
}

// IdempotencyKey identifies this create across bounded sends.
func (CreateDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ WriteDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies this absolute replacement across retries.
func (PutDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ WriteDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies this deletion across retries.
func (DeleteDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ GetDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke gets the selected document and its fields.
func (operation GetDocumentOperation) Invoke(call sdkgo.Call, input GetDocumentInput) sdkgo.QueryAttempt[FirestoreDocument] {
	path, name, ok := documentPath(input)
	if !ok {
		return firestoreQueryBranch("getDocument", "defect", FirestoreDocument{}, sdkgo.FailureValidation, "document identity is invalid")
	}
	response := operation.client.get(call, path)
	if response.status == http.StatusNotFound && response.err == nil {
		return firestoreQueryBranch("getDocument", "notFound", FirestoreDocument{}, sdkgo.FailureProviderRejection, "document does not exist")
	}
	if failure, done := classifyFirestoreResponse(response, "getDocument", http.StatusOK); done {
		return firestoreQueryDecision[FirestoreDocument](failure)
	}
	document, err := decodeFirestoreDocument(response.body, name)
	if err != nil {
		return firestoreQueryBranch("getDocument", "invalidResponse", FirestoreDocument{}, sdkgo.FailureProtocol, "Firestore returned an invalid document")
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("found"), document, nil, sdkgo.Receipt{})
}

// Invoke lists a bounded page of documents with their field values.
func (operation ListDocumentDetailsOperation) Invoke(call sdkgo.Call, input ListDocumentsInput) sdkgo.QueryAttempt[DocumentDetailsPage] {
	if !validProjectID(input.ProjectID) || !validCollectionID(input.CollectionID) || input.PageSize < 0 || input.PageSize > 100 || !validPageToken(input.PageToken) {
		return firestoreQueryBranch("listDocumentDetails", "defect", DocumentDetailsPage{}, sdkgo.FailureValidation, "document page input is invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	path := "/v1/projects/" + url.PathEscape(input.ProjectID) + "/databases/(default)/documents/" + url.PathEscape(input.CollectionID) + "?" + query.Encode()
	response := operation.client.get(call, path)
	if failure, done := classifyFirestoreResponse(response, "listDocumentDetails", http.StatusOK); done {
		return firestoreQueryDecision[DocumentDetailsPage](failure)
	}
	var wire struct {
		Documents     []json.RawMessage `json:"documents"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.Documents) > pageSize || !validPageToken(wire.NextPageToken) {
		return firestoreQueryBranch("listDocumentDetails", "invalidResponse", DocumentDetailsPage{}, sdkgo.FailureProtocol, "Firestore returned an invalid document page")
	}
	page := DocumentDetailsPage{Documents: make([]FirestoreDocument, 0, len(wire.Documents)), NextPageToken: wire.NextPageToken}
	prefix := "projects/" + input.ProjectID + "/databases/(default)/documents/" + input.CollectionID + "/"
	for _, item := range wire.Documents {
		document, err := decodeFirestoreDocument(item, "")
		if err != nil || !strings.HasPrefix(document.Name, prefix) || !validDocumentID(strings.TrimPrefix(document.Name, prefix)) {
			return firestoreQueryBranch("listDocumentDetails", "invalidResponse", DocumentDetailsPage{}, sdkgo.FailureProtocol, "Firestore returned an invalid document")
		}
		page.Documents = append(page.Documents, document)
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("listed"), page, nil, sdkgo.Receipt{})
}

// Invoke creates a document, reconciling unknown outcomes by its chosen ID and fields.
func (operation CreateDocumentOperation) Invoke(call sdkgo.Call, input WriteDocumentInput) sdkgo.MutationAttempt[FirestoreDocument] {
	path, name, body, ok := writeDocumentRequest(input)
	if !ok {
		return firestoreMutationBranch("createDocument", "defect", FirestoreDocument{}, sdkgo.FailureValidation, "document creation input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateDocumentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return firestoreMutationBranch("createDocument", "defect", FirestoreDocument{}, sdkgo.FailureLocalDefect, "create dispatch fence failed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return uncertainDocumentCreate()
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		document, confirmed, absent := operation.readBackCreatedDocument(call, input, name, body)
		if confirmed {
			return sdkgo.NewMutationBranch(sdkgo.BranchID("created"), document, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return uncertainDocumentCreate()
		}
		if !absent {
			return firestoreMutationRetry[FirestoreDocument]("createDocument", sdkgo.FailureAvailability, "creation could not be confirmed", 0)
		}
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.request(call, requestContext, http.MethodPost, path, body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return firestoreMutationRetry[FirestoreDocument]("createDocument", sdkgo.FailureAvailability, "create was not dispatched", 0)
	}
	if errors.Is(err, errCredential) {
		return firestoreMutationBranch("createDocument", "defect", FirestoreDocument{}, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return firestoreMutationRetry[FirestoreDocument]("createDocument", sdkgo.FailureAvailability, "create checkpoint failed", 0)
		}
		return firestoreMutationRetry[FirestoreDocument]("createDocument", sdkgo.FailureRateLimit, "Firestore deferred creation", response.retryAfter)
	}
	if response.status == http.StatusConflict && response.err == nil {
		if decision.MaximumPriorSends == 0 {
			return firestoreMutationBranch("createDocument", "alreadyExists", FirestoreDocument{}, sdkgo.FailureProviderRejection, "document ID already exists")
		}
		document, confirmed, _ := operation.readBackCreatedDocument(call, input, name, body)
		if confirmed {
			return sdkgo.NewMutationBranch(sdkgo.BranchID("created"), document, nil, sdkgo.Receipt{})
		}
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return uncertainDocumentCreate()
		}
		return firestoreMutationRetry[FirestoreDocument]("createDocument", sdkgo.FailureAvailability, "creation could not be confirmed after conflict", 0)
	}
	if response.err != nil || response.status >= 500 || response.status == http.StatusRequestTimeout {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return uncertainDocumentCreate()
		}
		return firestoreMutationRetry[FirestoreDocument]("createDocument", sdkgo.FailureAvailability, "creation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return firestoreMutationBranch("createDocument", "providerRejected", FirestoreDocument{}, sdkgo.FailureProviderRejection, "Firestore rejected HTTP "+strconv.Itoa(response.status))
	}
	document, err := decodeFirestoreDocument(response.body, name)
	if err != nil {
		return firestoreMutationRetry[FirestoreDocument]("createDocument", sdkgo.FailureProtocol, "Firestore returned an invalid create response", 0)
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("created"), document, nil, sdkgo.Receipt{})
}

func (operation CreateDocumentOperation) readBackCreatedDocument(call sdkgo.Call, input WriteDocumentInput, name string, body []byte) (FirestoreDocument, bool, bool) {
	getPath, _, _ := documentPath(GetDocumentInput{input.ProjectID, input.CollectionID, input.DocumentID})
	response := operation.client.get(call, getPath)
	if response.status == http.StatusNotFound && response.err == nil {
		return FirestoreDocument{}, false, true
	}
	if response.err != nil || response.status != http.StatusOK {
		return FirestoreDocument{}, false, false
	}
	document, err := decodeFirestoreDocument(response.body, name)
	if err != nil {
		return FirestoreDocument{}, false, false
	}
	var expected struct {
		Fields json.RawMessage `json:"fields"`
	}
	if json.Unmarshal(body, &expected) != nil {
		return FirestoreDocument{}, false, false
	}
	var actualFields map[string]any
	var wantedFields map[string]any
	if json.Unmarshal([]byte(document.FieldsJSON), &actualFields) != nil || json.Unmarshal(expected.Fields, &wantedFields) != nil {
		return FirestoreDocument{}, false, false
	}
	return document, reflect.DeepEqual(actualFields, wantedFields), false
}

func uncertainDocumentCreate() sdkgo.MutationAttempt[FirestoreDocument] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "google-firestore", Operation: "createDocument", Message: "document creation could not be confirmed after the send cap"}
	return sdkgo.NewMutationUncertain(FirestoreDocument{}, failure, sdkgo.Receipt{})
}

// Invoke replaces the complete field map at the selected document ID.
func (operation PutDocumentOperation) Invoke(call sdkgo.Call, input WriteDocumentInput) sdkgo.MutationAttempt[FirestoreDocument] {
	_, name, body, ok := writeDocumentRequest(input)
	if !ok {
		return firestoreMutationBranch("putDocument", "defect", FirestoreDocument{}, sdkgo.FailureValidation, "document replacement input is invalid")
	}
	patchPath, _, _ := documentPath(GetDocumentInput{input.ProjectID, input.CollectionID, input.DocumentID})
	response := operation.client.request(call, call.Context, http.MethodPatch, patchPath, body)
	if failure, done := classifyFirestoreResponse(response, "putDocument", http.StatusOK); done {
		return firestoreMutationDecision[FirestoreDocument](failure)
	}
	document, err := decodeFirestoreDocument(response.body, name)
	if err != nil {
		return firestoreMutationBranch("putDocument", "invalidResponse", FirestoreDocument{}, sdkgo.FailureProtocol, "Firestore returned an invalid document")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("written"), document, nil, sdkgo.Receipt{})
}

// Invoke deletes the selected document and accepts an already absent document.
func (operation DeleteDocumentOperation) Invoke(call sdkgo.Call, input GetDocumentInput) sdkgo.MutationAttempt[FirestoreDocument] {
	path, name, ok := documentPath(input)
	if !ok {
		return firestoreMutationBranch("deleteDocument", "defect", FirestoreDocument{}, sdkgo.FailureValidation, "document identity is invalid")
	}
	response := operation.client.request(call, call.Context, http.MethodDelete, path, nil)
	if response.status == http.StatusNoContent && response.err == nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), FirestoreDocument{Name: name}, nil, sdkgo.Receipt{})
	}
	if response.status == http.StatusNotFound && response.err == nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), FirestoreDocument{Name: name}, nil, sdkgo.Receipt{})
	}
	if failure, done := classifyFirestoreResponse(response, "deleteDocument", http.StatusOK); done {
		return firestoreMutationDecision[FirestoreDocument](failure)
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), FirestoreDocument{Name: name}, nil, sdkgo.Receipt{})
}

// Invoke runs a structured query with an explicit bounded result limit.
func (operation RunDocumentQueryOperation) Invoke(call sdkgo.Call, input RunDocumentQueryInput) sdkgo.QueryAttempt[QueriedDocuments] {
	if !validProjectID(input.ProjectID) || len(input.StructuredQueryJSON) == 0 || len(input.StructuredQueryJSON) > 65536 {
		return firestoreQueryBranch("runDocumentQuery", "defect", QueriedDocuments{}, sdkgo.FailureValidation, "query input is invalid")
	}
	var query map[string]json.RawMessage
	if json.Unmarshal([]byte(input.StructuredQueryJSON), &query) != nil || len(query) == 0 || len(query["limit"]) == 0 {
		return firestoreQueryBranch("runDocumentQuery", "defect", QueriedDocuments{}, sdkgo.FailureValidation, "structured query requires a limit")
	}
	var limit int
	if json.Unmarshal(query["limit"], &limit) != nil || limit < 1 || limit > 100 {
		return firestoreQueryBranch("runDocumentQuery", "defect", QueriedDocuments{}, sdkgo.FailureValidation, "query limit must be 1 through 100")
	}
	body, _ := json.Marshal(map[string]any{"structuredQuery": query})
	path := "/v1/projects/" + url.PathEscape(input.ProjectID) + "/databases/(default)/documents:runQuery"
	response := operation.client.request(call, call.Context, http.MethodPost, path, body)
	if failure, done := classifyFirestoreResponse(response, "runDocumentQuery", http.StatusOK); done {
		return firestoreQueryDecision[QueriedDocuments](failure)
	}
	var rows []struct {
		Document json.RawMessage `json:"document"`
	}
	if json.Unmarshal(response.body, &rows) != nil || len(rows) > limit {
		return firestoreQueryBranch("runDocumentQuery", "invalidResponse", QueriedDocuments{}, sdkgo.FailureProtocol, "Firestore returned an invalid query page")
	}
	documents := make([]FirestoreDocument, 0, len(rows))
	for _, row := range rows {
		if len(row.Document) == 0 {
			continue
		}
		document, err := decodeFirestoreDocument(row.Document, "")
		if err != nil || !strings.HasPrefix(document.Name, "projects/"+input.ProjectID+"/databases/(default)/documents/") {
			return firestoreQueryBranch("runDocumentQuery", "invalidResponse", QueriedDocuments{}, sdkgo.FailureProtocol, "Firestore returned an invalid query document")
		}
		documents = append(documents, document)
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("queried"), QueriedDocuments{Documents: documents}, nil, sdkgo.Receipt{})
}

// Invoke lists a page of root collection IDs.
func (operation ListRootCollectionsOperation) Invoke(call sdkgo.Call, input ListRootCollectionsInput) sdkgo.QueryAttempt[CollectionsPage] {
	if !validProjectID(input.ProjectID) || input.PageSize < 0 || input.PageSize > 100 || !validPageToken(input.PageToken) {
		return firestoreQueryBranch("listRootCollections", "defect", CollectionsPage{}, sdkgo.FailureValidation, "collection listing input is invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	body, _ := json.Marshal(struct {
		PageSize  int    `json:"pageSize"`
		PageToken string `json:"pageToken,omitempty"`
	}{pageSize, input.PageToken})
	path := "/v1/projects/" + url.PathEscape(input.ProjectID) + "/databases/(default)/documents:listCollectionIds"
	response := operation.client.request(call, call.Context, http.MethodPost, path, body)
	if failure, done := classifyFirestoreResponse(response, "listRootCollections", http.StatusOK); done {
		return firestoreQueryDecision[CollectionsPage](failure)
	}
	var page CollectionsPage
	if json.Unmarshal(response.body, &page) != nil || len(page.CollectionIDs) > pageSize || !validPageToken(page.NextPageToken) {
		return firestoreQueryBranch("listRootCollections", "invalidResponse", CollectionsPage{}, sdkgo.FailureProtocol, "Firestore returned an invalid collection page")
	}
	for _, collectionID := range page.CollectionIDs {
		if !validCollectionID(collectionID) {
			return firestoreQueryBranch("listRootCollections", "invalidResponse", CollectionsPage{}, sdkgo.FailureProtocol, "Firestore returned an invalid collection ID")
		}
	}
	if page.CollectionIDs == nil {
		page.CollectionIDs = []string{}
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("listed"), page, nil, sdkgo.Receipt{})
}

func documentPath(input GetDocumentInput) (string, string, bool) {
	if !validProjectID(input.ProjectID) || !validCollectionID(input.CollectionID) || !validDocumentID(input.DocumentID) {
		return "", "", false
	}
	name := "projects/" + input.ProjectID + "/databases/(default)/documents/" + input.CollectionID + "/" + input.DocumentID
	return "/v1/projects/" + url.PathEscape(input.ProjectID) + "/databases/(default)/documents/" + url.PathEscape(input.CollectionID) + "/" + url.PathEscape(input.DocumentID), name, true
}

func validDocumentID(documentID string) bool {
	if len(documentID) == 0 || len(documentID) > maxDocumentIDBytes || !utf8.ValidString(documentID) || strings.ContainsRune(documentID, '/') {
		return false
	}
	for _, character := range documentID {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func writeDocumentRequest(input WriteDocumentInput) (string, string, []byte, bool) {
	_, name, ok := documentPath(GetDocumentInput{input.ProjectID, input.CollectionID, input.DocumentID})
	if !ok || len(input.FieldsJSON) == 0 || len(input.FieldsJSON) > 1048576 {
		return "", "", nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(input.FieldsJSON), &fields) != nil || len(fields) == 0 {
		return "", "", nil, false
	}
	for fieldName, fieldValue := range fields {
		if fieldName == "" || len(fieldName) > 1500 || len(fieldValue) == 0 || fieldValue[0] != '{' {
			return "", "", nil, false
		}
	}
	body, err := json.Marshal(struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}{fields})
	if err != nil {
		return "", "", nil, false
	}
	path := "/v1/projects/" + url.PathEscape(input.ProjectID) + "/databases/(default)/documents/" + url.PathEscape(input.CollectionID) + "?documentId=" + url.QueryEscape(input.DocumentID)
	return path, name, body, true
}

func decodeFirestoreDocument(body []byte, expectedName string) (FirestoreDocument, error) {
	var wire struct {
		Name   string          `json:"name"`
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return FirestoreDocument{}, err
	}
	if wire.Name == "" || (expectedName != "" && wire.Name != expectedName) || len(wire.Fields) == 0 || wire.Fields[0] != '{' {
		return FirestoreDocument{}, fmt.Errorf("invalid document identity or fields")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, wire.Fields); err != nil {
		return FirestoreDocument{}, err
	}
	return FirestoreDocument{Name: wire.Name, FieldsJSON: compact.String()}, nil
}

type firestoreFailureDecision struct {
	operation string
	branch    sdkgo.BranchID
	kind      sdkgo.FailureKind
	message   string
	retry     bool
	delay     time.Duration
}

func classifyFirestoreResponse(response providerResponse, operation string, successStatus int) (firestoreFailureDecision, bool) {
	if errors.Is(response.err, errCredential) {
		return firestoreFailureDecision{operation, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable", false, 0}, true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return firestoreFailureDecision{operation, "invalidResponse", sdkgo.FailureResponseTooLarge, "Firestore response exceeded the configured limit", false, 0}, true
	}
	if response.err != nil {
		return firestoreFailureDecision{operation, "", sdkgo.FailureTransport, "Firestore request failed", true, 0}, true
	}
	if response.status == http.StatusTooManyRequests {
		return firestoreFailureDecision{operation, "", sdkgo.FailureRateLimit, "Firestore rate limited the request", true, response.retryAfter}, true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return firestoreFailureDecision{operation, "", sdkgo.FailureAvailability, "Firestore is temporarily unavailable", true, response.retryAfter}, true
	}
	if response.status != successStatus {
		return firestoreFailureDecision{operation, "providerRejected", sdkgo.FailureProviderRejection, "Firestore rejected HTTP " + strconv.Itoa(response.status), false, 0}, true
	}
	return firestoreFailureDecision{}, false
}

func firestoreQueryDecision[T any](decision firestoreFailureDecision) sdkgo.QueryAttempt[T] {
	if decision.retry {
		return firestoreQueryRetry[T](decision.operation, decision.kind, decision.message, decision.delay)
	}
	return firestoreQueryBranch(decision.operation, decision.branch, *new(T), decision.kind, decision.message)
}
func firestoreMutationDecision[T any](decision firestoreFailureDecision) sdkgo.MutationAttempt[T] {
	if decision.retry {
		return firestoreMutationRetry[T](decision.operation, decision.kind, decision.message, decision.delay)
	}
	return firestoreMutationBranch(decision.operation, decision.branch, *new(T), decision.kind, decision.message)
}
func firestoreQueryBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-firestore", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func firestoreQueryRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "google-firestore", Operation: operation, Message: message}, delay)
}
func firestoreMutationBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-firestore", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
func firestoreMutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "google-firestore", Operation: operation, Message: message}, delay)
}
