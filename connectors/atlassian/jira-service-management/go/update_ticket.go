// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateTicketOperationID    = "updateTicket"
	updateTicketFailureSubject = "request update"
)

// UpdateTicketInput adds or removes labels on one request and sets its priority. Set at least one change.
type UpdateTicketInput struct {
	// IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// AddLabels adds these labels, without whitespace, and keeps the request's other labels.
	AddLabels []string `json:"addLabels,omitempty"`
	// RemoveLabels removes these labels and keeps the request's other labels. Labels compare exactly.
	RemoveLabels []string `json:"removeLabels,omitempty"`
	// PriorityName sets the priority by the site's name, such as High, compared without case; blank keeps it.
	PriorityName string `json:"priorityName,omitempty"`
}

// UpdateTicketOutput is the request's labels and priority after the change.
type UpdateTicketOutput struct {
	// IssueID is the request's numeric issue ID.
	IssueID string `json:"issueId,omitempty"`
	// IssueKey is the request's current key.
	IssueKey string `json:"issueKey,omitempty"`
	// Labels lists the request's labels after the change.
	Labels []string `json:"labels"`
	// Priority is the request's priority after the change, or nil when it has none. After a write that set
	// the priority, only its Name is known.
	Priority *PriorityReference `json:"priority,omitempty"`
	// WasAlreadyApplied reports that the request already held every requested value, written by an earlier
	// attempt or by someone else, so this attempt wrote nothing.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// RejectedFieldIDs lists the field IDs Jira named in a rejection, such as priority.
	RejectedFieldIDs []string `json:"rejectedFieldIds,omitempty"`
}

// UpdateTicketOperation implements the updateTicket Mutation.
type UpdateTicketOperation struct{ client *Client }

type labelsAndPriorityResource struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Labels   []string          `json:"labels"`
		Priority *priorityResource `json:"priority"`
	} `json:"fields"`
}

type labelOperation struct {
	Add    string `json:"add,omitempty"`
	Remove string `json:"remove,omitempty"`
}

type editIssueRequestBody struct {
	Update map[string][]labelOperation    `json:"update,omitempty"`
	Fields map[string]jiraObjectReference `json:"fields,omitempty"`
}

// labelsAndPriorityChange is the validated change of one updateTicket input.
type labelsAndPriorityChange struct {
	issueIDOrKey string
	addLabels    []string
	removeLabels []string
	priorityName string
}

// Definition returns the immutable connector operation definition.
func (UpdateTicketOperation) Definition() sdkgo.MutationDefinition { return UpdateTicketDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Jira needs none: label
// additions and removals and a priority set are absolute, so a repeated write changes nothing more.
func (UpdateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the request's labels and priority, then sends PUT /rest/api/3/issue/{issueIdOrKey} with only
// the label operations and priority that differ. Every unconfirmed outcome is retried, because the next
// attempt reads the request again and finds a write that was applied.
func (operation UpdateTicketOperation) Invoke(call sdkgo.Call, input UpdateTicketInput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	client := operation.client
	change, err := validateLabelsAndPriorityChange(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{Labels: []string{}}, failurePointer(updateTicketOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, updateTicketOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[UpdateTicketOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{Labels: []string{}}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, providerRequest{
		method: http.MethodGet, api: jiraPlatformAPI, path: issuePath(change.issueIDOrKey), query: url.Values{"fields": {"labels,priority"}},
	})
	receipt := client.receipt(session, result.response, change.issueIDOrKey)
	if classification := client.classifyRead(updateTicketOperationID, "request", result); classification.outcome != readSucceeded {
		return updateTicketFailureAttempt(classification, UpdateTicketOutput{Labels: []string{}}, receipt)
	}
	var current labelsAndPriorityResource
	if err := json.Unmarshal(result.response.body, &current); err != nil || !numericIDPattern.MatchString(current.ID) || !issueKeyPattern.MatchString(current.Key) {
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, UpdateTicketOutput{Labels: []string{}}, failurePointer(updateTicketOperationID, sdkgo.FailureProtocol, "Jira returned an invalid request issue"), receipt)
	}
	receipt.ProviderObjectID = current.Key
	output := UpdateTicketOutput{IssueID: current.ID, IssueKey: current.Key, Labels: append([]string{}, current.Fields.Labels...)}
	if current.Fields.Priority != nil {
		output.Priority = &PriorityReference{ID: current.Fields.Priority.ID, Name: current.Fields.Priority.Name}
	}
	body, labelsAfter := change.editBody(output)
	if body.Update == nil && body.Fields == nil {
		output.WasAlreadyApplied = true
		return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, output, nil, receipt)
	}
	result = client.exchange(session, providerRequest{method: http.MethodPut, api: jiraPlatformAPI, path: issuePath(current.Key), payload: body})
	receipt = client.receipt(session, result.response, current.Key)
	if classification := client.classifyRead(updateTicketOperationID, updateTicketFailureSubject, result); classification.outcome != readSucceeded {
		if classification.outcome == readRejected {
			output.RejectedFieldIDs = readProviderRejection(result.response.body).fieldIDs
		}
		return updateTicketFailureAttempt(classification, output, receipt)
	}
	output.Labels = labelsAfter
	if _, isPrioritySet := body.Fields["priority"]; isPrioritySet {
		output.Priority = &PriorityReference{Name: change.priorityName}
	}
	return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, output, nil, receipt)
}

// editBody returns only the label operations and priority that differ from current, and the resulting labels.
func (change labelsAndPriorityChange) editBody(current UpdateTicketOutput) (editIssueRequestBody, []string) {
	present := map[string]bool{}
	for _, label := range current.Labels {
		present[label] = true
	}
	var operations []labelOperation
	labelsAfter := []string{}
	removed := map[string]bool{}
	for _, label := range change.removeLabels {
		if present[label] && !removed[label] {
			removed[label] = true
			operations = append(operations, labelOperation{Remove: label})
		}
	}
	for _, label := range current.Labels {
		if !removed[label] {
			labelsAfter = append(labelsAfter, label)
		}
	}
	for _, label := range change.addLabels {
		if !present[label] {
			present[label] = true
			operations = append(operations, labelOperation{Add: label})
			labelsAfter = append(labelsAfter, label)
		}
	}
	body := editIssueRequestBody{}
	if len(operations) > 0 {
		body.Update = map[string][]labelOperation{"labels": operations}
	}
	if change.priorityName != "" && (current.Priority == nil || !strings.EqualFold(current.Priority.Name, change.priorityName)) {
		body.Fields = map[string]jiraObjectReference{"priority": {Name: change.priorityName}}
	}
	return body, labelsAfter
}

func validateLabelsAndPriorityChange(input UpdateTicketInput) (labelsAndPriorityChange, error) {
	issueIDOrKey, err := validateIssueIDOrKey(input.IssueIDOrKey)
	if err != nil {
		return labelsAndPriorityChange{}, err
	}
	change := labelsAndPriorityChange{issueIDOrKey: issueIDOrKey}
	if change.addLabels, err = validateLabels(input.AddLabels, "addLabels"); err != nil {
		return labelsAndPriorityChange{}, err
	}
	if change.removeLabels, err = validateLabels(input.RemoveLabels, "removeLabels"); err != nil {
		return labelsAndPriorityChange{}, err
	}
	for _, added := range change.addLabels {
		for _, removed := range change.removeLabels {
			if added == removed {
				return labelsAndPriorityChange{}, errors.New("a label cannot be both added and removed")
			}
		}
	}
	if strings.TrimSpace(input.PriorityName) != "" {
		if change.priorityName, err = validateSingleLineText(input.PriorityName, "priorityName", maximumIdentifierCharacters); err != nil {
			return labelsAndPriorityChange{}, err
		}
	}
	if len(change.addLabels) == 0 && len(change.removeLabels) == 0 && change.priorityName == "" {
		return labelsAndPriorityChange{}, errors.New("set addLabels, removeLabels, or priorityName")
	}
	return change, nil
}

func updateTicketFailureAttempt(classification readClassification, output UpdateTicketOutput, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateTicketOutput] {
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewMutationRetry[UpdateTicketOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewMutationBranch(UpdateTicketBranchNotFound, output, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, output, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, output, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateTicketBranchProviderRejected, output, &classification.failure, receipt)
	}
}
