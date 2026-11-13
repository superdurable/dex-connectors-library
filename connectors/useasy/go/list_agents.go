// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package useasy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const agentsPath = "/openapi/platform/v1/agent/list"

// ListAgentsInput selects a one-based agent page; zero values use page 1 and size 20.
type ListAgentsInput struct {
	// Page is the one-based page number.
	Page int `json:"page"`
	// PageSize is the number of agents requested, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// AgentSummary excludes passwords and personal contact details returned by the provider.
type AgentSummary struct {
	// ID is the provider's stable agent ID.
	ID string `json:"id"`
	// Number is the agent's work number.
	Number string `json:"number"`
	// Enabled reports whether the agent is active.
	Enabled bool `json:"enabled"`
}

// ListAgentsOutput contains one page of agent metadata and the provider's page counts.
type ListAgentsOutput struct {
	// Agents are returned in provider order.
	Agents []AgentSummary `json:"agents"`
	// Page is the one-based page number returned by Useasy.
	Page int `json:"page"`
	// Total is the number of matching agents.
	Total int `json:"total"`
	// TotalPages is the number of available pages.
	TotalPages int `json:"totalPages"`
}

// ListAgentsOperation reads Useasy agent metadata.
type ListAgentsOperation struct{ client *Client }

// Definition returns the generated list-agent Query contract.
func (ListAgentsOperation) Definition() sdkgo.QueryDefinition { return ListAgentsDefinition }

// Invoke lists agents and classifies provider and transport outcomes.
func (operation ListAgentsOperation) Invoke(call sdkgo.Call, input ListAgentsInput) sdkgo.QueryAttempt[ListAgentsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppID(credentials.AppID.Reveal()) || !isValidSecret(credentials.SecretKey.Reveal()) {
		return agentBranch(ListAgentsBranchDefect, sdkgo.FailureAuthentication, "Useasy credentials are unavailable or invalid")
	}
	page, pageSize := input.Page, input.PageSize
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || page > 50000 || pageSize < 1 || pageSize > 100 {
		return agentBranch(ListAgentsBranchDefect, sdkgo.FailureValidation, "page or pageSize is outside the supported range")
	}
	var nonceBytes [4]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return agentRetry(sdkgo.FailureAvailability, "request nonce could not be generated", 0)
	}
	nonce := strconv.Itoa(100000 + int(uint32(nonceBytes[0])<<24|uint32(nonceBytes[1])<<16|uint32(nonceBytes[2])<<8|uint32(nonceBytes[3]))%900000)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	payload, _ := json.Marshal(struct {
		Page     int `json:"page"`
		PageSize int `json:"pageSize"`
	}{page, pageSize})
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+agentsPath, strings.NewReader(string(payload)))
	if err != nil {
		return agentBranch(ListAgentsBranchDefect, sdkgo.FailureValidation, "agent-list request is invalid")
	}
	request.Header.Set("Content-Type", "application/json;charset=utf-8")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("appid", credentials.AppID.Reveal())
	request.Header.Set("nonce", nonce)
	request.Header.Set("timestamp", timestamp)
	request.Header.Set("signature", signAgentRequest(credentials.AppID.Reveal(), credentials.SecretKey.Reveal(), timestamp, nonce))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return agentRetry(sdkgo.FailureAvailability, "Useasy request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return agentRetry(sdkgo.FailureRateLimit, "Useasy rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return agentRetry(sdkgo.FailureAvailability, "Useasy temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return agentBranch(ListAgentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "agent response exceeded the configured limit")
	}
	if err != nil {
		return agentRetry(sdkgo.FailureAvailability, "agent response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return agentBranch(ListAgentsBranchProviderRejected, kind, "Useasy rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    *struct {
			List []struct {
				ID     string `json:"_id"`
				Number string `json:"agentNumber"`
				Status *int   `json:"status"`
			} `json:"list"`
			Page struct {
				PageSize   int `json:"pageSize"`
				PageNumber int `json:"pageNumber"`
				TotalPage  int `json:"totalPage"`
				Total      int `json:"total"`
			} `json:"page"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&wire) != nil || wire.Code == "" {
		return agentBranch(ListAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Useasy returned invalid agent data")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return agentBranch(ListAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Useasy returned trailing agent data")
	}
	if !wire.Success || wire.Code != "200" {
		return agentBranch(ListAgentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Useasy rejected the agent-list request, code "+wire.Code)
	}
	if wire.Data == nil || wire.Data.List == nil || wire.Data.Page.PageNumber != page || wire.Data.Page.PageSize <= 0 || wire.Data.Page.Total < 0 || wire.Data.Page.TotalPage < 0 || len(wire.Data.List) > pageSize {
		return agentBranch(ListAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Useasy returned invalid agent page")
	}
	agents := make([]AgentSummary, 0, len(wire.Data.List))
	for _, item := range wire.Data.List {
		if item.ID == "" || len(item.ID) > 128 || item.Number == "" || len(item.Number) > 128 || item.Status == nil || (*item.Status != 0 && *item.Status != 1) {
			return agentBranch(ListAgentsBranchInvalidResponse, sdkgo.FailureProtocol, "Useasy returned invalid agent metadata")
		}
		agents = append(agents, AgentSummary{ID: item.ID, Number: item.Number, Enabled: *item.Status == 1})
	}
	return sdkgo.NewQueryBranch(ListAgentsBranchListed, ListAgentsOutput{Agents: agents, Page: page, Total: wire.Data.Page.Total, TotalPages: wire.Data.Page.TotalPage}, nil, sdkgo.Receipt{})
}

func signAgentRequest(appID, secret, timestamp, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(appID + timestamp + nonce))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func isValidAppID(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func isValidSecret(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func agentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListAgentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "useasy", Operation: "listAgents", Message: message}
	return sdkgo.NewQueryBranch(branch, ListAgentsOutput{}, &failure, sdkgo.Receipt{})
}
func agentRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListAgentsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "useasy", Operation: "listAgents", Message: message}
	return sdkgo.NewQueryRetry[ListAgentsOutput](failure, delay)
}
