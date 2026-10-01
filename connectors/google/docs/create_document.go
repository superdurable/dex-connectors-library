// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	createDocumentOperationID = "createDocument"
	// idempotencyAppPropertyKey names the private Drive app property that records the Step's idempotency key.
	idempotencyAppPropertyKey = "dexIdempotencyKey"
	// maxIdempotencyKeyBytes keeps the app property within Google's 124-byte key-plus-value limit.
	maxIdempotencyKeyBytes    = 100
	idempotencyLookupPageSize = 10
	maxTitleBytes             = 1024
	// createdFileFields selects the Drive file fields that CreatedDocument exposes.
	createdFileFields = "id,name,mimeType,parents,webViewLink,createdTime,trashed"
)

// importMediaTypes are the media types Drive converts into a Google Doc for each initial text format.
var importMediaTypes = map[TextFormat]string{
	TextFormatPlainText: "text/plain; charset=UTF-8",
	TextFormatMarkdown:  "text/markdown; charset=UTF-8",
}

// CreateDocumentInput describes one new Google Doc.
type CreateDocumentInput struct {
	// Title is the new document's title; it must not be blank or contain line breaks.
	Title string `json:"title"`
	// ParentFolderID is the destination Drive folder ID; blank creates the
	// document in the My Drive root of the connection's account.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// InitialText is the document's first content; blank creates an empty document.
	InitialText string `json:"initialText,omitempty"`
	// InitialTextFormat is plainText or markdown; blank imports plain text.
	// Markdown asks Drive's Markdown import to create headings, lists, and tables.
	InitialTextFormat TextFormat `json:"initialTextFormat,omitempty"`
}

// CreateDocumentOutput identifies the created document.
type CreateDocumentOutput struct {
	// Document is the created document's Drive metadata.
	Document CreatedDocument `json:"document"`
	// IsFromEarlierAttempt reports that an earlier attempt of the same Step
	// execution had already created Document, so this attempt created nothing.
	IsFromEarlierAttempt bool `json:"isFromEarlierAttempt,omitempty"`
}

// CreatedDocument is the Drive metadata of a document createDocument created.
type CreatedDocument struct {
	// DocumentID is the Google Docs document ID, which is also its Drive file ID.
	DocumentID string `json:"documentId"`
	// Title is the document title.
	Title string `json:"title"`
	// Parents lists the parent folder ID; Drive allows at most one parent.
	Parents []string `json:"parents,omitempty"`
	// WebViewLink opens the document in a browser for a user who can access it.
	WebViewLink string `json:"webViewLink,omitempty"`
	// CreatedTime is the creation time reported by Drive.
	CreatedTime time.Time `json:"createdTime,omitzero"`
	// IsTrashed reports that someone moved the document to the trash after an earlier attempt created it.
	IsTrashed bool `json:"isTrashed,omitempty"`
}

// CreateDocumentOperation implements the createDocument connector operation.
type CreateDocumentOperation struct{ client *Client }

type driveFileResource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	MimeType    string    `json:"mimeType"`
	Parents     []string  `json:"parents"`
	WebViewLink string    `json:"webViewLink"`
	CreatedTime time.Time `json:"createdTime"`
	Trashed     bool      `json:"trashed"`
}

type driveFileListResponse struct {
	Files            []driveFileResource `json:"files"`
	IncompleteSearch bool                `json:"incompleteSearch"`
}

var createDocumentLookupBranches = readBranches{
	operationID: createDocumentOperationID, notFound: CreateDocumentBranchProviderRejected,
	tooLarge: CreateDocumentBranchInvalidResponse, providerRejected: CreateDocumentBranchProviderRejected,
	invalidResponse: CreateDocumentBranchInvalidResponse, defect: CreateDocumentBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (CreateDocumentOperation) Definition() sdkgo.MutationDefinition { return CreateDocumentDefinition }

// IdempotencyKey derives the private Drive app property value from the stable
// connector call ID, so every attempt of one Step execution shares it.
func (CreateDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke first looks for a document that an earlier attempt created with the
// same idempotency key and returns it. Otherwise it records a Dex heartbeat
// checkpoint and sends one Drive create that tags the document with the key.
// A later attempt that finds the checkpoint but not the document selects
// uncertain, and so does every unconfirmed outcome of the create itself.
func (operation CreateDocumentOperation) Invoke(call sdkgo.Call, input CreateDocumentInput) sdkgo.MutationAttempt[CreateDocumentOutput] {
	client := operation.client
	format, err := validateCreateDocumentInput(input, client.maxTextBytes)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, docsFailurePointer(sdkgo.FailureValidation, createDocumentOperationID, err.Error()), sdkgo.Receipt{})
	}
	idempotencyKey := string(call.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > maxIdempotencyKeyBytes {
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, docsFailurePointer(sdkgo.FailureLocalDefect, createDocumentOperationID, "idempotency key is missing or too long"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, createDocumentOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, failure, sdkgo.Receipt{})
	}
	lookupQuery := url.Values{
		"q": {earlierDocumentQuery(idempotencyKey)}, "fields": {"incompleteSearch,files(" + createdFileFields + ")"},
		"pageSize": {strconv.Itoa(idempotencyLookupPageSize)}, "spaces": {"drive"},
	}
	lookupResponse, outcome := client.sendRead(call, &credential, createDocumentLookupBranches, googleRequest{
		method: http.MethodGet, target: client.filesListURL(lookupQuery), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return mutationAttemptFromReadOutcome[CreateDocumentOutput](outcome)
	}
	lookupReceipt := client.receipt(call, lookupResponse.requestID, "")
	earlierDocument, isIncompleteLookup, err := decodeEarlierDocument(lookupResponse.body)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateDocumentBranchInvalidResponse, CreateDocumentOutput{}, docsFailurePointer(sdkgo.FailureProtocol, createDocumentOperationID, "provider returned an invalid duplicate-lookup response"), lookupReceipt)
	}
	if earlierDocument != nil {
		lookupReceipt.ProviderObjectID = earlierDocument.DocumentID
		return sdkgo.NewMutationBranch(CreateDocumentBranchCreated, CreateDocumentOutput{Document: *earlierDocument, IsFromEarlierAttempt: true}, nil, lookupReceipt)
	}
	if isIncompleteLookup {
		// An incomplete lookup cannot prove that no earlier attempt created the document.
		return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureAvailability, createDocumentOperationID, "provider did not complete the duplicate lookup"), 0)
	}
	switch claimSingleDispatch(call) {
	case singleDispatchNotRecorded:
		return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureAvailability, createDocumentOperationID, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	case singleDispatchAlreadySent:
		return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(sdkgo.FailureTransport, createDocumentOperationID,
			"an earlier attempt of this Step may have created the document, and the duplicate lookup does not show it yet"), lookupReceipt)
	}
	request, err := client.buildCreateRequest(input, format, idempotencyKey)
	if err != nil {
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, docsFailurePointer(sdkgo.FailureLocalDefect, createDocumentOperationID, "create request could not be encoded"), sdkgo.Receipt{})
	}
	response, err := client.sendRequest(call, &credential, request)
	return client.createAttemptFromResponse(call, response, err)
}

// createAttemptFromResponse retries only an outcome that proves Google created nothing.
func (client *Client) createAttemptFromResponse(call sdkgo.Call, response googleResponse, sendErr error) sdkgo.MutationAttempt[CreateDocumentOutput] {
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *googleRequestError
	if errors.As(sendErr, &requestErr) && requestErr.kind == sdkgo.FailureLocalDefect {
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, docsFailurePointer(requestErr.kind, createDocumentOperationID, requestErr.message), receipt)
	}
	if sendErr != nil {
		return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(sdkgo.FailureTransport, createDocumentOperationID, "provider create outcome is unknown"), receipt)
	}
	tokens := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	switch {
	case response.status >= 200 && response.status < 300:
		var resource driveFileResource
		if err := json.Unmarshal(response.body, &resource); err != nil {
			return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(sdkgo.FailureProtocol, createDocumentOperationID, "provider returned an invalid create response"), receipt)
		}
		document, err := convertCreatedDocument(resource)
		if err != nil {
			return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(sdkgo.FailureProtocol, createDocumentOperationID, "provider returned an invalid create response"), receipt)
		}
		receipt.ProviderObjectID = document.DocumentID
		return sdkgo.NewMutationBranch(CreateDocumentBranchCreated, CreateDocumentOutput{Document: document}, nil, receipt)
	case isRateLimitStatus(response.status, tokens):
		// Google refused the request before creating a document, so the next attempt may send it.
		releaseSingleDispatch(call)
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureRateLimit, createDocumentOperationID, "provider temporarily rejected the create"), delay)
	case response.status >= 500 || response.status == http.StatusRequestTimeout:
		return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(sdkgo.FailureAvailability, createDocumentOperationID, "provider create outcome is unknown"), receipt)
	default:
		return sdkgo.NewMutationBranch(CreateDocumentBranchProviderRejected, CreateDocumentOutput{}, docsFailurePointer(statusFailureKind(response.status, tokens), createDocumentOperationID, "provider rejected the create"), receipt)
	}
}

// buildCreateRequest creates an empty document with metadata only, or converts
// the initial text through one multipart upload.
func (client *Client) buildCreateRequest(input CreateDocumentInput, format TextFormat, idempotencyKey string) (googleRequest, error) {
	metadata := map[string]any{
		"name": input.Title, "mimeType": googleDocumentMimeType,
		"appProperties": map[string]string{idempotencyAppPropertyKey: idempotencyKey},
	}
	if input.ParentFolderID != "" {
		metadata["parents"] = []string{input.ParentFolderID}
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return googleRequest{}, err
	}
	if input.InitialText == "" {
		return googleRequest{
			method: http.MethodPost, target: client.metadataCreateURL(), body: encodedMetadata,
			contentType: "application/json; charset=UTF-8", responseLimit: client.maxResponseBytes,
		}, nil
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadataPart, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	if err != nil {
		return googleRequest{}, err
	}
	if _, err := metadataPart.Write(encodedMetadata); err != nil {
		return googleRequest{}, err
	}
	mediaPart, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {importMediaTypes[format]}})
	if err != nil {
		return googleRequest{}, err
	}
	if _, err := mediaPart.Write([]byte(input.InitialText)); err != nil {
		return googleRequest{}, err
	}
	if err := writer.Close(); err != nil {
		return googleRequest{}, err
	}
	return googleRequest{
		method: http.MethodPost, target: client.multipartCreateURL(), body: body.Bytes(),
		contentType: "multipart/related; boundary=" + writer.Boundary(), responseLimit: client.maxResponseBytes,
	}, nil
}

// validateCreateDocumentInput returns the initial text format for valid input.
func validateCreateDocumentInput(input CreateDocumentInput, maxTextBytes int64) (TextFormat, error) {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > maxTitleBytes || strings.ContainsAny(input.Title, "\n\v") {
		return "", errors.New("title must be 1 to 1024 bytes on one line")
	}
	if err := validateWriteText("title", input.Title); err != nil {
		return "", err
	}
	if input.ParentFolderID != "" && !isDriveID(input.ParentFolderID) {
		return "", errors.New("parentFolderId must be a Drive folder ID or root")
	}
	format, isFormatValid := textFormatOrDefault(input.InitialTextFormat, TextFormatPlainText)
	if !isFormatValid {
		return "", errors.New("initialTextFormat must be plainText or markdown")
	}
	if int64(len(input.InitialText)) > maxTextBytes {
		return "", errors.New("initialText exceeds the configured text limit")
	}
	if err := validateWriteText("initialText", input.InitialText); err != nil {
		return "", err
	}
	return format, nil
}

// earlierDocumentQuery finds files, including trashed ones, that carry the idempotency key.
func earlierDocumentQuery(idempotencyKey string) string {
	return "appProperties has { key=" + quoteDriveQueryString(idempotencyAppPropertyKey) +
		" and value=" + quoteDriveQueryString(idempotencyKey) + " }"
}

// decodeEarlierDocument returns the earliest-created matching document, or nil, and
// whether Drive reported the lookup as incomplete.
func decodeEarlierDocument(content []byte) (*CreatedDocument, bool, error) {
	var response driveFileListResponse
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, false, err
	}
	var earliest *CreatedDocument
	for _, resource := range response.Files {
		document, err := convertCreatedDocument(resource)
		if err != nil {
			return nil, false, err
		}
		if earliest == nil || document.CreatedTime.Before(earliest.CreatedTime) {
			earliest = &document
		}
	}
	return earliest, response.IncompleteSearch, nil
}

// convertCreatedDocument validates one untrusted Drive file resource as a Google Doc.
func convertCreatedDocument(resource driveFileResource) (CreatedDocument, error) {
	if !isDriveID(resource.ID) || resource.MimeType != googleDocumentMimeType {
		return CreatedDocument{}, errors.New("file resource is not a Google Doc with a valid ID")
	}
	return CreatedDocument{
		DocumentID: resource.ID, Title: resource.Name, Parents: resource.Parents, WebViewLink: resource.WebViewLink,
		CreatedTime: resource.CreatedTime, IsTrashed: resource.Trashed,
	}, nil
}

// quoteDriveQueryString quotes value as a Drive query string literal.
func quoteDriveQueryString(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}
