// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getDocumentTextOperationID = "getDocumentText"

// GetDocumentTextInput identifies the document whose first tab is read.
type GetDocumentTextInput struct {
	// DocumentID is the Google Docs document ID, the part of a
	// docs.google.com/document/d/{documentId}/edit link after /d/.
	DocumentID string `json:"documentId"`
	// Format is markdown or plainText; blank renders Markdown.
	Format TextFormat `json:"format,omitempty"`
}

// GetDocumentTextOutput is the bounded text of a document's first tab, read
// with every pending suggestion rejected. The tooLarge branch returns the
// identity fields without Text.
type GetDocumentTextOutput struct {
	// DocumentID is the document that was read.
	DocumentID string `json:"documentId"`
	// Title is the document title shown in Google Docs and Drive.
	Title string `json:"title"`
	// RevisionID identifies the revision that Text renders. Pass it as
	// RequiredRevisionID to replaceDocumentText or appendText to write only
	// over this revision. Google returns it only to a connection with edit
	// access and guarantees it for 24 hours; it is empty for a viewer.
	RevisionID string `json:"revisionId,omitempty"`
	// Format is the rendering of Text.
	Format TextFormat `json:"format"`
	// Text is the rendered body without headers, footers, footnote text,
	// tables of contents, images, or equations.
	Text string `json:"text,omitempty"`
	// ByteCount is the UTF-8 byte length of Text.
	ByteCount int64 `json:"byteCount"`
	// HasOtherTabs reports that the document has tabs after the first, which
	// this operation does not read.
	HasOtherTabs bool `json:"hasOtherTabs,omitempty"`
}

// GetDocumentTextOperation implements the getDocumentText connector operation.
type GetDocumentTextOperation struct{ client *Client }

var getDocumentTextBranches = readBranches{
	operationID: getDocumentTextOperationID, notFound: GetDocumentTextBranchNotFound,
	tooLarge: GetDocumentTextBranchTooLarge, providerRejected: GetDocumentTextBranchProviderRejected,
	invalidResponse: GetDocumentTextBranchInvalidResponse, defect: GetDocumentTextBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetDocumentTextOperation) Definition() sdkgo.QueryDefinition { return GetDocumentTextDefinition }

// Invoke reads the document once and renders its first tab. A response above
// maxResponseBytes or text above maxTextBytes selects tooLarge.
func (operation GetDocumentTextOperation) Invoke(call sdkgo.Call, input GetDocumentTextInput) sdkgo.QueryAttempt[GetDocumentTextOutput] {
	client := operation.client
	format, isFormatValid := textFormatOrDefault(input.Format, TextFormatMarkdown)
	if !isDriveID(input.DocumentID) || !isFormatValid {
		return sdkgo.NewQueryBranch(GetDocumentTextBranchDefect, GetDocumentTextOutput{}, docsFailurePointer(sdkgo.FailureValidation, getDocumentTextOperationID, "documentId must be a Google Docs document ID and format must be markdown or plainText"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, getDocumentTextOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetDocumentTextBranchDefect, GetDocumentTextOutput{}, failure, sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, getDocumentTextBranches, googleRequest{
		method: http.MethodGet, target: client.documentURL(input.DocumentID, suggestionsPreviewWithout, readDocumentFields),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[GetDocumentTextOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID, input.DocumentID)
	document, err := decodeDocument(response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(GetDocumentTextBranchInvalidResponse, GetDocumentTextOutput{}, docsFailurePointer(sdkgo.FailureProtocol, getDocumentTextOperationID, "provider returned an invalid document"), receipt)
	}
	output := GetDocumentTextOutput{
		DocumentID: document.DocumentID, Title: document.Title, RevisionID: document.RevisionID,
		Format: format, HasOtherTabs: document.hasOtherTabs(),
	}
	text, err := renderDocumentText(document.firstTab(), format)
	if err != nil {
		return sdkgo.NewQueryBranch(GetDocumentTextBranchInvalidResponse, output, docsFailurePointer(sdkgo.FailureProtocol, getDocumentTextOperationID, "document structure could not be rendered"), receipt)
	}
	if int64(len(text)) > client.maxTextBytes {
		return sdkgo.NewQueryBranch(GetDocumentTextBranchTooLarge, output, docsFailurePointer(sdkgo.FailureResponseTooLarge, getDocumentTextOperationID, "document text exceeds the configured limit"), receipt)
	}
	output.Text = text
	output.ByteCount = int64(len(text))
	return sdkgo.NewQueryBranch(GetDocumentTextBranchRead, output, nil, receipt)
}

// textFormatOrDefault returns fallback for a blank format and reports whether format is known.
func textFormatOrDefault(format TextFormat, fallback TextFormat) (TextFormat, bool) {
	switch format {
	case "":
		return fallback, true
	case TextFormatMarkdown, TextFormatPlainText:
		return format, true
	default:
		return "", false
	}
}
