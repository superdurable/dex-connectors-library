// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"encoding/json"
	"errors"
	"net/http"
	"unicode"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	maxRevisionIDBytes = 1024
	// normalTextStyle is the named paragraph style that written paragraphs receive.
	normalTextStyle = "NORMAL_TEXT"
	// everyStyleField resets every style property that a request leaves unset.
	everyStyleField = "*"
)

// guardedWriteTarget names the document, revision, and branches of one guarded write.
type guardedWriteTarget struct {
	operationID        string
	documentID         string
	requiredRevisionID string
	applied            sdkgo.BranchID
	revisionChanged    sdkgo.BranchID
	notFound           sdkgo.BranchID
	providerRejected   sdkgo.BranchID
	invalidResponse    sdkgo.BranchID
	defect             sdkgo.BranchID
}

// guardedChange is the operation-specific part of a guarded write.
type guardedChange[OUT any] interface {
	// requestsAtRequiredRevision builds the batch; a non-nil attempt ends the operation without writing.
	requestsAtRequiredRevision(document documentResource, bodyText string) ([]documentRequest, *sdkgo.MutationAttempt[OUT])
	// isHeldBy reports whether a changed document's body text already holds the requested text.
	isHeldBy(bodyText string) bool
	// output builds the Result value; replies is empty unless this attempt wrote.
	output(revisionID string, replies []batchUpdateReply, wasAlreadyApplied bool) OUT
}

// batchUpdateRequest is the documents.batchUpdate body. Google applies it
// atomically and only while the document is at RequiredRevisionID.
type batchUpdateRequest struct {
	Requests     []documentRequest `json:"requests"`
	WriteControl writeControl      `json:"writeControl"`
}

type writeControl struct {
	RequiredRevisionID string `json:"requiredRevisionId"`
}

type documentRequest struct {
	ReplaceAllText         *replaceAllTextRequest       `json:"replaceAllText,omitempty"`
	DeleteContentRange     *rangeRequest                `json:"deleteContentRange,omitempty"`
	InsertText             *insertTextRequest           `json:"insertText,omitempty"`
	DeleteParagraphBullets *rangeRequest                `json:"deleteParagraphBullets,omitempty"`
	UpdateParagraphStyle   *updateParagraphStyleRequest `json:"updateParagraphStyle,omitempty"`
	UpdateTextStyle        *updateTextStyleRequest      `json:"updateTextStyle,omitempty"`
}

type replaceAllTextRequest struct {
	ReplaceText  string                 `json:"replaceText"`
	ContainsText substringMatchCriteria `json:"containsText"`
	TabsCriteria tabsCriteria           `json:"tabsCriteria"`
}

type substringMatchCriteria struct {
	Text      string `json:"text"`
	MatchCase bool   `json:"matchCase"`
}

type tabsCriteria struct {
	TabIDs []string `json:"tabIds"`
}

type insertTextRequest struct {
	Text     string           `json:"text"`
	Location documentLocation `json:"location"`
}

type documentLocation struct {
	Index int    `json:"index"`
	TabID string `json:"tabId"`
}

type rangeRequest struct {
	Range documentRange `json:"range"`
}

type documentRange struct {
	StartIndex int    `json:"startIndex"`
	EndIndex   int    `json:"endIndex"`
	TabID      string `json:"tabId"`
}

type updateParagraphStyleRequest struct {
	Range          documentRange          `json:"range"`
	ParagraphStyle paragraphStyleResource `json:"paragraphStyle"`
	Fields         string                 `json:"fields"`
}

type updateTextStyleRequest struct {
	Range     documentRange `json:"range"`
	TextStyle struct{}      `json:"textStyle"`
	Fields    string        `json:"fields"`
}

type batchUpdateResponse struct {
	DocumentID   string             `json:"documentId"`
	Replies      []batchUpdateReply `json:"replies"`
	WriteControl writeControl       `json:"writeControl"`
}

type batchUpdateReply struct {
	ReplaceAllText *replaceAllTextReply `json:"replaceAllText"`
}

type replaceAllTextReply struct {
	OccurrencesChanged int `json:"occurrencesChanged"`
}

// runGuardedWrite writes one batch only at the required revision, so a repeated dispatch can never apply it twice.
func runGuardedWrite[OUT any](client *Client, call sdkgo.Call, target guardedWriteTarget, change guardedChange[OUT]) sdkgo.MutationAttempt[OUT] {
	credential, failure := client.resolveCredential(call, target.operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(target.defect, *new(OUT), failure, sdkgo.Receipt{})
	}
	document, bodyText, receipt, attempt := readDocumentForWrite[OUT](client, call, &credential, target)
	if attempt != nil {
		return *attempt
	}
	if document.RevisionID != target.requiredRevisionID {
		return changedDocumentAttempt(target, change, document.RevisionID, bodyText, receipt)
	}
	requests, attempt := change.requestsAtRequiredRevision(document, bodyText)
	if attempt != nil {
		return *attempt
	}
	if len(requests) == 0 {
		return sdkgo.NewMutationBranch(target.applied, change.output(document.RevisionID, nil, true), nil, receipt)
	}
	encoded, err := json.Marshal(batchUpdateRequest{Requests: requests, WriteControl: writeControl{RequiredRevisionID: target.requiredRevisionID}})
	if err != nil {
		return sdkgo.NewMutationBranch(target.defect, *new(OUT), docsFailurePointer(sdkgo.FailureLocalDefect, target.operationID, "update request could not be encoded"), sdkgo.Receipt{})
	}
	response, err := client.sendRequest(call, &credential, googleRequest{
		method: http.MethodPost, target: client.batchUpdateURL(target.documentID), body: encoded,
		contentType: "application/json; charset=UTF-8", responseLimit: client.maxResponseBytes,
	})
	return guardedWriteAttemptFromResponse(client, call, &credential, target, change, response, err)
}

// guardedWriteAttemptFromResponse classifies the batch response. A 400 is
// ambiguous until a fresh read shows whether the revision moved.
func guardedWriteAttemptFromResponse[OUT any](
	client *Client,
	call sdkgo.Call,
	credential *Credentials,
	target guardedWriteTarget,
	change guardedChange[OUT],
	response googleResponse,
	sendErr error,
) sdkgo.MutationAttempt[OUT] {
	receipt := client.receipt(call, response.requestID, target.documentID)
	var requestErr *googleRequestError
	if errors.As(sendErr, &requestErr) && requestErr.kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewMutationBranch(target.defect, *new(OUT), docsFailurePointer(requestErr.kind, target.operationID, requestErr.message), receipt)
	}
	if sendErr != nil {
		return sdkgo.NewMutationRetry[OUT](docsFailure(sdkgo.FailureTransport, target.operationID, "provider update outcome is unknown; the next attempt reads the revision"), 0)
	}
	tokens := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	switch {
	case response.status >= 200 && response.status < 300:
		var decoded batchUpdateResponse
		if err := json.Unmarshal(response.body, &decoded); err != nil {
			return sdkgo.NewMutationRetry[OUT](docsFailure(sdkgo.FailureProtocol, target.operationID, "provider returned an invalid update response; the next attempt reads the revision"), 0)
		}
		return sdkgo.NewMutationBranch(target.applied, change.output(decoded.WriteControl.RequiredRevisionID, decoded.Replies, false), nil, receipt)
	case isRetryableStatus(response.status, tokens):
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[OUT](docsFailure(statusFailureKind(response.status, tokens), target.operationID, "provider temporarily rejected the update"), delay)
	case response.status == http.StatusBadRequest:
		document, bodyText, rereadReceipt, attempt := readDocumentForWrite[OUT](client, call, credential, target)
		if attempt != nil {
			return *attempt
		}
		if document.RevisionID == target.requiredRevisionID {
			return sdkgo.NewMutationBranch(target.providerRejected, *new(OUT), docsFailurePointer(sdkgo.FailureProviderRejection, target.operationID, "provider rejected the update at the required revision"), receipt)
		}
		return changedDocumentAttempt(target, change, document.RevisionID, bodyText, rereadReceipt)
	case response.status == http.StatusNotFound:
		return sdkgo.NewMutationBranch(target.notFound, *new(OUT), docsFailurePointer(sdkgo.FailureNotFound, target.operationID, "document was not found or is not visible to the connection"), receipt)
	default:
		return sdkgo.NewMutationBranch(target.providerRejected, *new(OUT), docsFailurePointer(statusFailureKind(response.status, tokens), target.operationID, "provider rejected the update"), receipt)
	}
}

// readDocumentForWrite reads the first tab with suggestions inline, the view
// Google requires for edit indexes. A non-nil attempt ends the operation.
func readDocumentForWrite[OUT any](
	client *Client,
	call sdkgo.Call,
	credential *Credentials,
	target guardedWriteTarget,
) (documentResource, string, sdkgo.Receipt, *sdkgo.MutationAttempt[OUT]) {
	branches := readBranches{
		operationID: target.operationID, notFound: target.notFound, tooLarge: target.invalidResponse,
		providerRejected: target.providerRejected, invalidResponse: target.invalidResponse, defect: target.defect,
	}
	response, outcome := client.sendRead(call, credential, branches, googleRequest{
		method: http.MethodGet, target: client.documentURL(target.documentID, suggestionsInline, writeDocumentFields),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		attempt := mutationAttemptFromReadOutcome[OUT](outcome)
		return documentResource{}, "", sdkgo.Receipt{}, &attempt
	}
	receipt := client.receipt(call, response.requestID, target.documentID)
	document, err := decodeDocument(response.body)
	if err != nil {
		attempt := sdkgo.NewMutationBranch(target.invalidResponse, *new(OUT), docsFailurePointer(sdkgo.FailureProtocol, target.operationID, "provider returned an invalid document"), receipt)
		return documentResource{}, "", receipt, &attempt
	}
	if document.RevisionID == "" {
		// Google returns a revision ID only to a connection with edit access.
		attempt := sdkgo.NewMutationBranch(target.providerRejected, *new(OUT), docsFailurePointer(sdkgo.FailureAuthorization, target.operationID, "connection cannot edit the document"), receipt)
		return documentResource{}, "", receipt, &attempt
	}
	bodyText, err := document.bodyText()
	if err != nil {
		attempt := sdkgo.NewMutationBranch(target.invalidResponse, *new(OUT), docsFailurePointer(sdkgo.FailureProtocol, target.operationID, "document structure could not be read"), receipt)
		return documentResource{}, "", receipt, &attempt
	}
	return document, bodyText, receipt, nil
}

// changedDocumentAttempt writes nothing: the text is already there, or another change won.
func changedDocumentAttempt[OUT any](
	target guardedWriteTarget,
	change guardedChange[OUT],
	revisionID string,
	bodyText string,
	receipt sdkgo.Receipt,
) sdkgo.MutationAttempt[OUT] {
	if change.isHeldBy(bodyText) {
		return sdkgo.NewMutationBranch(target.applied, change.output(revisionID, nil, true), nil, receipt)
	}
	return sdkgo.NewMutationBranch(target.revisionChanged, change.output(revisionID, nil, false),
		docsFailurePointer(sdkgo.FailureConflict, target.operationID, "document changed after the required revision; nothing was written"), receipt)
}

// plainParagraphRequests give written paragraphs the normal style without
// bullets or text styling, instead of inheriting the neighboring paragraph's.
func plainParagraphRequests(tabID string, startIndex int, endIndex int) []documentRequest {
	writtenRange := documentRange{StartIndex: startIndex, EndIndex: endIndex, TabID: tabID}
	return []documentRequest{
		{DeleteParagraphBullets: &rangeRequest{Range: writtenRange}},
		{UpdateParagraphStyle: &updateParagraphStyleRequest{
			Range: writtenRange, ParagraphStyle: paragraphStyleResource{NamedStyleType: normalTextStyle}, Fields: everyStyleField,
		}},
		{UpdateTextStyle: &updateTextStyleRequest{Range: writtenRange, Fields: everyStyleField}},
	}
}

// validateGuardedWriteTarget checks the identity every guarded write requires.
func validateGuardedWriteTarget(documentID string, requiredRevisionID string) error {
	if !isDriveID(documentID) {
		return errors.New("documentId must be a Google Docs document ID")
	}
	if requiredRevisionID == "" || len(requiredRevisionID) > maxRevisionIDBytes {
		return errors.New("requiredRevisionId must be the revisionId a getDocumentText Step returned")
	}
	for _, character := range requiredRevisionID {
		if !unicode.IsPrint(character) || unicode.IsSpace(character) {
			return errors.New("requiredRevisionId must be the revisionId a getDocumentText Step returned")
		}
	}
	return nil
}

// bodyStartIndex is where the first tab's body text starts, after its leading section break.
func bodyStartIndex(document documentResource) int {
	first := document.firstTab().DocumentTab.Body.Content[0]
	if first.SectionBreak != nil {
		return first.EndIndex
	}
	return first.StartIndex
}
