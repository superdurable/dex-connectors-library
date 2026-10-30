// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listTasksOperationID    = "listTasks"
	listTasksFailureSubject = "task list"
	defaultTaskPageSize     = 50
	maximumTaskPageSize     = 100
	maximumOffsetBytes      = 4096
	completedSinceNow       = "now"
)

// ListTasksInput selects one page of tasks. Set exactly one of ProjectID and SectionID, and at most one
// of IsIncompleteOnly and CompletedSince.
type ListTasksInput struct {
	// ProjectID lists the tasks of this project gid.
	ProjectID string `json:"projectId,omitempty"`
	// SectionID lists the tasks of this section gid.
	SectionID string `json:"sectionId,omitempty"`
	// IsIncompleteOnly lists only incomplete tasks.
	IsIncompleteOnly bool `json:"isIncompleteOnly,omitempty"`
	// CompletedSince lists incomplete tasks plus tasks completed at or after this time.
	CompletedSince *time.Time `json:"completedSince,omitempty"`
	// ModifiedSince lists only tasks that changed at or after this time. Asana counts assigning, renaming,
	// completing, commenting on, or adding the task to a project as a change.
	ModifiedSince *time.Time `json:"modifiedSince,omitempty"`
	// PageSize is the number of tasks to request, 1 to 100; zero requests 50.
	PageSize int `json:"pageSize,omitempty"`
	// Offset continues a previous list with the same filters; blank reads the first page. Asana expires
	// offset tokens after some time.
	Offset string `json:"offset,omitempty"`
}

// ListTasksOutput is one page of tasks in Asana's order.
type ListTasksOutput struct {
	// Tasks lists the page's tasks without notes or custom field values.
	Tasks []Task `json:"tasks"`
	// NextOffset continues the list; it is empty on the last page.
	NextOffset string `json:"nextOffset,omitempty"`
}

// ListTasksOperation implements the listTasks Query with GET /tasks.
type ListTasksOperation struct{ client *Client }

type taskPageResponse struct {
	Data     []taskResource `json:"data"`
	NextPage *struct {
		Offset string `json:"offset"`
	} `json:"next_page"`
}

// Definition returns the immutable connector operation definition.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Invoke reads one page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[ListTasksOutput] {
	client := operation.client
	query, err := buildListTasksQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListTasksBranchDefect, ListTasksOutput{}, failurePointer(listTasksOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, listTasksOperationID)
	if startFailure != nil {
		return sdkgo.NewQueryBranch(ListTasksBranchDefect, ListTasksOutput{}, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, asanaRequest{method: http.MethodGet, path: "/tasks", query: query})
	classification := client.classifyRead(listTasksOperationID, listTasksFailureSubject, result)
	receipt := client.receipt(session, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[ListTasksOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(ListTasksBranchNotFound, ListTasksOutput{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(ListTasksBranchDefect, ListTasksOutput{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(ListTasksBranchInvalidResponse, ListTasksOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListTasksBranchProviderRejected, ListTasksOutput{}, &classification.failure, receipt)
	}
	output, err := decodeTaskPage(result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListTasksBranchInvalidResponse, ListTasksOutput{}, failurePointer(listTasksOperationID, sdkgo.FailureProtocol, "Asana returned an invalid task page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListTasksBranchListed, output, nil, receipt)
}

func buildListTasksQuery(input ListTasksInput) (url.Values, error) {
	projectID, err := validateOptionalGID(input.ProjectID, "projectId")
	if err != nil {
		return nil, err
	}
	sectionID, err := validateOptionalGID(input.SectionID, "sectionId")
	if err != nil {
		return nil, err
	}
	query := url.Values{"opt_fields": {strings.Join(taskSummaryFields, ",")}}
	switch {
	case (projectID == "") == (sectionID == ""):
		return nil, errors.New("set exactly one of projectId and sectionId")
	case projectID != "":
		query.Set("project", projectID)
	default:
		query.Set("section", sectionID)
	}
	switch {
	case input.IsIncompleteOnly && input.CompletedSince != nil:
		return nil, errors.New("set at most one of isIncompleteOnly and completedSince")
	case input.IsIncompleteOnly:
		query.Set("completed_since", completedSinceNow)
	case input.CompletedSince != nil:
		query.Set("completed_since", input.CompletedSince.UTC().Format(time.RFC3339))
	}
	if input.ModifiedSince != nil {
		query.Set("modified_since", input.ModifiedSince.UTC().Format(time.RFC3339))
	}
	pageSize := input.PageSize
	switch {
	case pageSize == 0:
		pageSize = defaultTaskPageSize
	case pageSize < 1 || pageSize > maximumTaskPageSize:
		return nil, errors.New("pageSize must be between 1 and 100")
	}
	query.Set("limit", strconv.Itoa(pageSize))
	offset := strings.TrimSpace(input.Offset)
	if offset != "" {
		if !isOffsetToken(offset) {
			return nil, errors.New("offset must be the token from a previous task page")
		}
		query.Set("offset", offset)
	}
	return query, nil
}

func decodeTaskPage(body []byte) (ListTasksOutput, error) {
	var page taskPageResponse
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil {
		return ListTasksOutput{}, errors.New("response has no data array")
	}
	if len(page.Data) > maximumTaskPageSize {
		return ListTasksOutput{}, errors.New("page holds more tasks than requested")
	}
	output := ListTasksOutput{Tasks: make([]Task, 0, len(page.Data))}
	for _, resource := range page.Data {
		task, err := decodeTask(resource, false)
		if err != nil {
			return ListTasksOutput{}, err
		}
		output.Tasks = append(output.Tasks, task)
	}
	if page.NextPage != nil && page.NextPage.Offset != "" {
		if !isOffsetToken(page.NextPage.Offset) {
			return ListTasksOutput{}, errors.New("next page offset is invalid")
		}
		output.NextOffset = page.NextPage.Offset
	}
	return output, nil
}

// isOffsetToken accepts a bounded printable token without spaces, the shape of Asana's opaque offsets.
func isOffsetToken(value string) bool {
	if value == "" || len(value) > maximumOffsetBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
