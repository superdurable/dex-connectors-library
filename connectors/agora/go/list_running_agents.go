// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agora

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListRunningAgentsInput has no parameters; the connection identifies the project.
type ListRunningAgentsInput struct{}

// RunningAgent describes one agent in Agora's list response.
type RunningAgent struct {
	// AgentID identifies the running agent within Agora.
	AgentID string `json:"agentId"`
	// Status is RUNNING for the default list filter.
	Status string `json:"status"`
	// StartTimestamp is the agent creation time in Unix seconds.
	StartTimestamp int64 `json:"startTimestamp"`
}

// RunningAgents is one page of Agora Conversational AI agents.
type RunningAgents struct {
	// Count is the number of agents in this page.
	Count int `json:"count"`
	// Agents holds the current page, limited by Agora to its default 20 entries.
	Agents []RunningAgent `json:"agents"`
	// Total is the number of matching agents across pages.
	Total int `json:"total"`
	// Cursor is Agora’s cursor for the next page, if any.
	Cursor string `json:"cursor"`
}

// ListRunningAgentsOperation reads one page of running agents.
type ListRunningAgentsOperation struct{ client *Client }

// ListRunningAgents binds the Query to this client.
func (client *Client) ListRunningAgents() ListRunningAgentsOperation {
	return ListRunningAgentsOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (ListRunningAgentsOperation) Definition() sdkgo.QueryDefinition {
	return ListRunningAgentsDefinition
}

// Invoke queries Agora and classifies the provider outcome.
func (operation ListRunningAgentsOperation) Invoke(call sdkgo.Call, _ ListRunningAgentsInput) sdkgo.QueryAttempt[RunningAgents] {
	response := operation.client.listAgents(call)
	if errors.Is(response.err, errCredential) {
		return agentsBranch(ListRunningAgentsBranchDefect, sdkgo.FailureAuthentication, "Agora credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[RunningAgents](agentsFailure(sdkgo.FailureTransport, "Agora request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[RunningAgents](agentsFailure(sdkgo.FailureRateLimit, "Agora rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[RunningAgents](agentsFailure(sdkgo.FailureAvailability, "Agora is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return agentsBranch(ListRunningAgentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Agora response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[RunningAgents](agentsFailure(sdkgo.FailureTransport, "Agora response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return agentsBranch(ListRunningAgentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Agora rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return agentsBranch(ListRunningAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Agora returned invalid UTF-8")
	}
	var wire struct {
		Status string `json:"status"`
		Data   *struct {
			Count int `json:"count"`
			List  []struct {
				// AgentID identifies the running agent within Agora.
				AgentID string `json:"agent_id"`
				// Status is RUNNING for the default list filter.
				Status string `json:"status"`
				// StartTimestamp is the agent creation time in Unix seconds.
				StartTimestamp int64 `json:"start_ts"`
			} `json:"list"`
		} `json:"data"`
		Meta *struct {
			// Cursor is Agora’s cursor for the next page, if any.
			Cursor string `json:"cursor"`
			// Total is the number of matching agents across pages.
			Total int `json:"total"`
		} `json:"meta"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return agentsBranch(ListRunningAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Agora returned invalid agents JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return agentsBranch(ListRunningAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Agora returned trailing JSON")
	}
	if wire.Status != "ok" || wire.Data == nil || wire.Meta == nil || wire.Data.List == nil || wire.Data.Count != len(wire.Data.List) || wire.Data.Count < 0 || wire.Data.Count > 20 || wire.Meta.Total < wire.Data.Count {
		return agentsBranch(ListRunningAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Agora returned inconsistent agent page")
	}
	agents := make([]RunningAgent, 0, len(wire.Data.List))
	for _, agent := range wire.Data.List {
		if agent.AgentID == "" || agent.StartTimestamp <= 0 || agent.Status != "RUNNING" {
			return agentsBranch(ListRunningAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Agora returned invalid running agent")
		}
		agents = append(agents, RunningAgent{AgentID: agent.AgentID, Status: agent.Status, StartTimestamp: agent.StartTimestamp})
	}
	output := RunningAgents{Count: wire.Data.Count, Agents: agents, Total: wire.Meta.Total, Cursor: wire.Meta.Cursor}
	return sdkgo.NewQueryBranch(ListRunningAgentsBranchListed, output, nil, sdkgo.Receipt{})
}
func agentsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "agora", Operation: "listRunningAgents", Message: message}
}
func agentsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[RunningAgents] {
	failure := agentsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, RunningAgents{}, &failure, sdkgo.Receipt{})
}
