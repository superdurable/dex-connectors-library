// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateConversationOperation = "updateConversation"

	// maximumTagPages bounds the tag-name lookup to 500 inbox tags.
	maximumTagPages = 5
	// maximumTagNames bounds each of applyTagNames and removeTagNames.
	maximumTagNames = 20
	// maximumTagNameBytes bounds one tag name.
	maximumTagNameBytes = 100
)

// UpdateConversationInput is one absolute change to a conversation. Empty fields are left
// unchanged; Hiver's API documents no way to unassign a conversation.
type UpdateConversationInput struct {
	// InboxID is the Hiver shared inbox ID, as listInboxes returns it.
	InboxID string `json:"inboxId"`
	// ConversationID is the Hiver conversation ID or the shared mailbox user's Gmail thread ID.
	ConversationID string `json:"conversationId"`
	// Status is the new Hiver status, or empty to keep it.
	Status ConversationStatus `json:"status,omitempty"`
	// AssigneeEmail assigns the conversation to the inbox user with this email, or is empty to
	// keep the assignee. The connector first finds the user with GET /users/search, so an email
	// that is not a user of the inbox selects providerRejected before any change.
	AssigneeEmail string `json:"assigneeEmail,omitempty"`
	// ApplyTagNames are names of existing inbox tags to add; other tags stay. The connector
	// matches a name exactly, then without regard to letter case; it never creates a tag.
	ApplyTagNames []string `json:"applyTagNames,omitempty"`
	// RemoveTagNames are names of existing inbox tags to remove; other tags stay.
	RemoveTagNames []string `json:"removeTagNames,omitempty"`
}

// UpdateConversationOutput is the conversation as read back after the change.
type UpdateConversationOutput struct {
	// Conversation is the conversation Hiver returned after the change.
	Conversation Conversation `json:"conversation"`
	// AssigneeUserID is the Hiver user ID that AssigneeEmail resolved to.
	AssigneeUserID string `json:"assigneeUserId,omitempty"`
	// AppliedTags are the inbox tags ApplyTagNames resolved to.
	AppliedTags []Tag `json:"appliedTags,omitempty"`
	// RemovedTags are the inbox tags RemoveTagNames resolved to.
	RemovedTags []Tag `json:"removedTags,omitempty"`
}

// UpdateConversationOperation is the updateConversation Mutation.
type UpdateConversationOperation struct {
	client *Client
}

type updateConversationRequestWire struct {
	Status   *statusNameWire    `json:"status,omitempty"`
	Assignee *assigneeEmailWire `json:"assignee,omitempty"`
	Tags     *tagChangeWire     `json:"tags,omitempty"`
}

type statusNameWire struct {
	Name string `json:"name"`
}

type assigneeEmailWire struct {
	Email string `json:"email"`
}

type tagChangeWire struct {
	ToApply  []string `json:"to_apply,omitempty"`
	ToRemove []string `json:"to_remove,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (UpdateConversationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateConversationDefinition
}

// IdempotencyKey uses the stable connector Call ID. Hiver documents no idempotency key; the
// change sets absolute values, so the key only correlates the Receipt.
func (UpdateConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke resolves the assignee email and tag names, sends PATCH
// /v1/inboxes/{inbox_id}/conversations/{conversation_id}, and reads the conversation back. A
// retried or concurrently dispatched attempt sends the same absolute status, assignee, and tag
// change again, which leaves the conversation as one attempt would.
func (operation UpdateConversationOperation) Invoke(call sdkgo.Call, input UpdateConversationInput) sdkgo.MutationAttempt[UpdateConversationOutput] {
	if err := validateUpdateConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureValidation, updateConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, updateConversationOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, UpdateConversationOutput{}, failure, sdkgo.Receipt{})
	}
	output := UpdateConversationOutput{}
	if input.AssigneeEmail != "" {
		userID, attempt, isTerminal := operation.resolveAssignee(call, credentials, input)
		if isTerminal {
			return attempt
		}
		output.AssigneeUserID = userID
	}
	if len(input.ApplyTagNames) != 0 || len(input.RemoveTagNames) != 0 {
		applied, removed, attempt, isTerminal := operation.resolveTags(call, credentials, input)
		if isTerminal {
			return attempt
		}
		output.AppliedTags, output.RemovedTags = applied, removed
	}
	writeResult := operation.client.exchange(call, credentials, updateConversationOperation, hiverRequest{
		method: http.MethodPatch, path: conversationPath(input.InboxID, input.ConversationID), jsonPayload: buildConversationChange(input, output),
	})
	receipt := operation.client.receipt(call, writeResult.response, input.ConversationID)
	if attempt, isTerminal := updateConversationAttemptForExchange(writeResult, receipt); isTerminal {
		return attempt
	}
	readResult := operation.client.exchange(call, credentials, updateConversationOperation, hiverRequest{
		method: http.MethodGet, path: conversationPath(input.InboxID, input.ConversationID),
	})
	receipt = operation.client.receipt(call, readResult.response, input.ConversationID)
	if attempt, isTerminal := readBackAttemptForExchange(readResult, receipt); isTerminal {
		return attempt
	}
	details, err := decodeConversationBody(readResult.response.body, input.InboxID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, updateConversationOperation,
			"Hiver accepted the change but returned an invalid conversation: "+err.Error()), receipt)
	}
	output.Conversation = details.Conversation
	if missing := describeUnappliedChange(input, output); missing != "" {
		return sdkgo.NewMutationRetry[UpdateConversationOutput](hiverFailure(sdkgo.FailureConflict, updateConversationOperation,
			"Hiver accepted the change but the conversation read back does not show its "+missing+" yet"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, output, nil, operation.client.receipt(call, readResult.response, output.Conversation.ID))
}

// resolveAssignee finds the inbox user whose email matches AssigneeEmail exactly, ignoring letter case.
func (operation UpdateConversationOperation) resolveAssignee(call sdkgo.Call, credentials Credentials, input UpdateConversationInput) (string, sdkgo.MutationAttempt[UpdateConversationOutput], bool) {
	result := operation.client.exchange(call, credentials, updateConversationOperation, hiverRequest{
		method: http.MethodGet, path: inboxPath(input.InboxID) + "/users/search", query: url.Values{"email": {input.AssigneeEmail}},
	})
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	if attempt, isTerminal := updateConversationAttemptForExchange(result, receipt); isTerminal {
		return "", attempt, true
	}
	results, _, err := decodeListPage(result.response.body)
	if err != nil {
		return "", sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, updateConversationOperation,
			"Hiver returned an invalid user search page: "+err.Error()), receipt), true
	}
	var userIDs []string
	for _, raw := range results {
		var user inboxUserWire
		if json.Unmarshal(raw, &user) != nil || !hiverIDPattern.MatchString(string(user.ID)) {
			return "", sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, updateConversationOperation,
				"Hiver returned an invalid inbox user"), receipt), true
		}
		if strings.EqualFold(strings.TrimSpace(user.Email), input.AssigneeEmail) && !slices.Contains(userIDs, string(user.ID)) {
			userIDs = append(userIDs, string(user.ID))
		}
	}
	if len(userIDs) != 1 {
		return "", sdkgo.NewMutationBranch(UpdateConversationBranchProviderRejected, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureValidation, updateConversationOperation,
			fmt.Sprintf("assigneeEmail matches %d users of the shared inbox instead of one; the change was not sent", len(userIDs))), receipt), true
	}
	return userIDs[0], sdkgo.MutationAttempt[UpdateConversationOutput]{}, false
}

// resolveTags reads at most maximumTagPages pages of inbox tags and maps every requested name to one tag.
func (operation UpdateConversationOperation) resolveTags(call sdkgo.Call, credentials Credentials, input UpdateConversationInput) ([]Tag, []Tag, sdkgo.MutationAttempt[UpdateConversationOutput], bool) {
	var tags []Tag
	var receipt sdkgo.Receipt
	pageToken := ""
	isTagListComplete := false
	for page := 1; page <= maximumTagPages; page++ {
		result := operation.client.exchange(call, credentials, updateConversationOperation, hiverRequest{
			method: http.MethodGet, path: inboxPath(input.InboxID) + "/tags", query: pageQuery(pageToken, MaximumPageSize),
		})
		receipt = operation.client.receipt(call, result.response, input.ConversationID)
		if attempt, isTerminal := updateConversationAttemptForExchange(result, receipt); isTerminal {
			return nil, nil, attempt, true
		}
		results, nextPageToken, err := decodeListPage(result.response.body)
		if err != nil {
			return nil, nil, sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, updateConversationOperation,
				"Hiver returned an invalid tag page: "+err.Error()), receipt), true
		}
		for _, raw := range results {
			tag, err := decodeTag(raw)
			if err != nil {
				return nil, nil, sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, updateConversationOperation,
					"Hiver returned an invalid tag: "+err.Error()), receipt), true
			}
			tags = append(tags, tag)
		}
		isTagListComplete = nextPageToken == ""
		if isTagListComplete || hasExactTagForEveryName(tags, input) {
			break
		}
		pageToken = nextPageToken
	}
	missingTagDescription := "is not a tag of the shared inbox"
	if !isTagListComplete {
		missingTagDescription = fmt.Sprintf("is not among the tags on the first %d pages Hiver returned", maximumTagPages)
	}
	applied, applyErr := matchTagNames("applyTagNames", input.ApplyTagNames, tags, missingTagDescription)
	removed, removeErr := matchTagNames("removeTagNames", input.RemoveTagNames, tags, missingTagDescription)
	if err := errors.Join(applyErr, removeErr); err != nil {
		return nil, nil, sdkgo.NewMutationBranch(UpdateConversationBranchProviderRejected, UpdateConversationOutput{}, hiverFailurePointer(sdkgo.FailureValidation, updateConversationOperation,
			err.Error()+"; the change was not sent"), receipt), true
	}
	return applied, removed, sdkgo.MutationAttempt[UpdateConversationOutput]{}, false
}

// readBackAttemptForExchange selects invalidResponse for a rejected read-back, because Hiver already accepted the change.
func readBackAttemptForExchange(result hiverExchange, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[UpdateConversationOutput], bool) {
	if result.outcome != exchangeRejected {
		return updateConversationAttemptForExchange(result, receipt)
	}
	failure := result.failure
	failure.Message = "Hiver accepted the change but rejected the read-back: " + failure.Message
	return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, &failure, receipt), true
}

// updateConversationAttemptForExchange retries every unconfirmed outcome, because the change is absolute.
func updateConversationAttemptForExchange(result hiverExchange, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[UpdateConversationOutput], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateConversationOutput]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewMutationRetry[UpdateConversationOutput](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateConversationBranchNotFound, UpdateConversationOutput{}, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, UpdateConversationOutput{}, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(UpdateConversationBranchProviderRejected, UpdateConversationOutput{}, &result.failure, receipt), true
	}
}

func validateUpdateConversationInput(input UpdateConversationInput) error {
	if err := errors.Join(validateHiverID("inboxId", input.InboxID), validateHiverID("conversationId", input.ConversationID)); err != nil {
		return err
	}
	if input.Status != "" && !slices.Contains(ConversationStatuses(), input.Status) {
		return errors.New("status must be open, pending, or closed")
	}
	if input.AssigneeEmail != "" {
		address, err := mail.ParseAddress(input.AssigneeEmail)
		if err != nil || address.Name != "" || address.Address != input.AssigneeEmail {
			return errors.New("assigneeEmail must be one bare email address")
		}
	}
	if err := errors.Join(validateTagNames("applyTagNames", input.ApplyTagNames), validateTagNames("removeTagNames", input.RemoveTagNames)); err != nil {
		return err
	}
	for index, name := range input.ApplyTagNames {
		if slices.ContainsFunc(input.RemoveTagNames, func(candidate string) bool { return strings.EqualFold(candidate, name) }) {
			return fmt.Errorf("applyTagNames[%d] is also in removeTagNames", index)
		}
	}
	if input.Status == "" && input.AssigneeEmail == "" && len(input.ApplyTagNames) == 0 && len(input.RemoveTagNames) == 0 {
		return errors.New("at least one of status, assigneeEmail, applyTagNames, or removeTagNames is required")
	}
	return nil
}

func validateTagNames(field string, names []string) error {
	if len(names) > maximumTagNames {
		return fmt.Errorf("%s holds more than %d names", field, maximumTagNames)
	}
	for index, name := range names {
		if strings.TrimSpace(name) != name || name == "" || len(name) > maximumTagNameBytes || strings.ContainsAny(name, "\x00\r\n") {
			return fmt.Errorf("%s[%d] must be a tag name of 1 to %d bytes without surrounding spaces or line breaks", field, index, maximumTagNameBytes)
		}
	}
	return nil
}

// buildConversationChange sends only the requested parts, with tag IDs from the resolved names.
func buildConversationChange(input UpdateConversationInput, resolved UpdateConversationOutput) updateConversationRequestWire {
	change := updateConversationRequestWire{}
	if input.Status != "" {
		change.Status = &statusNameWire{Name: writeConversationStatus(input.Status)}
	}
	if input.AssigneeEmail != "" {
		change.Assignee = &assigneeEmailWire{Email: input.AssigneeEmail}
	}
	if len(resolved.AppliedTags) != 0 || len(resolved.RemovedTags) != 0 {
		change.Tags = &tagChangeWire{ToApply: tagIDs(resolved.AppliedTags), ToRemove: tagIDs(resolved.RemovedTags)}
	}
	return change
}

// describeUnappliedChange names the requested part the read-back conversation lacks, or returns "".
func describeUnappliedChange(input UpdateConversationInput, output UpdateConversationOutput) string {
	conversation := output.Conversation
	switch {
	case input.Status != "" && conversation.Status != input.Status:
		return "status"
	case output.AssigneeUserID != "" && (conversation.Assignee == nil || conversation.Assignee.ID != output.AssigneeUserID):
		return "assignee"
	}
	for _, tag := range output.AppliedTags {
		if !slices.Contains(conversation.TagIDs, tag.ID) {
			return "applied tags"
		}
	}
	for _, tag := range output.RemovedTags {
		if slices.Contains(conversation.TagIDs, tag.ID) {
			return "removed tags"
		}
	}
	return ""
}

func hasExactTagForEveryName(tags []Tag, input UpdateConversationInput) bool {
	for _, name := range slices.Concat(input.ApplyTagNames, input.RemoveTagNames) {
		if !slices.ContainsFunc(tags, func(tag Tag) bool { return tag.Name == name }) {
			return false
		}
	}
	return true
}

// matchTagNames prefers one exact name match, then one match ignoring letter case; errors name only positions.
func matchTagNames(field string, names []string, tags []Tag, missingTagDescription string) ([]Tag, error) {
	matched := make([]Tag, 0, len(names))
	var problems []string
	for index, name := range names {
		candidates := tagsNamed(tags, func(tagName string) bool { return tagName == name })
		if len(candidates) == 0 {
			candidates = tagsNamed(tags, func(tagName string) bool { return strings.EqualFold(tagName, name) })
		}
		switch len(candidates) {
		case 1:
			if !slices.Contains(matched, candidates[0]) {
				matched = append(matched, candidates[0])
			}
		case 0:
			problems = append(problems, field+"["+strconv.Itoa(index)+"] "+missingTagDescription)
		default:
			problems = append(problems, field+"["+strconv.Itoa(index)+"] matches several tags of the shared inbox")
		}
	}
	if len(problems) != 0 {
		return nil, errors.New(strings.Join(problems, ", "))
	}
	return matched, nil
}

func tagsNamed(tags []Tag, isMatch func(string) bool) []Tag {
	var matches []Tag
	for _, tag := range tags {
		if isMatch(tag.Name) && !slices.ContainsFunc(matches, func(match Tag) bool { return match.ID == tag.ID }) {
			matches = append(matches, tag)
		}
	}
	return matches
}

func tagIDs(tags []Tag) []string {
	ids := make([]string, 0, len(tags))
	for _, tag := range tags {
		ids = append(ids, tag.ID)
	}
	return ids
}
