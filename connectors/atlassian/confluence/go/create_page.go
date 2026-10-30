// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createPageOperationID    = "createPage"
	createPageFailureSubject = "page"
	// writeAttributionSkew tolerates clock differences between the Worker, Dex, and Confluence.
	writeAttributionSkew = time.Minute
	// ambiguousWriteSettleDelay lets an unconfirmed write commit before the next attempt looks for it.
	ambiguousWriteSettleDelay = 5 * time.Second
)

// CreatePageInput describes one published page. Set exactly one of SpaceID and SpaceKey.
type CreatePageInput struct {
	// SpaceID is the numeric space ID, as the space picker stores it.
	SpaceID string `json:"spaceId,omitempty"`
	// SpaceKey is the space key, such as OPS; the connector looks up its ID before creating.
	SpaceKey string `json:"spaceKey,omitempty"`
	// ParentPageID places the page below this page in the same space; blank uses the space homepage.
	ParentPageID string `json:"parentPageId,omitempty"`
	// Title is the one-line page title, at most 255 characters. Confluence allows one page per title
	// in a space.
	Title string `json:"title"`
	// Body is the page content of at most 262144 characters in BodyFormat.
	Body string `json:"body"`
	// BodyFormat is the format of Body; blank reads Markdown.
	BodyFormat TextFormat `json:"bodyFormat,omitempty"`
}

// CreatePageOutput identifies the page. On created it is the new page. On titleConflict it is the
// existing page with that title, so an application can update it with VersionNumber plus one. On
// notFound and providerRejected only the requested title and space are echoed.
type CreatePageOutput struct {
	// PageID is the page's numeric ID.
	PageID string `json:"pageId,omitempty"`
	// Title echoes the requested title.
	Title string `json:"title"`
	// SpaceID is the numeric ID of the space, resolved from SpaceKey when needed.
	SpaceID string `json:"spaceId,omitempty"`
	// SpaceKey echoes the requested space key.
	SpaceKey string `json:"spaceKey,omitempty"`
	// ParentPageID is the page's parent page ID.
	ParentPageID string `json:"parentPageId,omitempty"`
	// VersionNumber is the page's current version number: 1 for a new page.
	VersionNumber int `json:"versionNumber,omitempty"`
	// WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
	WebURL string `json:"webUrl,omitempty"`
	// IsConfirmedByTitleLookup reports that Confluence did not confirm the create, and the connector
	// found the page this Step execution sent by its title and content.
	IsConfirmedByTitleLookup bool `json:"isConfirmedByTitleLookup,omitempty"`
}

// CreatePageOperation implements the createPage Mutation.
type CreatePageOperation struct{ client *Client }

type createPageRequestBody struct {
	SpaceID  string        `json:"spaceId"`
	Status   string        `json:"status"`
	Title    string        `json:"title"`
	ParentID string        `json:"parentId,omitempty"`
	Body     pageBodyWrite `json:"body"`
}

type pageBodyWrite struct {
	Representation string `json:"representation"`
	Value          string `json:"value"`
}

// writeDispatchCheckpoint is the heartbeat value recorded before a create or comment leaves the Worker.
type writeDispatchCheckpoint struct {
	DispatchedAtUnixMilli int64 `json:"dispatchedAtUnixMilli"`
}

// pageCreation is one validated create with what reconciliation compares against.
type pageCreation struct {
	body           createPageRequestBody
	requested      CreatePageOutput
	comparisonText string
	earliestSendAt time.Time
}

// Definition returns the immutable connector operation definition.
func (CreatePageOperation) Definition() sdkgo.MutationDefinition { return CreatePageDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Confluence accepts no idempotency key; title uniqueness within a space prevents a second page.
func (CreatePageOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one page. An attempt that follows an unconfirmed create first looks the title up and
// reports the page it finds, and a create Confluence refuses is checked the same way, so a repeated
// create returns the page instead of a second one.
func (operation CreatePageOperation) Invoke(call sdkgo.Call, input CreatePageInput) sdkgo.MutationAttempt[CreatePageOutput] {
	client := operation.client
	creation, err := buildPageCreation(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, creation.requested, failurePointer(createPageOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, createPageOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[CreatePageOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, creation.requested, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	if creation.body.SpaceID == "" {
		if attempt, isDecided := operation.resolveSpace(session, &creation); isDecided {
			return attempt
		}
	}
	creation.earliestSendAt = earliestDispatchTime(call, client.now())
	if checkpoint, hasCheckpoint := readDispatchCheckpoint(call); hasCheckpoint {
		creation.earliestSendAt = earlierTime(creation.earliestSendAt, checkpoint)
		lookup := client.findPageByTitle(session, createPageOperationID, creation.body.SpaceID, creation.body.Title)
		if attempt, isDecided := operation.reconcileByTitle(session, creation, lookup); isDecided {
			return attempt
		}
		// No page has the title, and Confluence keeps one page per title, so sending again is safe.
	}
	if err := recordDispatchCheckpoint(call, creation.earliestSendAt); err != nil {
		return sdkgo.NewMutationRetry[CreatePageOutput](newFailure(createPageOperationID, sdkgo.FailureAvailability, "the create checkpoint could not be recorded, so nothing was sent to Confluence"), 0)
	}
	result := client.exchange(session, confluenceRequest{method: http.MethodPost, api: contentAPI, path: "/pages", payload: creation.body})
	classification := client.classifyWrite(createPageOperationID, createPageFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
		var resource pageResource
		if json.Unmarshal(result.response.body, &resource) == nil && validatePageResource(resource) == nil {
			return sdkgo.NewMutationBranch(CreatePageBranchCreated, creation.createdOutput(resource, "", false), nil, client.receipt(session, result.response, resource.ID))
		}
		classification.failure = newFailure(createPageOperationID, sdkgo.FailureProtocol, "Confluence accepted the page but returned an unusable page")
	case writeNotApplied:
		clearDispatchCheckpoint(call)
		return sdkgo.NewMutationRetry[CreatePageOutput](classification.failure, classification.retryAfter)
	case writeNotFound:
		return sdkgo.NewMutationBranch(CreatePageBranchNotFound, creation.requested, &classification.failure, receipt)
	case writeDefect:
		clearDispatchCheckpoint(call)
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, creation.requested, &classification.failure, receipt)
	case writeRejected:
		if result.response.statusCode != http.StatusBadRequest && result.response.statusCode != http.StatusConflict {
			return sdkgo.NewMutationBranch(CreatePageBranchProviderRejected, creation.requested, &classification.failure, receipt)
		}
		// Confluence refuses a second page with the same title, so look for one before reporting a rejection.
		lookup := client.findPageByTitle(session, createPageOperationID, creation.body.SpaceID, creation.body.Title)
		if attempt, isDecided := operation.reconcileByTitle(session, creation, lookup); isDecided {
			return attempt
		}
		return sdkgo.NewMutationBranch(CreatePageBranchProviderRejected, creation.requested, &classification.failure, receipt)
	}
	// The create may have been applied: look for it now, otherwise let a later attempt look again first.
	lookup := client.findPageByTitle(session, createPageOperationID, creation.body.SpaceID, creation.body.Title)
	if lookup.page != nil {
		if attempt, isDecided := operation.reconcileByTitle(session, creation, lookup); isDecided {
			return attempt
		}
	}
	return sdkgo.NewMutationRetry[CreatePageOutput](classification.failure, max(classification.retryAfter, ambiguousWriteSettleDelay))
}

// resolveSpace looks up the ID of the requested space key; isDecided reports a terminal or retry attempt.
func (operation CreatePageOperation) resolveSpace(session *operationSession, creation *pageCreation) (sdkgo.MutationAttempt[CreatePageOutput], bool) {
	spaceID, classification := operation.client.resolveSpaceID(session, createPageOperationID, creation.requested.SpaceKey)
	receipt := operation.client.receipt(session, confluenceResponse{}, "")
	switch classification.outcome {
	case readSucceeded:
		creation.body.SpaceID, creation.requested.SpaceID = spaceID, spaceID
		return sdkgo.MutationAttempt[CreatePageOutput]{}, false
	case readRetry:
		return sdkgo.NewMutationRetry[CreatePageOutput](classification.failure, classification.retryAfter), true
	case readNotFound:
		return sdkgo.NewMutationBranch(CreatePageBranchNotFound, creation.requested, &classification.failure, receipt), true
	case readDefect:
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, creation.requested, &classification.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(CreatePageBranchProviderRejected, creation.requested, &classification.failure, receipt), true
	}
}

// reconcileByTitle reports this Step's page as created and another as titleConflict; isDecided is false without a page.
func (operation CreatePageOperation) reconcileByTitle(
	session *operationSession, creation pageCreation, lookup pageLookup,
) (sdkgo.MutationAttempt[CreatePageOutput], bool) {
	client := operation.client
	switch lookup.classification.outcome {
	case readSucceeded:
	case readNotFound:
		return sdkgo.NewMutationBranch(CreatePageBranchNotFound, creation.requested, &lookup.classification.failure, client.receipt(session, lookup.response, "")), true
	case readRejected:
		// A refused lookup proves nothing, but Confluence still refuses a second page with the title.
		return sdkgo.MutationAttempt[CreatePageOutput]{}, false
	default:
		failure := lookup.classification.failure
		failure.Message = "an earlier create may have reached Confluence and its page could not be looked up: " + failure.Message
		return sdkgo.NewMutationRetry[CreatePageOutput](failure, max(lookup.classification.retryAfter, ambiguousWriteSettleDelay)), true
	}
	if lookup.page == nil {
		return sdkgo.MutationAttempt[CreatePageOutput]{}, false
	}
	receipt := client.receipt(session, lookup.response, lookup.page.ID)
	if creation.isCreatedByThisStep(*lookup.page) {
		return sdkgo.NewMutationBranch(CreatePageBranchCreated, creation.createdOutput(*lookup.page, lookup.siteBase, true), nil, receipt), true
	}
	conflict := creation.createdOutput(*lookup.page, lookup.siteBase, false)
	return sdkgo.NewMutationBranch(CreatePageBranchTitleConflict, conflict,
		failurePointer(createPageOperationID, sdkgo.FailureConflict, "another page in the space already has this title, so nothing was created"), receipt), true
}

// buildPageCreation validates input and returns the request plus the echo used by every branch.
func buildPageCreation(input CreatePageInput) (pageCreation, error) {
	creation := pageCreation{requested: CreatePageOutput{
		Title: strings.TrimSpace(input.Title), SpaceID: strings.TrimSpace(input.SpaceID), SpaceKey: strings.TrimSpace(input.SpaceKey),
		ParentPageID: strings.TrimSpace(input.ParentPageID),
	}}
	requested := &creation.requested
	switch {
	case (requested.SpaceID == "") == (requested.SpaceKey == ""):
		return creation, errors.New("set exactly one of spaceId and spaceKey")
	case requested.SpaceID != "" && !contentIDPattern.MatchString(requested.SpaceID):
		return creation, errors.New("spaceId must be a numeric Confluence space ID")
	case requested.SpaceKey != "" && !spaceKeyPattern.MatchString(requested.SpaceKey):
		return creation, errors.New("spaceKey must be a space key such as OPS")
	case requested.ParentPageID != "" && !contentIDPattern.MatchString(requested.ParentPageID):
		return creation, errors.New("parentPageId must be a numeric Confluence page ID")
	}
	title, err := validatePageTitle(input.Title)
	if err != nil {
		return creation, err
	}
	requested.Title = title
	storage, comparisonText, err := convertWrittenBody(input.Body, input.BodyFormat, "body")
	if err != nil {
		return creation, err
	}
	creation.comparisonText = comparisonText
	creation.body = createPageRequestBody{
		SpaceID: requested.SpaceID, Status: "current", Title: title, ParentID: requested.ParentPageID,
		Body: pageBodyWrite{Representation: string(BodyRepresentationStorage), Value: storage},
	}
	return creation, nil
}

// isCreatedByThisStep requires creation after the first send, the requested parent, and the requested content.
func (creation pageCreation) isCreatedByThisStep(resource pageResource) bool {
	createdAt, err := time.Parse(time.RFC3339Nano, resource.CreatedAt)
	if err != nil || createdAt.Before(creation.earliestSendAt.Add(-writeAttributionSkew)) {
		return false
	}
	if creation.body.ParentID != "" && resource.ParentID != creation.body.ParentID {
		return false
	}
	return resource.Body != nil && resource.Body.Storage != nil && storageComparisonText(resource.Body.Storage.Value) == creation.comparisonText
}

func (creation pageCreation) createdOutput(resource pageResource, siteBase string, isConfirmedByTitleLookup bool) CreatePageOutput {
	output := creation.requested
	output.PageID, output.SpaceID, output.VersionNumber = resource.ID, resource.SpaceID, resource.Version.Number
	output.ParentPageID = ""
	if contentIDPattern.MatchString(resource.ParentID) {
		output.ParentPageID = resource.ParentID
	}
	output.WebURL = buildWebURL(firstNonEmpty(resource.Links.Base, siteBase), resource.Links.WebUI)
	output.IsConfirmedByTitleLookup = isConfirmedByTitleLookup
	return output
}

// convertWrittenBody returns the storage body and the text reconciliation compares a read-back against.
func convertWrittenBody(body string, format TextFormat, fieldName string) (string, string, error) {
	blocks, err := parseWrittenText(body, format, fieldName)
	if err != nil {
		return "", "", err
	}
	storage := renderStorageDocument(blocks)
	return storage, storageComparisonText(storage), nil
}

// storageComparisonText reads storage back the same way for both sides, so markup normalization cancels out.
func storageComparisonText(storage string) string {
	blocks, err := parseStorageDocument(storage)
	if err != nil {
		return ""
	}
	return documentComparisonText(blocks)
}

// readDispatchCheckpoint reports an earlier attempt's send time; an unreadable checkpoint still counts.
func readDispatchCheckpoint(call sdkgo.Call) (time.Time, bool) {
	var checkpoint writeDispatchCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	if err != nil {
		return time.Time{}, true
	}
	if !isFound {
		return time.Time{}, false
	}
	if checkpoint.DispatchedAtUnixMilli <= 0 {
		return time.Time{}, true
	}
	return time.UnixMilli(checkpoint.DispatchedAtUnixMilli), true
}

func recordDispatchCheckpoint(call sdkgo.Call, dispatchedAt time.Time) error {
	return call.Context.RecordHeartbeat(writeDispatchCheckpoint{DispatchedAtUnixMilli: dispatchedAt.UnixMilli()})
}

// clearDispatchCheckpoint removes the checkpoint after Confluence provably wrote nothing.
func clearDispatchCheckpoint(call sdkgo.Call) {
	// A lost clear leaves the checkpoint, so the next attempt looks before it sends.
	_ = call.Context.RecordHeartbeat(nil)
}

// earliestDispatchTime is the earlier of now and Dex's first attempt time for this Step execution.
func earliestDispatchTime(call sdkgo.Call, now time.Time) time.Time {
	return earlierTime(now, call.Context.FirstAttemptAt())
}

// earlierTime returns the earlier of two times, ignoring a zero one.
func earlierTime(current time.Time, candidate time.Time) time.Time {
	if !candidate.IsZero() && (current.IsZero() || candidate.Before(current)) {
		return candidate
	}
	return current
}
