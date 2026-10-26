// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"bytes"
	"context"
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
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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

type createDocumentDispatch struct {
	client     *Client
	call       sdkgo.Call
	credential Credentials
	request    googleRequest
	response   googleResponse
	sendErr    error
}

func (dispatch *createDocumentDispatch) send(ctx context.Context) error {
	dispatch.request.requestContext = ctx
	dispatch.request.isSingleDispatch = true
	dispatch.response, dispatch.sendErr = dispatch.client.sendRequest(dispatch.call, &dispatch.credential, dispatch.request)
	return dispatch.sendErr
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

// Invoke checks the single-dispatch fence before credentials and Drive lookup.
// A possibly accepted create is reconciled by its private Drive app property.
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
	fence, err := singledispatch.NewFence(call, 30*time.Second)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, docsFailurePointer(sdkgo.FailureLocalDefect, createDocumentOperationID, "single-dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(sdkgo.FailureTransport, createDocumentOperationID, "Dex dispatch checkpoint cannot be trusted"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, createDocumentOperationID)
	if failure != nil {
		if decision.Kind == singledispatch.SendAllowed {
			return retryCreateBeforeSend(fence, sdkgo.FailureAuthentication, "connection credentials are unavailable")
		}
		return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureAuthentication, createDocumentOperationID, "connection credentials are unavailable for duplicate lookup"), 0)
	}
	return client.createDocumentWithFence(call, input, format, idempotencyKey, credential, fence, decision)
}

func (client *Client) createDocumentWithFence(call sdkgo.Call, input CreateDocumentInput, format TextFormat, idempotencyKey string, credential Credentials, fence *singledispatch.Fence, decision singledispatch.Decision) sdkgo.MutationAttempt[CreateDocumentOutput] {
	lookupContext, cancelLookup := boundedCreateLookupContext(call)
	defer cancelLookup()
	lookupQuery := url.Values{
		"q": {earlierDocumentQuery(idempotencyKey)}, "fields": {"incompleteSearch,files(" + createdFileFields + ")"},
		"pageSize": {strconv.Itoa(idempotencyLookupPageSize)}, "spaces": {"drive"},
	}
	lookupResponse, outcome := client.sendRead(call, &credential, createDocumentLookupBranches, googleRequest{
		method: http.MethodGet, target: client.filesListURL(lookupQuery), responseLimit: client.maxResponseBytes, requestContext: lookupContext,
	})
	if outcome != nil {
		if decision.Kind == singledispatch.SendAllowed && outcome.branch == "" {
			return retryCreateBeforeSend(fence, sdkgo.FailureAvailability, "duplicate lookup is unavailable")
		}
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
		if decision.Kind == singledispatch.SendAllowed {
			return retryCreateBeforeSend(fence, sdkgo.FailureAvailability, "provider did not complete the duplicate lookup")
		}
		return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureAvailability, createDocumentOperationID, "provider did not complete the duplicate lookup"), 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(sdkgo.FailureTransport, createDocumentOperationID,
			"an earlier attempt of this Step may have created the document, and the duplicate lookup does not show it yet"), lookupReceipt)
	}
	request, err := client.buildCreateRequest(input, format, idempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, docsFailurePointer(sdkgo.FailureLocalDefect, createDocumentOperationID, "create request could not be encoded"), sdkgo.Receipt{})
	}
	dispatch := &createDocumentDispatch{client: client, call: call, credential: credential, request: request}
	err = fence.Dispatch(dispatch.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureAvailability, createDocumentOperationID, "Dex could not record a dispatch claim or the attempt has too little time"), 0)
	}
	return client.createAttemptFromResponse(call, fence, &dispatch.credential, idempotencyKey, dispatch.response, err)
}

func retryCreateBeforeSend(fence *singledispatch.Fence, failureKind sdkgo.FailureKind, reason string) sdkgo.MutationAttempt[CreateDocumentOutput] {
	if err := fence.RecordNotSent(); err != nil {
		reason += "; Dex could not record the no-send checkpoint"
	}
	return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(failureKind, createDocumentOperationID, reason), 0)
}

// createAttemptFromResponse reconciles accepted or ambiguous replies before permitting another send.
func (client *Client) createAttemptFromResponse(call sdkgo.Call, fence *singledispatch.Fence, credential *Credentials, idempotencyKey string, response googleResponse, sendErr error) sdkgo.MutationAttempt[CreateDocumentOutput] {
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *googleRequestError
	if errors.As(sendErr, &requestErr) && requestErr.kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewMutationBranch(CreateDocumentBranchDefect, CreateDocumentOutput{}, docsFailurePointer(requestErr.kind, createDocumentOperationID, requestErr.message), receipt)
	}
	tokens := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	switch {
	case response.status >= 200 && response.status < 300:
		var resource driveFileResource
		if sendErr == nil && json.Unmarshal(response.body, &resource) == nil {
			if document, err := convertCreatedDocument(resource); err == nil {
				receipt.ProviderObjectID = document.DocumentID
				_ = fence.RecordProviderID(document.DocumentID)
				return sdkgo.NewMutationBranch(CreateDocumentBranchCreated, CreateDocumentOutput{Document: document}, nil, receipt)
			}
		}
		return client.reconcileCreateAfterSend(call, fence, credential, idempotencyKey, receipt, sdkgo.FailureProtocol, "provider returned an unusable accepted create response")
	case isRateLimitStatus(response.status, tokens) || response.status == http.StatusServiceUnavailable:
		if document, isLookupComplete := client.findCreatedDocumentAfterSend(call, credential, idempotencyKey); document != nil {
			receipt.ProviderObjectID = document.DocumentID
			_ = fence.RecordProviderID(document.DocumentID)
			return sdkgo.NewMutationBranch(CreateDocumentBranchCreated, CreateDocumentOutput{Document: *document}, nil, receipt)
		} else if !isLookupComplete {
			return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureAvailability, createDocumentOperationID, "duplicate lookup could not confirm the create outcome"), 0)
		}
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureAvailability, createDocumentOperationID, "Dex could not record safe resend after provider rejection"), 0)
		}
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(statusFailureKind(response.status, tokens), createDocumentOperationID, "provider temporarily rejected the create"), delay)
	case sendErr != nil:
		if errors.As(sendErr, &requestErr) && requestErr.isProvablyUndispatched {
			if err := fence.RecordCanResend(); err != nil {
				return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureTransport, createDocumentOperationID, "Dex could not record a proven no-send"), 0)
			}
			return sdkgo.NewMutationRetry[CreateDocumentOutput](docsFailure(sdkgo.FailureTransport, createDocumentOperationID, "provider connection failed before the create was sent"), 0)
		}
		return client.reconcileCreateAfterSend(call, fence, credential, idempotencyKey, receipt, sdkgo.FailureTransport, "provider create outcome is unknown")
	case response.status >= 500 || response.status == http.StatusRequestTimeout || response.status == http.StatusConflict:
		return client.reconcileCreateAfterSend(call, fence, credential, idempotencyKey, receipt, sdkgo.FailureAvailability, "provider create outcome is unknown")
	default:
		return sdkgo.NewMutationBranch(CreateDocumentBranchProviderRejected, CreateDocumentOutput{}, docsFailurePointer(statusFailureKind(response.status, tokens), createDocumentOperationID, "provider rejected the create"), receipt)
	}
}

func (client *Client) reconcileCreateAfterSend(call sdkgo.Call, fence *singledispatch.Fence, credential *Credentials, idempotencyKey string, receipt sdkgo.Receipt, failureKind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreateDocumentOutput] {
	if document, _ := client.findCreatedDocumentAfterSend(call, credential, idempotencyKey); document != nil {
		receipt.ProviderObjectID = document.DocumentID
		_ = fence.RecordProviderID(document.DocumentID)
		return sdkgo.NewMutationBranch(CreateDocumentBranchCreated, CreateDocumentOutput{Document: *document}, nil, receipt)
	}
	return sdkgo.NewMutationUncertain(CreateDocumentOutput{}, docsFailure(failureKind, createDocumentOperationID, message), receipt)
}

func (client *Client) findCreatedDocumentAfterSend(call sdkgo.Call, credential *Credentials, idempotencyKey string) (*CreatedDocument, bool) {
	lookupContext, cancelLookup := boundedCreateLookupContext(call)
	defer cancelLookup()
	query := url.Values{"q": {earlierDocumentQuery(idempotencyKey)}, "fields": {"incompleteSearch,files(" + createdFileFields + ")"}, "pageSize": {strconv.Itoa(idempotencyLookupPageSize)}, "spaces": {"drive"}}
	response, outcome := client.sendRead(call, credential, createDocumentLookupBranches, googleRequest{
		method: http.MethodGet, target: client.filesListURL(query), responseLimit: client.maxResponseBytes, requestContext: lookupContext,
	})
	if outcome != nil {
		return nil, false
	}
	document, incomplete, err := decodeEarlierDocument(response.body)
	if err != nil || incomplete {
		return nil, false
	}
	return document, true
}

func boundedCreateLookupContext(call sdkgo.Call) (context.Context, context.CancelFunc) {
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		margin := min(time.Second, max(time.Millisecond, time.Until(deadline)/10))
		return context.WithDeadline(call.Context, deadline.Add(-margin))
	}
	return context.WithTimeout(call.Context, defaultRequestTimeout)
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
