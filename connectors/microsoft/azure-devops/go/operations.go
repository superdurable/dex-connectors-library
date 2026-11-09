// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azuredevops

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListProjectsInput selects a bounded page of accessible projects.
type ListProjectsInput struct {
	// Top is the maximum number of projects in this page, from 1 to 100.
	Top int `json:"top"`
	// Skip is the nonnegative number of projects omitted before this page.
	Skip int `json:"skip"`
}

// ProjectMetadata contains stable project identity and state.
type ProjectMetadata struct {
	// ID is the Azure DevOps project UUID.
	ID string `json:"id"`
	// Name is the project name shown in Azure DevOps.
	Name string `json:"name"`
	// State is the provider's current project state.
	State string `json:"state"`
}

// Projects contains accessible projects without links or descriptions.
type Projects struct {
	// Projects contains IDs, names, and states.
	Projects []ProjectMetadata `json:"projects"`
	// Top is the requested maximum page size.
	Top int `json:"top"`
	// Skip is the requested project offset.
	Skip int `json:"skip"`
}

// GetWorkItemInput selects one work item by numeric ID.
type GetWorkItemInput struct {
	// WorkItemID is the decimal identifier displayed in Azure DevOps.
	WorkItemID string `json:"workItemId"`
}

// WorkItemMetadata contains a work item's basic identity.
type WorkItemMetadata struct {
	// ID is the stable work item identifier.
	ID int64 `json:"id"`
	// Title is the work item's title.
	Title string `json:"title"`
	// State is the current workflow state.
	State string `json:"state"`
	// WorkItemType is the Azure DevOps work item type.
	WorkItemType string `json:"workItemType"`
}

// ListProjectsOperation reads accessible projects.
type ListProjectsOperation struct{ client *Client }

// GetWorkItemOperation reads one work item.
type GetWorkItemOperation struct{ client *Client }

// ListProjects binds project discovery to this client.
func (client *Client) ListProjects() ListProjectsOperation { return ListProjectsOperation{client} }

// GetWorkItem binds the work item Query to this client.
func (client *Client) GetWorkItem() GetWorkItemOperation { return GetWorkItemOperation{client} }

// Definition returns the generated projects Query contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Definition returns the generated work item Query contract.
func (GetWorkItemOperation) Definition() sdkgo.QueryDefinition { return GetWorkItemDefinition }

// Invoke reads IDs, names, and states of visible projects.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[Projects] {
	branches := queryBranches{found: ListProjectsBranchListed, rejected: ListProjectsBranchProviderRejected, invalid: ListProjectsBranchInvalidResponse, defect: ListProjectsBranchDefect}
	if input.Top < 1 || input.Top > 100 || input.Skip < 0 {
		return queryBranch("listProjects", branches.defect, Projects{}, sdkgo.FailureValidation, "project page size or offset is invalid")
	}
	query := url.Values{"$top": {strconv.Itoa(input.Top)}, "$skip": {strconv.Itoa(input.Skip)}, "api-version": {"7.1"}}
	return invokeQuery(call, operation.client, "listProjects", "/_apis/projects?"+query.Encode(), branches, func(body []byte) (Projects, error) { return decodeProjects(body, input) })
}

// Invoke reads one work item identity by ID.
func (operation GetWorkItemOperation) Invoke(call sdkgo.Call, input GetWorkItemInput) sdkgo.QueryAttempt[WorkItemMetadata] {
	branches := queryBranches{found: GetWorkItemBranchFound, notFound: GetWorkItemBranchNotFound, rejected: GetWorkItemBranchProviderRejected, invalid: GetWorkItemBranchInvalidResponse, defect: GetWorkItemBranchDefect}
	if !isPositiveWorkItemID(input.WorkItemID) {
		return queryBranch("getWorkItem", branches.defect, WorkItemMetadata{}, sdkgo.FailureValidation, "work item ID must be a positive decimal identifier")
	}
	return invokeQuery(call, operation.client, "getWorkItem", "/_apis/wit/workitems/"+input.WorkItemID+"?api-version=7.1", branches, decodeWorkItem)
}

func isPositiveWorkItemID(workItemID string) bool {
	if workItemID == "" {
		return false
	}
	for _, digit := range workItemID {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	value, err := strconv.ParseUint(workItemID, 10, 31)
	return err == nil && value > 0
}

func decodeProjects(body []byte, input ListProjectsInput) (Projects, error) {
	var wire struct {
		Value []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Projects{}, err
	}
	if wire.Value == nil {
		return Projects{}, fmt.Errorf("project list missing")
	}
	projects := make([]ProjectMetadata, 0, len(wire.Value))
	for _, project := range wire.Value {
		if project.ID == "" || project.Name == "" {
			return Projects{}, fmt.Errorf("project identity missing")
		}
		projects = append(projects, ProjectMetadata{ID: project.ID, Name: project.Name, State: project.State})
	}
	return Projects{Projects: projects, Top: input.Top, Skip: input.Skip}, nil
}

func decodeWorkItem(body []byte) (WorkItemMetadata, error) {
	var wire struct {
		ID     int64 `json:"id"`
		Fields struct {
			Title        string `json:"System.Title"`
			State        string `json:"System.State"`
			WorkItemType string `json:"System.WorkItemType"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return WorkItemMetadata{}, err
	}
	if wire.ID <= 0 || wire.ID > 2147483647 || wire.Fields.Title == "" {
		return WorkItemMetadata{}, fmt.Errorf("work item identity missing")
	}
	return WorkItemMetadata{ID: wire.ID, Title: wire.Fields.Title, State: wire.Fields.State, WorkItemType: wire.Fields.WorkItemType}, nil
}
