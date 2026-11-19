// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ProjectMetadata contains the public identity and status of one Copper project.
type ProjectMetadata struct {
	// ID is the stable Copper project identifier.
	ID int64 `json:"id"`
	// Name is the project display name.
	Name string `json:"name"`
	// Status is the current project status.
	Status string `json:"status"`
}

// GetProjectInput identifies one project by Copper ID.
type GetProjectInput struct {
	// ProjectID is a positive decimal identifier.
	ProjectID string `json:"projectId"`
}

// GetProjectOperation reads one project.
type GetProjectOperation struct{ client *Client }

// GetProject binds the Query to this client.
func (client *Client) GetProject() GetProjectOperation {
	return GetProjectOperation{client}
}

// Definition returns the generated Query contract.
func (GetProjectOperation) Definition() sdkgo.QueryDefinition { return GetProjectDefinition }

// Invoke reads one project without custom fields.
func (operation GetProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.QueryAttempt[ProjectMetadata] {
	branches := queryBranches{found: GetProjectBranchFound, notFound: GetProjectBranchNotFound, rejected: GetProjectBranchProviderRejected, invalid: GetProjectBranchInvalidResponse, defect: GetProjectBranchDefect}
	if !validProjectID(input.ProjectID) {
		return queryBranch("getProject", branches.defect, ProjectMetadata{}, sdkgo.FailureValidation, "project ID must be positive")
	}
	return invokeQuery(call, operation.client, "getProject", "/projects/"+input.ProjectID, branches, decodeProject)
}

func decodeProject(body []byte) (ProjectMetadata, error) {
	var wire struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ProjectMetadata{}, err
	}
	if wire.ID <= 0 || strings.TrimSpace(wire.Name) == "" || strings.TrimSpace(wire.Status) == "" {
		return ProjectMetadata{}, fmt.Errorf("project identity missing")
	}
	return ProjectMetadata{ID: wire.ID, Name: wire.Name, Status: wire.Status}, nil
}
