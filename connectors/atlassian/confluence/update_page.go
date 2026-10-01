// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updatePageOperationID           = "updatePage"
	updatePageFailureSubject        = "page update"
	maximumVersionMessageCharacters = 200
	versionMarkerPrefix             = "dex:"
	versionMarkerHexCharacters      = 16
)

// UpdatePageInput replaces one page's title and body as the next version.
type UpdatePageInput struct {
	// PageID is the numeric page ID, such as 123456.
	PageID string `json:"pageId"`
	// Title is the page title after the update, one line of at most 255 characters; repeat the current
	// title to keep it.
	Title string `json:"title"`
	// Body is the complete new page content of at most 262144 characters in BodyFormat.
	Body string `json:"body"`
	// BodyFormat is the format of Body; blank reads Markdown.
	BodyFormat TextFormat `json:"bodyFormat,omitempty"`
	// NextVersionNumber is the version this update creates: the version number getPage returned plus one.
	// Confluence refuses it when another edit saved that version first.
	NextVersionNumber int `json:"nextVersionNumber"`
	// VersionMessage is an optional one-line change summary of at most 200 characters, shown in page
	// history. The connector appends a marker such as [dex:3f9a2c1b7e04d5a6] to recognize its own update.
	VersionMessage string `json:"versionMessage,omitempty"`
}

// UpdatePageOutput describes the page after the update. On versionConflict VersionNumber is the page's
// current version, so an application can read the page again and decide whether to update it.
type UpdatePageOutput struct {
	// PageID echoes the requested page.
	PageID string `json:"pageId"`
	// Title echoes the requested title.
	Title string `json:"title"`
	// VersionNumber is the page's version: NextVersionNumber on updated, the current version otherwise.
	VersionNumber int `json:"versionNumber,omitempty"`
	// WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
	WebURL string `json:"webUrl,omitempty"`
	// IsConfirmedByReadBack reports that Confluence did not confirm the update, and the connector found
	// this Step execution's version by reading the page back.
	IsConfirmedByReadBack bool `json:"isConfirmedByReadBack,omitempty"`
}

// UpdatePageOperation implements the updatePage Mutation.
type UpdatePageOperation struct{ client *Client }

type updatePageRequestBody struct {
	ID      string             `json:"id"`
	Status  string             `json:"status"`
	Title   string             `json:"title"`
	Body    pageBodyWrite      `json:"body"`
	Version versionWriteRecord `json:"version"`
}

type versionWriteRecord struct {
	Number  int    `json:"number"`
	Message string `json:"message"`
}

// pageUpdate is one validated update with the marker that identifies it in page history.
type pageUpdate struct {
	body      updatePageRequestBody
	requested UpdatePageOutput
	marker    string
}

// Definition returns the immutable connector operation definition.
func (UpdatePageOperation) Definition() sdkgo.MutationDefinition { return UpdatePageDefinition }

// IdempotencyKey derives the key, and so the version marker, from the stable call ID.
func (UpdatePageOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends the update and settles every unconfirmed or refused outcome by reading the page back.
// Confluence accepts each version number once, so a repeated update cannot apply twice: the page at
// NextVersionNumber carrying this Step execution's marker is reported as updated.
func (operation UpdatePageOperation) Invoke(call sdkgo.Call, input UpdatePageInput) sdkgo.MutationAttempt[UpdatePageOutput] {
	client := operation.client
	update, err := buildPageUpdate(input, call.IdempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdatePageBranchDefect, update.requested, failurePointer(updatePageOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, updatePageOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[UpdatePageOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(UpdatePageBranchDefect, update.requested, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, confluenceRequest{method: http.MethodPut, api: contentAPI, path: pagePath(update.body.ID), payload: update.body})
	classification := client.classifyWrite(updatePageOperationID, updatePageFailureSubject, result)
	receipt := client.receipt(session, result.response, update.body.ID)
	switch classification.outcome {
	case writeAccepted:
		var resource pageResource
		if json.Unmarshal(result.response.body, &resource) == nil && validatePageResource(resource) == nil && resource.Version.Number == update.body.Version.Number {
			return sdkgo.NewMutationBranch(UpdatePageBranchUpdated, update.output(resource, "", false), nil, receipt)
		}
		classification.failure = newFailure(updatePageOperationID, sdkgo.FailureProtocol, "Confluence accepted the update but returned an unusable page")
	case writeNotApplied:
		return sdkgo.NewMutationRetry[UpdatePageOutput](classification.failure, classification.retryAfter)
	case writeNotFound:
		return sdkgo.NewMutationBranch(UpdatePageBranchNotFound, update.requested, &classification.failure, receipt)
	case writeDefect:
		return sdkgo.NewMutationBranch(UpdatePageBranchDefect, update.requested, &classification.failure, receipt)
	case writeRejected:
		if result.response.statusCode != http.StatusBadRequest && result.response.statusCode != http.StatusConflict {
			return sdkgo.NewMutationBranch(UpdatePageBranchProviderRejected, update.requested, &classification.failure, receipt)
		}
		// A stale version is refused like an invalid body, so the page's current version tells them apart.
		return operation.reconcileRefusedUpdate(session, update, classification.failure)
	}
	return operation.reconcileUnconfirmedUpdate(session, update, classification)
}

// reconcileRefusedUpdate reads the page after a refusal; an unchanged page means Confluence refused the content.
func (operation UpdatePageOperation) reconcileRefusedUpdate(session *operationSession, update pageUpdate, refusal sdkgo.Failure) sdkgo.MutationAttempt[UpdatePageOutput] {
	lookup := operation.client.readPage(session, updatePageOperationID, update.body.ID)
	if attempt, isDecided := operation.reportReadBack(session, update, lookup); isDecided {
		return attempt
	}
	return sdkgo.NewMutationBranch(UpdatePageBranchProviderRejected, update.requested, &refusal, operation.client.receipt(session, lookup.response, update.body.ID))
}

// reconcileUnconfirmedUpdate reads the page after an unconfirmed send and retries when the page is unchanged.
func (operation UpdatePageOperation) reconcileUnconfirmedUpdate(session *operationSession, update pageUpdate, classification writeClassification) sdkgo.MutationAttempt[UpdatePageOutput] {
	lookup := operation.client.readPage(session, updatePageOperationID, update.body.ID)
	if attempt, isDecided := operation.reportReadBack(session, update, lookup); isDecided {
		return attempt
	}
	return sdkgo.NewMutationRetry[UpdatePageOutput](classification.failure, max(classification.retryAfter, ambiguousWriteSettleDelay))
}

// reportReadBack decides from the current version; isDecided is false while the page precedes the requested version.
func (operation UpdatePageOperation) reportReadBack(session *operationSession, update pageUpdate, lookup pageLookup) (sdkgo.MutationAttempt[UpdatePageOutput], bool) {
	receipt := operation.client.receipt(session, lookup.response, update.body.ID)
	switch lookup.classification.outcome {
	case readSucceeded:
	case readNotFound:
		return sdkgo.NewMutationBranch(UpdatePageBranchNotFound, update.requested, &lookup.classification.failure, receipt), true
	default:
		failure := lookup.classification.failure
		failure.Message = "the update outcome is unknown and the page could not be read back: " + failure.Message
		return sdkgo.NewMutationRetry[UpdatePageOutput](failure, max(lookup.classification.retryAfter, ambiguousWriteSettleDelay)), true
	}
	page := *lookup.page
	requestedVersion, currentVersion := update.body.Version.Number, page.Version.Number
	switch {
	case currentVersion == requestedVersion && strings.Contains(page.Version.Message, update.marker):
		return sdkgo.NewMutationBranch(UpdatePageBranchUpdated, update.output(page, lookup.siteBase, true), nil, receipt), true
	case currentVersion == requestedVersion-1:
		return sdkgo.MutationAttempt[UpdatePageOutput]{}, false
	case currentVersion > requestedVersion:
		// A later edit may follow this Step's version, so read that version's own message.
		message, classification := operation.client.readPageVersionMessage(session, updatePageOperationID, update.body.ID, requestedVersion)
		switch {
		case classification.outcome == readSucceeded && strings.Contains(message, update.marker):
			updated := update.output(page, lookup.siteBase, true)
			updated.VersionNumber = requestedVersion
			return sdkgo.NewMutationBranch(UpdatePageBranchUpdated, updated, nil, receipt), true
		case classification.outcome == readRetry:
			return sdkgo.NewMutationRetry[UpdatePageOutput](classification.failure, classification.retryAfter), true
		}
	}
	conflict := update.output(page, lookup.siteBase, false)
	return sdkgo.NewMutationBranch(UpdatePageBranchVersionConflict, conflict, failurePointer(updatePageOperationID, sdkgo.FailureConflict,
		"the page is no longer at the version before nextVersionNumber, so nothing was changed"), receipt), true
}

// buildPageUpdate validates input and derives the version marker from the idempotency key.
func buildPageUpdate(input UpdatePageInput, idempotencyKey sdkgo.IdempotencyKey) (pageUpdate, error) {
	update := pageUpdate{requested: UpdatePageOutput{PageID: strings.TrimSpace(input.PageID), Title: strings.TrimSpace(input.Title)}}
	pageID, err := validateContentID(input.PageID, "pageId")
	if err != nil {
		return update, err
	}
	title, err := validatePageTitle(input.Title)
	if err != nil {
		return update, err
	}
	update.requested.Title = title
	if input.NextVersionNumber < 2 {
		return update, errors.New("nextVersionNumber must be the page's current version number plus one, at least 2")
	}
	message, err := validateVersionMessage(input.VersionMessage)
	if err != nil {
		return update, err
	}
	storage, _, err := convertWrittenBody(input.Body, input.BodyFormat, "body")
	if err != nil {
		return update, err
	}
	update.marker = buildVersionMarker(idempotencyKey)
	if message != "" {
		message += " "
	}
	update.body = updatePageRequestBody{
		ID: pageID, Status: "current", Title: title,
		Body:    pageBodyWrite{Representation: string(BodyRepresentationStorage), Value: storage},
		Version: versionWriteRecord{Number: input.NextVersionNumber, Message: message + "[" + update.marker + "]"},
	}
	return update, nil
}

func validateVersionMessage(message string) (string, error) {
	trimmed := strings.TrimSpace(message)
	switch {
	case !utf8.ValidString(trimmed):
		return "", errors.New("versionMessage must be valid UTF-8")
	case utf8.RuneCountInString(trimmed) > maximumVersionMessageCharacters:
		return "", errors.New("versionMessage cannot exceed 200 characters")
	}
	for _, character := range trimmed {
		if character < ' ' || character == 0x7f {
			return "", errors.New("versionMessage must be one line without control characters")
		}
	}
	return trimmed, nil
}

// buildVersionMarker is stable for one Step execution, so every attempt writes and recognizes the same marker.
func buildVersionMarker(idempotencyKey sdkgo.IdempotencyKey) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return versionMarkerPrefix + hex.EncodeToString(digest[:])[:versionMarkerHexCharacters]
}

func (update pageUpdate) output(resource pageResource, siteBase string, isConfirmedByReadBack bool) UpdatePageOutput {
	output := update.requested
	output.VersionNumber = resource.Version.Number
	output.WebURL = buildWebURL(firstNonEmpty(resource.Links.Base, siteBase), resource.Links.WebUI)
	output.IsConfirmedByReadBack = isConfirmedByReadBack
	return output
}
