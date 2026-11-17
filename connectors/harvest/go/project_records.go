// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetProjectInput identifies one Harvest project.
type GetProjectInput struct {
	// ID is the positive Harvest project ID.
	ID int64 `json:"id"`
}

// GetProjectOperation reads one Harvest project.
type GetProjectOperation struct{ client *Client }

// GetProject binds the project lookup Query to this client.
func (client *Client) GetProject() GetProjectOperation { return GetProjectOperation{client} }

// Definition returns the generated project lookup contract.
func (GetProjectOperation) Definition() sdkgo.QueryDefinition { return GetProjectDefinition }

// Invoke validates the ID and reads one Harvest project.
func (operation GetProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.QueryAttempt[Project] {
	if input.ID <= 0 {
		return failedBranch[Project]("getProject", GetProjectBranchDefect, sdkgo.FailureValidation, "project ID must be positive")
	}
	return invokeGet(operation.client, call, "getProject", "/projects/"+strconv.FormatInt(input.ID, 10), GetProjectBranchFound, GetProjectBranchNotFound, decodeProject)
}

func decodeProject(body []byte) (Project, error) {
	var wire struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		IsActive bool   `json:"is_active"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Project{}, err
	}
	if wire.ID <= 0 || wire.Name == "" {
		return Project{}, fmt.Errorf("missing project identity")
	}
	return Project{ID: wire.ID, Name: wire.Name, IsActive: wire.IsActive}, nil
}
