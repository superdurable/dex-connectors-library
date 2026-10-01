// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	replaceDocumentTextOperationID = "replaceDocumentText"
	maxPlaceholders                = 50
	maxPlaceholderBytes            = 256
)

// ReplaceTarget selects what replaceDocumentText replaces.
type ReplaceTarget string

const (
	// ReplaceTargetWholeBody replaces the first tab's entire body with Text as
	// plain normal-style paragraphs, one per line.
	ReplaceTargetWholeBody ReplaceTarget = "wholeBody"
	// ReplaceTargetPlaceholders replaces every case-sensitive occurrence of each
	// placeholder, keeping the surrounding formatting.
	ReplaceTargetPlaceholders ReplaceTarget = "placeholders"
)

// ReplaceDocumentTextInput describes one guarded replacement in a document's first tab.
type ReplaceDocumentTextInput struct {
	// DocumentID is the Google Docs document ID.
	DocumentID string `json:"documentId"`
	// RequiredRevisionID is the revision the replacement applies to, normally
	// the RevisionID of a getDocumentText Step. Google applies the change only
	// while the document is still at this revision.
	RequiredRevisionID string `json:"requiredRevisionId"`
	// Target is wholeBody or placeholders.
	Target ReplaceTarget `json:"target"`
	// Text is the new body text for wholeBody; blank empties the body. It is
	// inserted literally, so Markdown markup is not interpreted.
	Text string `json:"text,omitempty"`
	// Placeholders lists the placeholders to replace for placeholders, such as
	// {{effectiveDate}}; every one must occur in the body at RequiredRevisionID.
	Placeholders []PlaceholderReplacement `json:"placeholders,omitempty"`
}

// PlaceholderReplacement is one named text placeholder and its replacement.
type PlaceholderReplacement struct {
	// Placeholder is the exact, case-sensitive text to replace, such as
	// {{effectiveDate}}. It is 1 to 256 bytes on one line.
	Placeholder string `json:"placeholder"`
	// Text replaces every occurrence; blank removes the placeholder. It cannot
	// contain any requested placeholder.
	Text string `json:"text"`
}

// ReplaceDocumentTextOutput reports the document after the replacement.
type ReplaceDocumentTextOutput struct {
	// DocumentID is the document that was changed.
	DocumentID string `json:"documentId"`
	// RevisionID is the document's revision after the replacement; on
	// revisionChanged it is the current revision that blocked the write. It is
	// empty only when Google omits it from a successful update response.
	RevisionID string `json:"revisionId,omitempty"`
	// Replacements reports Google's occurrence count per placeholder when this attempt wrote.
	Replacements []PlaceholderReplacementResult `json:"replacements,omitempty"`
	// MissingPlaceholders lists the placeholders absent on placeholderNotFound.
	MissingPlaceholders []string `json:"missingPlaceholders,omitempty"`
	// WasAlreadyApplied reports that this attempt wrote nothing because the
	// document already held the requested text, normally written by an earlier
	// attempt of the same Step execution whose response was lost or slow.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// PlaceholderReplacementResult is Google's count of replaced occurrences of one placeholder.
type PlaceholderReplacementResult struct {
	// Placeholder is the replaced placeholder.
	Placeholder string `json:"placeholder"`
	// OccurrencesChanged is the number of occurrences Google replaced.
	OccurrencesChanged int `json:"occurrencesChanged"`
}

// ReplaceDocumentTextOperation implements the replaceDocumentText connector operation.
type ReplaceDocumentTextOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ReplaceDocumentTextOperation) Definition() sdkgo.MutationDefinition {
	return ReplaceDocumentTextDefinition
}

// IdempotencyKey derives the receipt key from the stable connector call ID. The
// required revision, not this key, keeps a repeated dispatch from applying twice.
func (ReplaceDocumentTextOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplaceDocumentTextInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the document and, only at RequiredRevisionID, sends one batch
// guarded by that revision. A document that moved on is reported as replaced
// when it already holds the requested text, and as revisionChanged otherwise.
func (operation ReplaceDocumentTextOperation) Invoke(call sdkgo.Call, input ReplaceDocumentTextInput) sdkgo.MutationAttempt[ReplaceDocumentTextOutput] {
	if err := validateReplaceDocumentTextInput(input, operation.client.maxTextBytes); err != nil {
		return sdkgo.NewMutationBranch(ReplaceDocumentTextBranchDefect, ReplaceDocumentTextOutput{}, docsFailurePointer(sdkgo.FailureValidation, replaceDocumentTextOperationID, err.Error()), sdkgo.Receipt{})
	}
	return runGuardedWrite[ReplaceDocumentTextOutput](operation.client, call, guardedWriteTarget{
		operationID: replaceDocumentTextOperationID, documentID: input.DocumentID, requiredRevisionID: input.RequiredRevisionID,
		applied: ReplaceDocumentTextBranchReplaced, revisionChanged: ReplaceDocumentTextBranchRevisionChanged,
		notFound: ReplaceDocumentTextBranchNotFound, providerRejected: ReplaceDocumentTextBranchProviderRejected,
		invalidResponse: ReplaceDocumentTextBranchInvalidResponse, defect: ReplaceDocumentTextBranchDefect,
	}, documentTextReplacement{input: input})
}

// documentTextReplacement is the guarded change of one replaceDocumentText call.
type documentTextReplacement struct {
	input ReplaceDocumentTextInput
}

func (replacement documentTextReplacement) requestsAtRequiredRevision(
	document documentResource,
	bodyText string,
) ([]documentRequest, *sdkgo.MutationAttempt[ReplaceDocumentTextOutput]) {
	tabID := document.firstTab().TabProperties.TabID
	if replacement.input.Target == ReplaceTargetWholeBody {
		return wholeBodyRequests(document, tabID, replacement.input.Text), nil
	}
	var missing []string
	requests := make([]documentRequest, 0, len(replacement.input.Placeholders))
	for _, placeholder := range replacement.input.Placeholders {
		if !strings.Contains(bodyText, placeholder.Placeholder) {
			missing = append(missing, placeholder.Placeholder)
			continue
		}
		requests = append(requests, documentRequest{ReplaceAllText: &replaceAllTextRequest{
			ReplaceText:  placeholder.Text,
			ContainsText: substringMatchCriteria{Text: placeholder.Placeholder, MatchCase: true},
			TabsCriteria: tabsCriteria{TabIDs: []string{tabID}},
		}})
	}
	if len(missing) > 0 {
		attempt := sdkgo.NewMutationBranch(ReplaceDocumentTextBranchPlaceholderNotFound,
			ReplaceDocumentTextOutput{DocumentID: replacement.input.DocumentID, RevisionID: document.RevisionID, MissingPlaceholders: missing},
			docsFailurePointer(sdkgo.FailureValidation, replaceDocumentTextOperationID, "a placeholder does not occur in the document body; nothing was written"), sdkgo.Receipt{})
		return nil, &attempt
	}
	return requests, nil
}

// isHeldBy reports a body that equals the requested text, or that has no
// placeholder left and contains every non-blank replacement.
func (replacement documentTextReplacement) isHeldBy(bodyText string) bool {
	if replacement.input.Target == ReplaceTargetWholeBody {
		return bodyText == replacement.input.Text+"\n"
	}
	for _, placeholder := range replacement.input.Placeholders {
		if strings.Contains(bodyText, placeholder.Placeholder) || !strings.Contains(bodyText, placeholder.Text) {
			return false
		}
	}
	return true
}

func (replacement documentTextReplacement) output(revisionID string, replies []batchUpdateReply, wasAlreadyApplied bool) ReplaceDocumentTextOutput {
	output := ReplaceDocumentTextOutput{DocumentID: replacement.input.DocumentID, RevisionID: revisionID, WasAlreadyApplied: wasAlreadyApplied}
	if replacement.input.Target != ReplaceTargetPlaceholders || len(replies) == 0 {
		return output
	}
	for index, placeholder := range replacement.input.Placeholders {
		result := PlaceholderReplacementResult{Placeholder: placeholder.Placeholder}
		if index < len(replies) && replies[index].ReplaceAllText != nil {
			result.OccurrencesChanged = replies[index].ReplaceAllText.OccurrencesChanged
		}
		output.Replacements = append(output.Replacements, result)
	}
	return output
}

// wholeBodyRequests delete everything but the body's final newline, then
// insert text as plain normal-style paragraphs.
func wholeBodyRequests(document documentResource, tabID string, text string) []documentRequest {
	startIndex, finalNewlineIndex := bodyStartIndex(document), document.bodyEndIndex()-1
	var requests []documentRequest
	if finalNewlineIndex > startIndex {
		requests = append(requests, documentRequest{DeleteContentRange: &rangeRequest{
			Range: documentRange{StartIndex: startIndex, EndIndex: finalNewlineIndex, TabID: tabID},
		}})
	}
	if text == "" {
		return requests
	}
	insertedLength := utf16Length(text)
	requests = append(requests, documentRequest{InsertText: &insertTextRequest{
		Text: text, Location: documentLocation{Index: startIndex, TabID: tabID},
	}})
	return append(requests, plainParagraphRequests(tabID, startIndex, startIndex+insertedLength)...)
}

func validateReplaceDocumentTextInput(input ReplaceDocumentTextInput, maxTextBytes int64) error {
	if err := validateGuardedWriteTarget(input.DocumentID, input.RequiredRevisionID); err != nil {
		return err
	}
	switch input.Target {
	case ReplaceTargetWholeBody:
		if len(input.Placeholders) != 0 {
			return errors.New("wholeBody takes text, not placeholders")
		}
		if int64(len(input.Text)) > maxTextBytes {
			return errors.New("text exceeds the configured text limit")
		}
		return validateWriteText("text", input.Text)
	case ReplaceTargetPlaceholders:
		return validatePlaceholderReplacements(input, maxTextBytes)
	default:
		return errors.New("target must be wholeBody or placeholders")
	}
}

// validatePlaceholderReplacements rejects placeholders whose replacement order
// or result would be ambiguous, so a written document is recognizable.
func validatePlaceholderReplacements(input ReplaceDocumentTextInput, maxTextBytes int64) error {
	if input.Text != "" {
		return errors.New("placeholders takes placeholders, not text")
	}
	if len(input.Placeholders) == 0 || len(input.Placeholders) > maxPlaceholders {
		return fmt.Errorf("placeholders must list 1 to %d placeholders", maxPlaceholders)
	}
	totalBytes := 0
	for index, placeholder := range input.Placeholders {
		name := placeholder.Placeholder
		if strings.TrimSpace(name) == "" || len(name) > maxPlaceholderBytes || strings.ContainsAny(name, "\n\v") {
			return fmt.Errorf("placeholders[%d].placeholder must be 1 to %d bytes on one line", index, maxPlaceholderBytes)
		}
		if err := validateWriteText(fmt.Sprintf("placeholders[%d].placeholder", index), name); err != nil {
			return err
		}
		if err := validateWriteText(fmt.Sprintf("placeholders[%d].text", index), placeholder.Text); err != nil {
			return err
		}
		totalBytes += len(placeholder.Text)
		for otherIndex, other := range input.Placeholders {
			if otherIndex == index {
				continue
			}
			if strings.Contains(other.Placeholder, name) {
				return fmt.Errorf("placeholders[%d].placeholder repeats or is part of another placeholder", index)
			}
		}
		for _, other := range input.Placeholders {
			if strings.Contains(placeholder.Text, other.Placeholder) {
				return fmt.Errorf("placeholders[%d].text cannot contain a requested placeholder", index)
			}
		}
	}
	if int64(totalBytes) > maxTextBytes {
		return errors.New("placeholder replacements exceed the configured text limit")
	}
	return nil
}
