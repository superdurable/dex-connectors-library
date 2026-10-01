// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const appendTextOperationID = "appendText"

// AppendTextInput describes text appended to the end of a document's first tab.
type AppendTextInput struct {
	// DocumentID is the Google Docs document ID.
	DocumentID string `json:"documentId"`
	// RequiredRevisionID is the revision the text is appended to, normally the
	// RevisionID of a getDocumentText or replaceDocumentText Step. Google
	// appends only while the document is still at this revision.
	RequiredRevisionID string `json:"requiredRevisionId"`
	// Text is appended as one or more plain normal-style paragraphs, one per
	// line, after the body's last paragraph. It must not be blank.
	Text string `json:"text"`
}

// AppendTextOutput reports the document after the append.
type AppendTextOutput struct {
	// DocumentID is the document that was changed.
	DocumentID string `json:"documentId"`
	// RevisionID is the document's revision after the append; on
	// revisionChanged it is the current revision that blocked the write. It is
	// empty only when Google omits it from a successful update response.
	RevisionID string `json:"revisionId,omitempty"`
	// WasAlreadyApplied reports that this attempt wrote nothing because the
	// changed document already ends with the text, normally appended by an
	// earlier attempt of the same Step execution whose response was lost or slow.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// AppendTextOperation implements the appendText connector operation.
type AppendTextOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (AppendTextOperation) Definition() sdkgo.MutationDefinition { return AppendTextDefinition }

// IdempotencyKey derives the receipt key from the stable connector call ID. The
// required revision, not this key, keeps a repeated dispatch from appending twice.
func (AppendTextOperation) IdempotencyKey(callID sdkgo.CallID, _ AppendTextInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the document and, only at RequiredRevisionID, inserts the text
// before the body's final newline in one batch guarded by that revision.
func (operation AppendTextOperation) Invoke(call sdkgo.Call, input AppendTextInput) sdkgo.MutationAttempt[AppendTextOutput] {
	if err := validateAppendTextInput(input, operation.client.maxTextBytes); err != nil {
		return sdkgo.NewMutationBranch(AppendTextBranchDefect, AppendTextOutput{}, docsFailurePointer(sdkgo.FailureValidation, appendTextOperationID, err.Error()), sdkgo.Receipt{})
	}
	return runGuardedWrite[AppendTextOutput](operation.client, call, guardedWriteTarget{
		operationID: appendTextOperationID, documentID: input.DocumentID, requiredRevisionID: input.RequiredRevisionID,
		applied: AppendTextBranchAppended, revisionChanged: AppendTextBranchRevisionChanged,
		notFound: AppendTextBranchNotFound, providerRejected: AppendTextBranchProviderRejected,
		invalidResponse: AppendTextBranchInvalidResponse, defect: AppendTextBranchDefect,
	}, documentTextAppend{input: input})
}

// documentTextAppend is the guarded change of one appendText call.
type documentTextAppend struct {
	input AppendTextInput
}

// requestsAtRequiredRevision starts a new paragraph unless the body is empty.
func (textAppend documentTextAppend) requestsAtRequiredRevision(
	document documentResource,
	bodyText string,
) ([]documentRequest, *sdkgo.MutationAttempt[AppendTextOutput]) {
	tabID := document.firstTab().TabProperties.TabID
	separator := "\n"
	if bodyText == "\n" {
		separator = ""
	}
	insertIndex := document.bodyEndIndex() - 1
	appendedStart := insertIndex + utf16Length(separator)
	requests := []documentRequest{{InsertText: &insertTextRequest{
		Text: separator + textAppend.input.Text, Location: documentLocation{Index: insertIndex, TabID: tabID},
	}}}
	return append(requests, plainParagraphRequests(tabID, appendedStart, appendedStart+utf16Length(textAppend.input.Text))...), nil
}

// isHeldBy reports a body whose last paragraphs are exactly the appended text.
func (textAppend documentTextAppend) isHeldBy(bodyText string) bool {
	appended := textAppend.input.Text + "\n"
	return bodyText == appended || strings.HasSuffix(bodyText, "\n"+appended)
}

func (textAppend documentTextAppend) output(revisionID string, _ []batchUpdateReply, wasAlreadyApplied bool) AppendTextOutput {
	return AppendTextOutput{DocumentID: textAppend.input.DocumentID, RevisionID: revisionID, WasAlreadyApplied: wasAlreadyApplied}
}

func validateAppendTextInput(input AppendTextInput, maxTextBytes int64) error {
	if err := validateGuardedWriteTarget(input.DocumentID, input.RequiredRevisionID); err != nil {
		return err
	}
	if strings.TrimSpace(input.Text) == "" {
		return errors.New("text must not be blank")
	}
	if int64(len(input.Text)) > maxTextBytes {
		return errors.New("text exceeds the configured text limit")
	}
	return validateWriteText("text", input.Text)
}
