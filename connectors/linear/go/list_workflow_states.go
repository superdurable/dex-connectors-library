// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listWorkflowStatesOperationID = "listWorkflowStates"
	// MaxWorkflowStates bounds one team's listed states, far above Linear's defaults of about six.
	MaxWorkflowStates = 100

	listWorkflowStatesDocument = `query LinearListWorkflowStates($teamId: ID!) {
  workflowStates(filter: { team: { id: { eq: $teamId } } }, first: 100) {
    nodes { id name type position color team { id } }
  }
}`
)

// ListWorkflowStatesInput names the team whose workflow states to list.
type ListWorkflowStatesInput struct {
	// TeamID is the team's UUID, such as the teamPicker unit's teamId.
	TeamID string `json:"teamId"`
}

// WorkflowState is one of a team's workflow states, Linear's issue statuses.
type WorkflowState struct {
	// ID is the state's UUID, which createIssue and updateIssue accept as StateID.
	ID string `json:"id"`
	// Name is the team's own name for the state, such as In Progress.
	Name string `json:"name"`
	// Type is the state's category, such as started.
	Type WorkflowStateType `json:"type"`
	// Position orders the states of one type on the team's board.
	Position float64 `json:"position"`
	// Color is the state's hex color.
	Color string `json:"color,omitempty"`
}

// ListWorkflowStatesOutput is a team's active workflow states, ordered by type from triage to canceled and
// then by position.
type ListWorkflowStatesOutput struct {
	// TeamID echoes the requested team.
	TeamID string `json:"teamId"`
	// States are the team's workflow states.
	States []WorkflowState `json:"states"`
}

// StateNamed returns the state whose name equals name without case, such as In Review.
func (output ListWorkflowStatesOutput) StateNamed(name string) (WorkflowState, bool) {
	for _, state := range output.States {
		if strings.EqualFold(state.Name, strings.TrimSpace(name)) {
			return state, true
		}
	}
	return WorkflowState{}, false
}

// FirstStateOfType returns the first state of the type in board order, such as the team's first started state.
func (output ListWorkflowStatesOutput) FirstStateOfType(stateType WorkflowStateType) (WorkflowState, bool) {
	for _, state := range output.States {
		if state.Type == stateType {
			return state, true
		}
	}
	return WorkflowState{}, false
}

// ListWorkflowStatesOperation implements the listWorkflowStates Query.
type ListWorkflowStatesOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ListWorkflowStatesOperation) Definition() sdkgo.QueryDefinition {
	return ListWorkflowStatesDefinition
}

// Invoke lists the team's states. Every Linear team has states, so an empty list means the team does not
// exist or the connection cannot see it.
func (operation ListWorkflowStatesOperation) Invoke(call sdkgo.Call, input ListWorkflowStatesInput) sdkgo.QueryAttempt[ListWorkflowStatesOutput] {
	requested := ListWorkflowStatesOutput{TeamID: input.TeamID, States: []WorkflowState{}}
	teamID, err := validateUUIDField(input.TeamID, "teamId", true)
	if err != nil {
		return sdkgo.NewQueryBranch(ListWorkflowStatesBranchDefect, requested, linearFailurePointer(sdkgo.FailureValidation, listWorkflowStatesOperationID, err.Error()), sdkgo.Receipt{})
	}
	requested.TeamID = teamID
	session, cancel, failed := operation.client.openSession(call, listWorkflowStatesOperationID)
	defer cancel()
	if failed != nil {
		return queryAttemptForExchange(*failed, requested, sdkgo.Receipt{CallID: call.ID, Provider: providerName}, listWorkflowStatesBranches)
	}
	result := session.exchange(graphQLRequest{operationName: "LinearListWorkflowStates", document: listWorkflowStatesDocument,
		variables: map[string]any{"teamId": teamID}})
	receipt := session.receipt(result.response, teamID)
	if result.outcome != exchangeSucceeded {
		return queryAttemptForExchange(result, requested, receipt, listWorkflowStatesBranches)
	}
	states, err := decodeWorkflowStates(result.response.data, teamID)
	switch {
	case err != nil:
		return sdkgo.NewQueryBranch(ListWorkflowStatesBranchInvalidResponse, requested, linearFailurePointer(sdkgo.FailureProtocol, listWorkflowStatesOperationID, err.Error()), receipt)
	case len(states) == 0:
		return sdkgo.NewQueryBranch(ListWorkflowStatesBranchNotFound, requested, linearFailurePointer(sdkgo.FailureNotFound, listWorkflowStatesOperationID,
			"Linear has no team with this ID that the connection can see"), receipt)
	}
	output := requested
	output.States = states
	return sdkgo.NewQueryBranch(ListWorkflowStatesBranchListed, output, nil, receipt)
}

var listWorkflowStatesBranches = queryBranches{
	providerRejected: ListWorkflowStatesBranchProviderRejected, invalidResponse: ListWorkflowStatesBranchInvalidResponse,
	defect: ListWorkflowStatesBranchDefect,
}

// decodeWorkflowStates requires every state to belong to the requested team and keeps Linear's type values.
func decodeWorkflowStates(data json.RawMessage, teamID string) ([]WorkflowState, error) {
	var document struct {
		WorkflowStates *struct {
			Nodes []struct {
				WorkflowState
				Team *struct {
					ID string `json:"id"`
				} `json:"team"`
			} `json:"nodes"`
		} `json:"workflowStates"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.WorkflowStates == nil || len(document.WorkflowStates.Nodes) > MaxWorkflowStates {
		return nil, errors.New("Linear returned a malformed workflow state list")
	}
	states := make([]WorkflowState, 0, len(document.WorkflowStates.Nodes))
	for _, node := range document.WorkflowStates.Nodes {
		if !isLinearUUID(node.ID) || node.Name == "" || node.Type == "" || node.Team == nil || !strings.EqualFold(node.Team.ID, teamID) {
			return nil, errors.New("Linear returned a malformed workflow state or one of another team")
		}
		states = append(states, node.WorkflowState)
	}
	sort.SliceStable(states, func(left, right int) bool {
		leftOrder, rightOrder := stateTypeRank(states[left].Type), stateTypeRank(states[right].Type)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return states[left].Position < states[right].Position
	})
	return states, nil
}

// stateTypeRank places a type Linear adds later after every documented type.
func stateTypeRank(stateType WorkflowStateType) int {
	if rank := workflowStateTypeOrder[stateType]; rank != 0 {
		return rank
	}
	return len(workflowStateTypeOrder) + 1
}
