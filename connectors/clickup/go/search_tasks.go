// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// SearchTasksPageSize is the number of tasks ClickUp returns in one full page.
	SearchTasksPageSize = 100
	// MaxSearchFilterValues bounds each list filter of a searchTasks input.
	MaxSearchFilterValues = 50
	// MaxSearchPage is the largest accepted zero-based page number.
	MaxSearchPage = 1000

	searchTasksOperation = "searchTasks"
)

// TaskOrder is a field ClickUp orders searched tasks by.
type TaskOrder string

const (
	// TaskOrderCreated orders by creation time, ClickUp's default.
	TaskOrderCreated TaskOrder = "created"
	// TaskOrderUpdated orders by last update time.
	TaskOrderUpdated TaskOrder = "updated"
	// TaskOrderDueDate orders by due date.
	TaskOrderDueDate TaskOrder = "due_date"
	// TaskOrderID orders by task ID.
	TaskOrderID TaskOrder = "id"
)

// SearchTasksInput filters one Workspace's tasks. Every non-empty filter narrows the search, and the
// values of one list filter are alternatives.
type SearchTasksInput struct {
	// WorkspaceID is the Workspace (ClickUp API team) ID, the first number in a ClickUp web address.
	WorkspaceID string `json:"workspaceId"`
	// ListIDs keeps tasks of these Lists.
	ListIDs []string `json:"listIds,omitempty"`
	// FolderIDs keeps tasks of Lists in these Folders.
	FolderIDs []string `json:"folderIds,omitempty"`
	// SpaceIDs keeps tasks of these Spaces.
	SpaceIDs []string `json:"spaceIds,omitempty"`
	// Statuses keeps tasks in these status names, such as in progress.
	Statuses []string `json:"statuses,omitempty"`
	// AssigneeIDs keeps tasks assigned to any of these user IDs.
	AssigneeIDs []int64 `json:"assigneeIds,omitempty"`
	// Tags keeps tasks with any of these tag names.
	Tags []string `json:"tags,omitempty"`
	// UpdatedAfter keeps tasks updated after this time, for polling changes since a checkpoint.
	UpdatedAfter *time.Time `json:"updatedAfter,omitempty"`
	// IsClosedIncluded also returns tasks in a closed status; ClickUp excludes them by default.
	IsClosedIncluded bool `json:"isClosedIncluded,omitempty"`
	// AreSubtasksIncluded also returns subtasks; ClickUp excludes them by default.
	AreSubtasksIncluded bool `json:"areSubtasksIncluded,omitempty"`
	// OrderBy is created, updated, due_date, or id; empty uses ClickUp's default order by creation.
	OrderBy TaskOrder `json:"orderBy,omitempty"`
	// IsReverse reverses ClickUp's order.
	IsReverse bool `json:"isReverse,omitempty"`
	// Page is the zero-based page to read; continue with SearchTasksOutput.NextPage.
	Page int `json:"page,omitempty"`
}

// SearchTasksOutput is one page of matching tasks.
type SearchTasksOutput struct {
	// Tasks are the page's tasks without descriptions or custom fields; read those with getTask.
	Tasks []Task `json:"tasks"`
	// Page is the page that was read.
	Page int `json:"page"`
	// NextPage is the next page to read, or nil on the last page.
	NextPage *int `json:"nextPage,omitempty"`
}

// SearchTasksOperation is the searchTasks Query.
type SearchTasksOperation struct {
	client *Client
}

type searchTasksWire struct {
	Tasks    []taskWire `json:"tasks"`
	LastPage *bool      `json:"last_page"`
}

// Definition returns the immutable connector operation definition.
func (SearchTasksOperation) Definition() sdkgo.QueryDefinition { return SearchTasksDefinition }

// Invoke reads GET /team/{team_id}/task, ClickUp's filtered Workspace task search, for one page.
func (operation SearchTasksOperation) Invoke(call sdkgo.Call, input SearchTasksInput) sdkgo.QueryAttempt[SearchTasksOutput] {
	output := SearchTasksOutput{Tasks: []Task{}, Page: input.Page}
	if err := validateSearchTasksInput(input); err != nil {
		return sdkgo.NewQueryBranch(SearchTasksBranchDefect, output, clickupFailurePointer(sdkgo.FailureValidation, searchTasksOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, searchTasksOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchTasksBranchDefect, output, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call.Context, credentials, searchTasksOperation, clickupRequest{
		method: http.MethodGet, path: "/team/" + input.WorkspaceID + "/task", query: searchTasksQuery(input),
	})
	receipt := operation.client.receipt(call, input.WorkspaceID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewQueryRetry[SearchTasksOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(SearchTasksBranchNotFound, output, &result.failure, receipt)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(SearchTasksBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(SearchTasksBranchDefect, output, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchTasksBranchProviderRejected, output, &result.failure, receipt)
	}
	decoded, err := decodeSearchTasksPage(result.response.body, input.Page)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTasksBranchInvalidResponse, output, clickupFailurePointer(sdkgo.FailureProtocol, searchTasksOperation,
			"ClickUp returned an invalid page of tasks: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchTasksBranchSearched, decoded, nil, receipt)
}

// searchTasksQuery encodes the filters with ClickUp's bracketed array parameter names.
func searchTasksQuery(input SearchTasksInput) url.Values {
	query := url.Values{"page": {strconv.Itoa(input.Page)}}
	query["list_ids[]"] = input.ListIDs
	query["project_ids[]"] = input.FolderIDs
	query["space_ids[]"] = input.SpaceIDs
	query["statuses[]"] = input.Statuses
	query["tags[]"] = input.Tags
	for _, assigneeID := range input.AssigneeIDs {
		query.Add("assignees[]", strconv.FormatInt(assigneeID, 10))
	}
	for name, values := range query {
		if len(values) == 0 {
			delete(query, name)
		}
	}
	if input.UpdatedAfter != nil {
		query.Set("date_updated_gt", strconv.FormatInt(input.UpdatedAfter.UnixMilli(), 10))
	}
	if input.IsClosedIncluded {
		query.Set("include_closed", "true")
	}
	if input.AreSubtasksIncluded {
		query.Set("subtasks", "true")
	}
	if input.OrderBy != "" {
		query.Set("order_by", string(input.OrderBy))
	}
	if input.IsReverse {
		query.Set("reverse", "true")
	}
	return query
}

// decodeSearchTasksPage decodes a page; without last_page, a full page means another may follow.
func decodeSearchTasksPage(body []byte, page int) (SearchTasksOutput, error) {
	var wire searchTasksWire
	if err := json.Unmarshal(body, &wire); err != nil || wire.Tasks == nil {
		return SearchTasksOutput{}, errors.New("the response has no tasks array")
	}
	if len(wire.Tasks) > SearchTasksPageSize {
		return SearchTasksOutput{}, fmt.Errorf("the page holds more than %d tasks", SearchTasksPageSize)
	}
	output := SearchTasksOutput{Tasks: make([]Task, 0, len(wire.Tasks)), Page: page}
	for _, taskWire := range wire.Tasks {
		task, err := decodeTask(taskWire, false)
		if err != nil {
			return SearchTasksOutput{}, err
		}
		output.Tasks = append(output.Tasks, task)
	}
	hasMore := len(wire.Tasks) == SearchTasksPageSize
	if wire.LastPage != nil {
		hasMore = !*wire.LastPage && len(wire.Tasks) > 0
	}
	if hasMore {
		nextPage := page + 1
		output.NextPage = &nextPage
	}
	return output, nil
}

func validateSearchTasksInput(input SearchTasksInput) error {
	if err := validateNumericID("workspaceId", input.WorkspaceID); err != nil {
		return err
	}
	for field, ids := range map[string][]string{"listIds": input.ListIDs, "folderIds": input.FolderIDs, "spaceIds": input.SpaceIDs} {
		if len(ids) > MaxSearchFilterValues {
			return fmt.Errorf("%s accepts at most %d IDs", field, MaxSearchFilterValues)
		}
		for _, id := range ids {
			if err := validateNumericID(field, id); err != nil {
				return err
			}
		}
	}
	if len(input.Statuses) > MaxSearchFilterValues || len(input.AssigneeIDs) > MaxSearchFilterValues || len(input.Tags) > MaxSearchFilterValues {
		return fmt.Errorf("statuses, assigneeIds, and tags accept at most %d values each", MaxSearchFilterValues)
	}
	for _, status := range input.Statuses {
		if status == "" {
			return errors.New("statuses must not contain an empty status")
		}
		if err := validateStatusName(status); err != nil {
			return err
		}
	}
	if err := validateUserIDs("assigneeIds", input.AssigneeIDs); err != nil {
		return err
	}
	if err := validateTagNames("tags", input.Tags); err != nil {
		return err
	}
	switch input.OrderBy {
	case "", TaskOrderCreated, TaskOrderUpdated, TaskOrderDueDate, TaskOrderID:
	default:
		return fmt.Errorf("orderBy %q must be created, updated, due_date, or id", input.OrderBy)
	}
	if input.Page < 0 || input.Page > MaxSearchPage {
		return fmt.Errorf("page must be 0 through %d", MaxSearchPage)
	}
	if input.UpdatedAfter != nil && input.UpdatedAfter.UnixMilli() <= 0 {
		return errors.New("updatedAfter must be after the Unix epoch")
	}
	return nil
}
