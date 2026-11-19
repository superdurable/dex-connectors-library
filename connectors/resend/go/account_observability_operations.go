// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetUsageInput selects the current account's usage counters.
type GetUsageInput struct {
	// Period is all, daily, or monthly; blank means all.
	Period string `json:"period,omitempty"`
}

// ListLogsInput selects one page of API request logs.
type ListLogsInput struct {
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive log cursor.
	After string `json:"after,omitempty"`
}

// GetLogInput selects one API request log.
type GetLogInput struct {
	// ID is the log ID.
	ID string `json:"id"`
}

// UsageCounters reports message counts in one period.
type UsageCounters struct {
	// Used is sent plus received messages.
	Used int `json:"used"`
	// Limit is nil when the period has no configured limit.
	Limit *int `json:"limit"`
	// Sent is the number of outbound messages.
	Sent int `json:"sent"`
	// Received is the number of inbound messages.
	Received int `json:"received"`
	// ResetsAt is the daily counter reset timestamp, when present.
	ResetsAt string `json:"resets_at,omitempty"`
}

// AccountUsage contains daily and monthly email usage.
type AccountUsage struct {
	// Emails contains email message counters.
	Emails struct {
		// Daily reports the current day's counters.
		Daily UsageCounters `json:"daily"`
		// Monthly reports the billing period's counters.
		Monthly UsageCounters `json:"monthly"`
	} `json:"emails"`
}

// RequestLog contains non-sensitive API request metadata only.
type RequestLog struct {
	// ID is the log ID.
	ID string `json:"id"`
	// CreatedAt is the request timestamp.
	CreatedAt string `json:"created_at"`
	// Endpoint is the called API endpoint.
	Endpoint string `json:"endpoint"`
	// Method is the HTTP verb.
	Method string `json:"method"`
	// ResponseStatus is the HTTP status code.
	ResponseStatus int `json:"response_status"`
	// UserAgent identifies the requesting client when available.
	UserAgent *string `json:"user_agent"`
}

// RequestLogPage contains one log page.
type RequestLogPage struct {
	// Data contains safe request metadata.
	Data []RequestLog `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// GetUsageOperation retrieves account usage.
type GetUsageOperation struct{ client *Client }

// ListLogsOperation lists API request metadata.
type ListLogsOperation struct{ client *Client }

// GetLogOperation retrieves API request metadata.
type GetLogOperation struct{ client *Client }

// GetUsage binds account usage retrieval to this client.
func (client *Client) GetUsage() GetUsageOperation { return GetUsageOperation{client} }

// ListLogs binds log listing to this client.
func (client *Client) ListLogs() ListLogsOperation { return ListLogsOperation{client} }

// GetLog binds log retrieval to this client.
func (client *Client) GetLog() GetLogOperation { return GetLogOperation{client} }

// Definition returns the generated getUsage contract.
func (GetUsageOperation) Definition() sdkgo.QueryDefinition { return GetUsageDefinition }

// Definition returns the generated listLogs contract.
func (ListLogsOperation) Definition() sdkgo.QueryDefinition { return ListLogsDefinition }

// Definition returns the generated getLog contract.
func (GetLogOperation) Definition() sdkgo.QueryDefinition { return GetLogDefinition }

// Invoke retrieves the account's current usage counters.
func (operation GetUsageOperation) Invoke(call sdkgo.Call, input GetUsageInput) sdkgo.QueryAttempt[AccountUsage] {
	if input.Period != "" && input.Period != "all" && input.Period != "daily" && input.Period != "monthly" {
		return queryFailure[AccountUsage]("getUsage", GetUsageBranchDefect, sdkgo.FailureValidation, "period must be all, daily, or monthly")
	}
	decode := func(body []byte) (AccountUsage, error) {
		usage, err := decodeAccountUsage(body)
		if err != nil {
			return AccountUsage{}, err
		}
		if input.Period == "daily" {
			usage.Emails.Monthly = UsageCounters{}
		} else if input.Period == "monthly" {
			usage.Emails.Daily = UsageCounters{}
		}
		return usage, nil
	}
	return runQuery(operation.client, call, "getUsage", "/usage", GetUsageBranchFound, "", GetUsageBranchProviderRejected, GetUsageBranchInvalidResponse, GetUsageBranchDefect, decode)
}

// Invoke lists log metadata without request or response bodies.
func (operation ListLogsOperation) Invoke(call sdkgo.Call, input ListLogsInput) sdkgo.QueryAttempt[RequestLogPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[RequestLogPage]("listLogs", ListLogsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listLogs", "/logs"+query, ListLogsBranchListed, "", ListLogsBranchProviderRejected, ListLogsBranchInvalidResponse, ListLogsBranchDefect, decodeRequestLogPage)
}

// Invoke retrieves log metadata without request or response bodies.
func (operation GetLogOperation) Invoke(call sdkgo.Call, input GetLogInput) sdkgo.QueryAttempt[RequestLog] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[RequestLog]("getLog", GetLogBranchDefect, sdkgo.FailureValidation, "log ID is required")
	}
	return runQuery(operation.client, call, "getLog", "/logs/"+url.PathEscape(input.ID), GetLogBranchFound, GetLogBranchNotFound, GetLogBranchProviderRejected, GetLogBranchInvalidResponse, GetLogBranchDefect, decodeRequestLog)
}

func decodeAccountUsage(body []byte) (AccountUsage, error) {
	var envelope struct {
		Object string `json:"object"`
		AccountUsage
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Object != "usage" {
		return AccountUsage{}, fmt.Errorf("invalid usage response")
	}
	return envelope.AccountUsage, nil
}

func decodeRequestLogPage(body []byte) (RequestLogPage, error) {
	var page RequestLogPage
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil {
		return RequestLogPage{}, fmt.Errorf("invalid logs response")
	}
	return page, nil
}

func decodeRequestLog(body []byte) (RequestLog, error) {
	var log RequestLog
	if err := json.Unmarshal(body, &log); err != nil || log.ID == "" {
		return RequestLog{}, fmt.Errorf("invalid log response")
	}
	return log, nil
}
