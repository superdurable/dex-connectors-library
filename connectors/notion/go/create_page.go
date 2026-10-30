// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	createPageOperationID    = "createPage"
	createPageFailureSubject = "page creation"
	// MaximumBodyTextCharacters bounds the plain-text body one CreatePage sends.
	MaximumBodyTextCharacters = 50000
)

var paragraphSeparatorPattern = regexp.MustCompile(`\n[ \t]*\n\s*`)

// CreatePageInput describes one database row. Set exactly one of DataSourceID and DatabaseID.
type CreatePageInput struct {
	// DataSourceID is the data source that receives the row. In Notion, open the
	// database's settings menu > Manage data sources and choose Copy data source ID.
	DataSourceID string `json:"dataSourceId,omitempty"`
	// DatabaseID is the database ID from its URL, or the URL itself; the database
	// must hold exactly one data source.
	DatabaseID string `json:"databaseId,omitempty"`
	// Properties maps property names or IDs to typed values, at most 100. A
	// property left out keeps the data source's default, such as an empty title.
	Properties map[string]PropertyValue `json:"properties,omitempty"`
	// BodyText is optional plain text for the page body, at most 50,000 characters
	// in at most 100 paragraphs. Blank lines separate paragraphs, other line breaks
	// stay inside a paragraph, and the text is never read as markup.
	BodyText string `json:"bodyText,omitempty"`
}

// CreatePageOutput identifies the created row. On every other branch PageID is
// empty and DataSourceID names the data source when it was resolved.
type CreatePageOutput struct {
	// DataSourceID is the data source that received, or would have received, the row.
	DataSourceID string `json:"dataSourceId,omitempty"`
	// PageID is the created page's ID.
	PageID string `json:"pageId,omitempty"`
	// Page is the created page, or nil on a non-created outcome.
	Page *Page `json:"page,omitempty"`
	// IsConfirmedAfterTimeout reports a 503 with a saved page ID confirmed during
	// this attempt; a later attempt's read-back cannot recover the original status.
	IsConfirmedAfterTimeout bool `json:"confirmedAfterTimeout,omitempty"`
}

// CreatePageOperation implements the createPage Mutation.
type CreatePageOperation struct{ client *Client }

type createPageRequestBody struct {
	Parent     map[string]string `json:"parent"`
	Properties map[string]any    `json:"properties"`
	Children   []map[string]any  `json:"children,omitempty"`
}

type createPageRequest struct {
	selection  dataSourceSelection
	properties map[string]any
	children   []map[string]any
}

type createPageSend struct {
	client  *Client
	session *operationSession
	body    createPageRequestBody
	result  exchangeResult
}

func (send *createPageSend) dispatch(ctx context.Context) error {
	requestSession := *send.session
	requestSession.context = ctx
	send.result = send.client.exchange(&requestSession, notionRequest{
		method: http.MethodPost, path: "/pages", payload: send.body,
		timeout: writeRequestTimeout, singleDispatchWrite: true,
	})
	return nil
}

// Definition returns the immutable connector operation definition.
func (CreatePageOperation) Definition() sdkgo.MutationDefinition { return CreatePageDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Notion accepts no idempotency key, so it is never sent and cannot deduplicate a repeated create.
func (CreatePageOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one row and never resends a request Notion may have received.
func (operation CreatePageOperation) Invoke(call sdkgo.Call, input CreatePageInput) sdkgo.MutationAttempt[CreatePageOutput] {
	client := operation.client
	fence, err := singledispatch.NewFence(call, CreatePageDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, CreatePageOutput{}, failurePointer(createPageOperationID, sdkgo.FailureLocalDefect, "Notion page dispatch could not be initialized"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(CreatePageOutput{}, newFailure(createPageOperationID, sdkgo.FailureProtocol, "Notion page dispatch checkpoint cannot be trusted"), sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		if decision.Marker.CallID != call.ID || decision.Marker.ProviderID == "" {
			return sdkgo.NewMutationUncertain(CreatePageOutput{}, newFailure(createPageOperationID, sdkgo.FailureProtocol, "Notion page creation may already have been sent without a confirmable page ID"), sdkgo.Receipt{})
		}
		return client.readBackCreatedPage(call, decision.Marker.ProviderID)
	}
	request, err := buildCreatePageRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, CreatePageOutput{}, validationFailure(createPageOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, sessionFailure, isCredentialFailureTransient := client.startSessionWithCredentialStatus(call, createPageOperationID, createPageOperationDeadline)
	if sessionFailure != nil {
		if err := fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[CreatePageOutput](newFailure(createPageOperationID, sdkgo.FailureAvailability, "Notion credential resolution and dispatch checkpoint are unavailable"), 0)
		}
		if isCredentialFailureTransient {
			return sdkgo.NewMutationRetry[CreatePageOutput](*sessionFailure, 0)
		}
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, CreatePageOutput{}, sessionFailure, sdkgo.Receipt{})
	}
	defer cancel()
	dataSourceID, resolution, resolutionResponse := client.resolveDataSourceID(session, createPageOperationID, request.selection)
	if resolution.outcome != readSucceeded {
		if resolution.outcome == readRetry {
			if err := fence.RecordNotSent(); err != nil {
				return sdkgo.NewMutationRetry[CreatePageOutput](newFailure(createPageOperationID, sdkgo.FailureAvailability, "Notion data source lookup and dispatch checkpoint are unavailable"), 0)
			}
		}
		return createPageAttemptForLookup(resolution, client.receipt(session, resolutionResponse, request.selection.databaseID))
	}
	output := CreatePageOutput{DataSourceID: dataSourceID}
	body := createPageRequestBody{
		Parent:     map[string]string{"type": string(ParentTypeDataSource), "data_source_id": dataSourceID},
		Properties: request.properties, Children: request.children,
	}
	send := &createPageSend{client: client, session: session, body: body}
	if err := fence.Dispatch(send.dispatch); err != nil {
		if errors.Is(err, singledispatch.ErrInsufficientTime) || errors.Is(err, singledispatch.ErrCheckpoint) {
			return sdkgo.NewMutationRetry[CreatePageOutput](newFailure(createPageOperationID, sdkgo.FailureTransport, "Notion page dispatch checkpoint or attempt budget is temporarily unavailable"), 0)
		}
		return sdkgo.NewMutationUncertain(output, newFailure(createPageOperationID, sdkgo.FailureProtocol, "Notion page dispatch could not be classified"), sdkgo.Receipt{})
	}
	result := send.result
	classification := client.classifyUnkeyedWrite(createPageOperationID, createPageFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
	case writeCommitted:
		// A lost accepted-ID checkpoint cannot authorize another write.
		_ = fence.RecordProviderID(classification.committedResourceID)
		return client.confirmCommittedPage(session, output, classification.committedResourceID, receipt, true)
	case writeRetry:
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CreatePageOutput](newFailure(createPageOperationID, sdkgo.FailureAvailability, "Notion retry checkpoint could not be recorded"), classification.retryAfter)
		}
		return sdkgo.NewMutationRetry[CreatePageOutput](classification.failure, classification.retryAfter)
	case writeNotFound:
		return sdkgo.NewMutationBranch(CreatePageBranchNotFound, output, &classification.failure, receipt)
	case writeRejected:
		return sdkgo.NewMutationBranch(CreatePageBranchProviderRejected, output, &classification.failure, receipt)
	case writeDefect:
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, output, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationUncertain(output, classification.failure, receipt)
	}
	page, err := decodePageBody(result.response.body)
	if err != nil {
		if pageID := readAcceptedPageID(result.response.body); pageID != "" {
			// Read-back can still confirm this accepted page if its checkpoint is lost.
			_ = fence.RecordProviderID(pageID)
			return client.confirmCommittedPage(session, output, pageID, receipt, false)
		}
		return sdkgo.NewMutationUncertain(output, newFailure(createPageOperationID, sdkgo.FailureProtocol,
			"Notion accepted the page creation but returned an unusable page"), receipt)
	}
	// The complete response confirms the page even if the checkpoint is lost.
	_ = fence.RecordProviderID(page.ID)
	output.PageID, output.Page = page.ID, &page
	receipt.ProviderObjectID = page.ID
	return sdkgo.NewMutationBranch(CreatePageBranchCreated, output, nil, receipt)
}

// confirmCommittedPage reads a known accepted page without sending another write.
func (client *Client) confirmCommittedPage(session *operationSession, output CreatePageOutput, pageID string, receipt sdkgo.Receipt, confirmedAfterTimeout bool) sdkgo.MutationAttempt[CreatePageOutput] {
	output.PageID, output.IsConfirmedAfterTimeout = pageID, confirmedAfterTimeout
	receipt.ProviderObjectID = pageID
	result := client.exchange(session, notionRequest{method: http.MethodGet, path: "/pages/" + url.PathEscape(pageID)})
	classification := client.classifyRead(createPageOperationID, createPageFailureSubject, result)
	if classification.outcome == readRetry {
		return sdkgo.NewMutationRetry[CreatePageOutput](classification.failure, classification.retryAfter)
	}
	if classification.outcome != readSucceeded {
		return sdkgo.NewMutationUncertain(output, newFailure(createPageOperationID, sdkgo.FailureProtocol, "Notion accepted the page but read-back could not confirm it"), receipt)
	}
	page, err := decodePageBody(result.response.body)
	if err != nil || page.ID != pageID {
		return sdkgo.NewMutationUncertain(output, newFailure(createPageOperationID, sdkgo.FailureProtocol, "Notion page read-back did not match the accepted page ID"), receipt)
	}
	output.Page = &page
	if page.Parent.Type == ParentTypeDataSource {
		output.DataSourceID = page.Parent.ID
	}
	return sdkgo.NewMutationBranch(CreatePageBranchCreated, output, nil, receipt)
}

func (client *Client) readBackCreatedPage(call sdkgo.Call, pageID string) sdkgo.MutationAttempt[CreatePageOutput] {
	session, cancel, failure := client.startSession(call, createPageOperationID, createPageOperationDeadline)
	if failure != nil {
		return sdkgo.NewMutationRetry[CreatePageOutput](*failure, 0)
	}
	defer cancel()
	return client.confirmCommittedPage(session, CreatePageOutput{}, pageID, client.receipt(session, notionResponse{}, pageID), false)
}

func readAcceptedPageID(body []byte) string {
	var accepted struct {
		Object string `json:"object"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(body, &accepted); err != nil || accepted.Object != "page" {
		return ""
	}
	id, err := ParseID(accepted.ID)
	if err != nil {
		return ""
	}
	return id
}

// createPageAttemptForLookup maps a failed data source lookup; nothing was written, so a Retry is safe.
func createPageAttemptForLookup(classification readClassification, receipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatePageOutput] {
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewMutationRetry[CreatePageOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewMutationBranch(CreatePageBranchNotFound, CreatePageOutput{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewMutationBranch(CreatePageBranchDefect, CreatePageOutput{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewMutationBranch(CreatePageBranchInvalidResponse, CreatePageOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(CreatePageBranchProviderRejected, CreatePageOutput{}, &classification.failure, receipt)
	}
}

func buildCreatePageRequest(input CreatePageInput) (createPageRequest, error) {
	selection, err := selectDataSource(input.DataSourceID, input.DatabaseID)
	if err != nil {
		return createPageRequest{}, err
	}
	properties, err := encodePropertyValues(input.Properties, "properties")
	if err != nil {
		return createPageRequest{}, err
	}
	children, err := encodeBodyParagraphs(input.BodyText)
	if err != nil {
		return createPageRequest{}, err
	}
	return createPageRequest{selection: selection, properties: properties, children: children}, nil
}

// encodeBodyParagraphs turns plain text into paragraph blocks, one per blank-line-separated paragraph.
func encodeBodyParagraphs(text string) ([]map[string]any, error) {
	if !utf8.ValidString(text) {
		return nil, errors.New("bodyText must be valid UTF-8")
	}
	if utf8.RuneCountInString(text) > MaximumBodyTextCharacters {
		return nil, fmt.Errorf("bodyText is at most %d characters", MaximumBodyTextCharacters)
	}
	normalized := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if normalized == "" {
		return nil, nil
	}
	paragraphs := paragraphSeparatorPattern.Split(normalized, -1)
	if len(paragraphs) > maximumArrayElements {
		return nil, fmt.Errorf("bodyText holds at most %d paragraphs", maximumArrayElements)
	}
	blocks := make([]map[string]any, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		richText, err := encodeRichText(strings.TrimRight(paragraph, " \t\n"), MaximumBodyTextCharacters)
		if err != nil {
			return nil, fmt.Errorf("bodyText: %w", err)
		}
		blocks = append(blocks, map[string]any{"object": "block", "type": "paragraph", "paragraph": map[string]any{"rich_text": richText}})
	}
	return blocks, nil
}
