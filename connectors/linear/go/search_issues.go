// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	searchIssuesOperationID = "searchIssues"
	// MaxSearchPageSize is the connector's page bound, which keeps one page's Step state small.
	MaxSearchPageSize = 100
	// DefaultSearchPageSize is the page size a zero PageSize requests, Linear's own default.
	DefaultSearchPageSize = 50
	// maxFilterValues bounds each list in an IssueSearchFilter.
	maxFilterValues = 20

	searchIssuesDocument = `query LinearSearchIssues($filter: IssueFilter, $first: Int!, $after: String, $orderBy: PaginationOrderBy, $includeArchived: Boolean) {
  issues(filter: $filter, first: $first, after: $after, orderBy: $orderBy, includeArchived: $includeArchived) {
    nodes { ` + issueSummaryFields + ` }
    pageInfo { hasNextPage endCursor }
  }
}`
)

var cursorPattern = regexp.MustCompile(`^[A-Za-z0-9_\-.~=+/:]{1,512}$`)

// IssueOrder is Linear's pagination order. Linear returns the most recent first.
type IssueOrder string

const (
	// IssueOrderCreatedAt orders by creation time, Linear's default.
	IssueOrderCreatedAt IssueOrder = "createdAt"
	// IssueOrderUpdatedAt orders by last change, which suits a changed-since sweep.
	IssueOrderUpdatedAt IssueOrder = "updatedAt"
)

// IssueSearchFilter selects issues; every set field must match, and an empty filter matches every issue
// the connection can see. Lists match any of their values.
type IssueSearchFilter struct {
	// TeamID limits the search to one team's UUID, such as the teamPicker unit's teamId.
	TeamID string `json:"teamId,omitempty"`
	// TeamKey limits the search to the team with this key, such as ENG; set at most one of TeamID and TeamKey.
	TeamKey string `json:"teamKey,omitempty"`
	// StateTypes keeps issues whose workflow state has one of these types, such as started.
	StateTypes []WorkflowStateType `json:"stateTypes,omitempty"`
	// StateIDs keeps issues in one of these workflow states.
	StateIDs []string `json:"stateIds,omitempty"`
	// AssigneeID keeps issues assigned to this user UUID.
	AssigneeID string `json:"assigneeId,omitempty"`
	// AssigneeEmail keeps issues assigned to the user with this email, compared without case.
	AssigneeEmail string `json:"assigneeEmail,omitempty"`
	// IsUnassigned keeps only unassigned issues; it excludes AssigneeID and AssigneeEmail.
	IsUnassigned bool `json:"unassigned,omitempty"`
	// LabelIDs keeps issues that carry at least one of these labels.
	LabelIDs []string `json:"labelIds,omitempty"`
	// LabelNames keeps issues that carry a label with one of these exact names, such as Bug.
	LabelNames []string `json:"labelNames,omitempty"`
	// ProjectID keeps issues of this project UUID.
	ProjectID string `json:"projectId,omitempty"`
	// Title keeps issues whose title is exactly this text.
	Title string `json:"title,omitempty"`
	// TitleContains keeps issues whose title contains this text, compared without case.
	TitleContains string `json:"titleContains,omitempty"`
	// UpdatedAfter keeps issues changed strictly after this time; it is the changed-since watermark.
	UpdatedAfter *time.Time `json:"updatedAfter,omitempty"`
	// CreatedAfter keeps issues created strictly after this time.
	CreatedAfter *time.Time `json:"createdAfter,omitempty"`
}

// SearchIssuesInput requests one page. A later page repeats the same Filter, Order, and IncludesArchived
// with the previous page's NextCursor.
type SearchIssuesInput struct {
	// Filter selects the issues.
	Filter IssueSearchFilter `json:"filter"`
	// Order is createdAt or updatedAt; blank uses createdAt.
	Order IssueOrder `json:"order,omitempty"`
	// PageSize is 1 to MaxSearchPageSize; zero requests DefaultSearchPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor continues after a previous page's NextCursor; blank reads the first page.
	Cursor string `json:"cursor,omitempty"`
	// IncludesArchived also returns archived issues.
	IncludesArchived bool `json:"includesArchived,omitempty"`
}

// SearchIssuesOutput is one page of issue summaries.
type SearchIssuesOutput struct {
	// Issues are the page's issue summaries in Linear's order.
	Issues []IssueSummary `json:"issues"`
	// NextCursor continues the search; blank means this was the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// SearchIssuesOperation implements the searchIssues Query.
type SearchIssuesOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (SearchIssuesOperation) Definition() sdkgo.QueryDefinition { return SearchIssuesDefinition }

// Invoke reads one page through Linear's filtered issues query, not its relevance-ranked searchIssues
// full-text query, so pages follow Order and a cursor continues them.
func (operation SearchIssuesOperation) Invoke(call sdkgo.Call, input SearchIssuesInput) sdkgo.QueryAttempt[SearchIssuesOutput] {
	empty := SearchIssuesOutput{Issues: []IssueSummary{}}
	request, err := buildSearchIssuesRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchIssuesBranchDefect, empty, linearFailurePointer(sdkgo.FailureValidation, searchIssuesOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failed := operation.client.openSession(call, searchIssuesOperationID)
	defer cancel()
	if failed != nil {
		return queryAttemptForExchange(*failed, empty, sdkgo.Receipt{CallID: call.ID, Provider: providerName}, searchIssuesBranches)
	}
	result := session.exchange(request)
	receipt := session.receipt(result.response, "")
	if result.outcome != exchangeSucceeded {
		return queryAttemptForExchange(result, empty, receipt, searchIssuesBranches)
	}
	output, err := decodeIssuePage(result.response.data)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchIssuesBranchInvalidResponse, empty, linearFailurePointer(sdkgo.FailureProtocol, searchIssuesOperationID, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchIssuesBranchSearched, output, nil, receipt)
}

var searchIssuesBranches = queryBranches{
	providerRejected: SearchIssuesBranchProviderRejected, invalidResponse: SearchIssuesBranchInvalidResponse, defect: SearchIssuesBranchDefect,
}

func buildSearchIssuesRequest(input SearchIssuesInput) (graphQLRequest, error) {
	pageSize := input.PageSize
	switch {
	case pageSize == 0:
		pageSize = DefaultSearchPageSize
	case pageSize < 1 || pageSize > MaxSearchPageSize:
		return graphQLRequest{}, fmt.Errorf("pageSize must be between 1 and %d", MaxSearchPageSize)
	}
	order := input.Order
	switch order {
	case "":
		order = IssueOrderCreatedAt
	case IssueOrderCreatedAt, IssueOrderUpdatedAt:
	default:
		return graphQLRequest{}, errors.New("order must be createdAt or updatedAt")
	}
	filter, err := input.Filter.graphQLFilter()
	if err != nil {
		return graphQLRequest{}, err
	}
	variables := map[string]any{"first": pageSize, "orderBy": string(order), "includeArchived": input.IncludesArchived}
	if len(filter) != 0 {
		variables["filter"] = filter
	}
	if cursor := strings.TrimSpace(input.Cursor); cursor != "" {
		if !cursorPattern.MatchString(cursor) {
			return graphQLRequest{}, errors.New("cursor must be a NextCursor returned by searchIssues")
		}
		variables["after"] = cursor
	}
	return graphQLRequest{operationName: "LinearSearchIssues", document: searchIssuesDocument, variables: variables}, nil
}

// graphQLFilter builds Linear's IssueFilter; every value travels as a variable, never as query text.
func (filter IssueSearchFilter) graphQLFilter() (map[string]any, error) {
	clauses := map[string]any{}
	if err := filter.addTeamClause(clauses); err != nil {
		return nil, err
	}
	if err := filter.addStateClause(clauses); err != nil {
		return nil, err
	}
	if err := filter.addAssigneeClause(clauses); err != nil {
		return nil, err
	}
	if err := filter.addLabelClause(clauses); err != nil {
		return nil, err
	}
	projectID, err := validateUUIDField(filter.ProjectID, "filter.projectId", false)
	if err != nil {
		return nil, err
	}
	if projectID != "" {
		clauses["project"] = map[string]any{"id": map[string]any{"eq": projectID}}
	}
	titleClause := map[string]any{}
	if filter.Title != "" {
		title, err := validateOneLineText(filter.Title, "filter.title", MaxIssueTitleCharacters)
		if err != nil {
			return nil, err
		}
		titleClause["eq"] = title
	}
	if filter.TitleContains != "" {
		fragment, err := validateOneLineText(filter.TitleContains, "filter.titleContains", MaxIssueTitleCharacters)
		if err != nil {
			return nil, err
		}
		titleClause["containsIgnoreCase"] = fragment
	}
	if len(titleClause) != 0 {
		clauses["title"] = titleClause
	}
	if filter.UpdatedAfter != nil {
		clauses["updatedAt"] = map[string]any{"gt": formatLinearTime(*filter.UpdatedAfter)}
	}
	if filter.CreatedAfter != nil {
		clauses["createdAt"] = map[string]any{"gt": formatLinearTime(*filter.CreatedAfter)}
	}
	return clauses, nil
}

func (filter IssueSearchFilter) addTeamClause(clauses map[string]any) error {
	teamID, err := validateUUIDField(filter.TeamID, "filter.teamId", false)
	if err != nil {
		return err
	}
	teamKey := strings.TrimSpace(filter.TeamKey)
	switch {
	case teamID != "" && teamKey != "":
		return errors.New("set at most one of filter.teamId and filter.teamKey")
	case teamID != "":
		clauses["team"] = map[string]any{"id": map[string]any{"eq": teamID}}
	case teamKey != "":
		if !teamKeyPattern.MatchString(teamKey) {
			return errors.New("filter.teamKey must be a team key such as ENG")
		}
		clauses["team"] = map[string]any{"key": map[string]any{"eq": strings.ToUpper(teamKey)}}
	}
	return nil
}

func (filter IssueSearchFilter) addStateClause(clauses map[string]any) error {
	if len(filter.StateTypes) > maxFilterValues {
		return fmt.Errorf("filter.stateTypes accepts at most %d values", maxFilterValues)
	}
	stateClause := map[string]any{}
	if len(filter.StateTypes) != 0 {
		types := make([]string, 0, len(filter.StateTypes))
		for _, stateType := range filter.StateTypes {
			if !stateType.IsKnown() {
				return errors.New("filter.stateTypes values must be triage, backlog, unstarted, started, completed, canceled, or duplicate")
			}
			types = append(types, string(stateType))
		}
		stateClause["type"] = map[string]any{"in": types}
	}
	stateIDs, err := validateUUIDList(filter.StateIDs, "filter.stateIds")
	if err != nil {
		return err
	}
	if len(stateIDs) != 0 {
		stateClause["id"] = map[string]any{"in": stateIDs}
	}
	if len(stateClause) != 0 {
		clauses["state"] = stateClause
	}
	return nil
}

func (filter IssueSearchFilter) addAssigneeClause(clauses map[string]any) error {
	assigneeID, err := validateUUIDField(filter.AssigneeID, "filter.assigneeId", false)
	if err != nil {
		return err
	}
	switch {
	case filter.IsUnassigned && (assigneeID != "" || filter.AssigneeEmail != ""):
		return errors.New("filter.unassigned excludes filter.assigneeId and filter.assigneeEmail")
	case filter.IsUnassigned:
		clauses["assignee"] = map[string]any{"null": true}
		return nil
	}
	assigneeClause := map[string]any{}
	if assigneeID != "" {
		assigneeClause["id"] = map[string]any{"eq": assigneeID}
	}
	if filter.AssigneeEmail != "" {
		email, err := validateEmail(filter.AssigneeEmail)
		if err != nil {
			return fmt.Errorf("filter.assigneeEmail: %w", err)
		}
		assigneeClause["email"] = map[string]any{"eqIgnoreCase": email}
	}
	if len(assigneeClause) != 0 {
		clauses["assignee"] = assigneeClause
	}
	return nil
}

func (filter IssueSearchFilter) addLabelClause(clauses map[string]any) error {
	labelIDs, err := validateUUIDList(filter.LabelIDs, "filter.labelIds")
	if err != nil {
		return err
	}
	if len(filter.LabelNames) > maxFilterValues {
		return fmt.Errorf("filter.labelNames accepts at most %d names", maxFilterValues)
	}
	labelClause := map[string]any{}
	if len(labelIDs) != 0 {
		labelClause["id"] = map[string]any{"in": labelIDs}
	}
	if len(filter.LabelNames) != 0 {
		names := make([]string, 0, len(filter.LabelNames))
		for _, name := range filter.LabelNames {
			validated, err := validateOneLineText(name, "filter.labelNames", MaxIssueTitleCharacters)
			if err != nil {
				return err
			}
			names = append(names, validated)
		}
		labelClause["name"] = map[string]any{"in": names}
	}
	if len(labelClause) != 0 {
		clauses["labels"] = map[string]any{"some": labelClause}
	}
	return nil
}

// decodeIssuePage reads issues.nodes and pageInfo; a page that continues must name its cursor.
func decodeIssuePage(data json.RawMessage) (SearchIssuesOutput, error) {
	var document struct {
		Issues *struct {
			Nodes    []issueWire `json:"nodes"`
			PageInfo struct {
				HasNextPage bool    `json:"hasNextPage"`
				EndCursor   *string `json:"endCursor"`
			} `json:"pageInfo"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Issues == nil || len(document.Issues.Nodes) > MaxSearchPageSize {
		return SearchIssuesOutput{}, errors.New("Linear returned a malformed issue page")
	}
	output := SearchIssuesOutput{Issues: make([]IssueSummary, 0, len(document.Issues.Nodes))}
	for _, wire := range document.Issues.Nodes {
		summary, err := decodeIssueSummary(wire)
		if err != nil {
			return SearchIssuesOutput{}, err
		}
		output.Issues = append(output.Issues, summary)
	}
	if document.Issues.PageInfo.HasNextPage {
		if document.Issues.PageInfo.EndCursor == nil || !cursorPattern.MatchString(*document.Issues.PageInfo.EndCursor) {
			return SearchIssuesOutput{}, errors.New("Linear returned a continuing page without a usable cursor")
		}
		output.NextCursor = *document.Issues.PageInfo.EndCursor
	}
	return output, nil
}

// formatLinearTime writes the UTC millisecond form Linear stores.
func formatLinearTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}
