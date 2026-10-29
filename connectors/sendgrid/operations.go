// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetStatsInput selects a global email statistics date range.
type GetStatsInput struct {
	// StartDate is the first day in YYYY-MM-DD format.
	StartDate string `json:"startDate"`
	// EndDate is the final day in YYYY-MM-DD format; blank means today.
	EndDate string `json:"endDate,omitempty"`
}

// StatsDay gives the requested and delivered counts for one date.
type StatsDay struct {
	// Date is the reporting date in YYYY-MM-DD format.
	Date string `json:"date"`
	// Requests is the number of email requests.
	Requests int `json:"requests"`
	// Delivered is the number confirmed delivered.
	Delivered int `json:"delivered"`
}

// StatsOutput contains daily global email statistics.
type StatsOutput struct {
	// Days is the returned date series.
	Days []StatsDay `json:"days"`
}

// ListTemplatesInput chooses a page of transactional templates.
type ListTemplatesInput struct {
	// Generation is dynamic or legacy; blank selects dynamic.
	Generation string `json:"generation,omitempty"`
	// PageSize is 1 to 200 templates; zero selects 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the token from the preceding page; blank starts at page one.
	PageToken string `json:"pageToken,omitempty"`
}

// Template identifies a transactional template.
type Template struct {
	// ID is the provider template ID.
	ID string `json:"id"`
	// Name is the human-readable template name.
	Name string `json:"name"`
	// Generation is legacy or dynamic.
	Generation string `json:"generation"`
}

// TemplatesOutput holds the returned template page.
type TemplatesOutput struct {
	// Templates is the returned page of templates.
	Templates []Template `json:"templates"`
	// Count is the provider-reported count; its precise meaning is unverified.
	Count int `json:"count"`
	// NextPageToken selects the next page when the provider supplies one.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// GetStatsOperation reads global email statistics.
type GetStatsOperation struct{ client *Client }

// ListTemplatesOperation reads transactional templates.
type ListTemplatesOperation struct{ client *Client }

// GetStats binds this query to the client.
func (client *Client) GetStats() GetStatsOperation { return GetStatsOperation{client} }

// ListTemplates binds this query to the client.
func (client *Client) ListTemplates() ListTemplatesOperation { return ListTemplatesOperation{client} }

// Definition returns the generated statistics contract.
func (GetStatsOperation) Definition() sdkgo.QueryDefinition { return GetStatsDefinition }

// Definition returns the generated templates contract.
func (ListTemplatesOperation) Definition() sdkgo.QueryDefinition { return ListTemplatesDefinition }

// Invoke requests statistics for an inclusive date range.
func (operation GetStatsOperation) Invoke(call sdkgo.Call, input GetStatsInput) sdkgo.QueryAttempt[StatsOutput] {
	branches := queryBranches{found: GetStatsBranchFound, rejected: GetStatsBranchProviderRejected, invalid: GetStatsBranchInvalidResponse, defect: GetStatsBranchDefect}
	start, err := time.Parse(time.DateOnly, input.StartDate)
	if err != nil {
		return queryBranch("getStats", branches.defect, StatsOutput{}, sdkgo.FailureValidation, "startDate must be YYYY-MM-DD")
	}
	query := url.Values{"start_date": {input.StartDate}}
	if input.EndDate != "" {
		end, err := time.Parse(time.DateOnly, input.EndDate)
		if err != nil || end.Before(start) {
			return queryBranch("getStats", branches.defect, StatsOutput{}, sdkgo.FailureValidation, "endDate must be YYYY-MM-DD and not before startDate")
		}
		query.Set("end_date", input.EndDate)
	}
	return invokeJSONQuery(call, operation.client, "getStats", branches, http.MethodGet, "/v3/stats", query, nil, decodeStats)
}

// Invoke lists transactional templates of one generation.
func (operation ListTemplatesOperation) Invoke(call sdkgo.Call, input ListTemplatesInput) sdkgo.QueryAttempt[TemplatesOutput] {
	branches := queryBranches{found: ListTemplatesBranchListed, rejected: ListTemplatesBranchProviderRejected, invalid: ListTemplatesBranchInvalidResponse, defect: ListTemplatesBranchDefect}
	if input.Generation == "" {
		input.Generation = "dynamic"
	}
	if input.Generation != "dynamic" && input.Generation != "legacy" {
		return queryBranch("listTemplates", branches.defect, TemplatesOutput{}, sdkgo.FailureValidation, "generation must be dynamic or legacy")
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	if input.PageSize < 1 || input.PageSize > 200 {
		return queryBranch("listTemplates", branches.defect, TemplatesOutput{}, sdkgo.FailureValidation, "pageSize must be 1 to 200")
	}
	query := url.Values{"generations": {input.Generation}, "page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	return invokeJSONQuery(call, operation.client, "listTemplates", branches, http.MethodGet, "/v3/templates", query, nil, decodeTemplates)
}

func decodeStats(body []byte) (StatsOutput, error) {
	var wire []struct {
		Date  string `json:"date"`
		Stats []struct {
			Metrics struct {
				Requests  int `json:"requests"`
				Delivered int `json:"delivered"`
			} `json:"metrics"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return StatsOutput{}, err
	}
	if wire == nil {
		return StatsOutput{}, fmt.Errorf("statistics array missing")
	}
	output := StatsOutput{Days: make([]StatsDay, 0, len(wire))}
	for _, day := range wire {
		if day.Date == "" || len(day.Stats) == 0 {
			return StatsOutput{}, fmt.Errorf("statistics day incomplete")
		}
		var requests, delivered int
		for _, category := range day.Stats {
			requests += category.Metrics.Requests
			delivered += category.Metrics.Delivered
		}
		output.Days = append(output.Days, StatsDay{Date: day.Date, Requests: requests, Delivered: delivered})
	}
	return output, nil
}

func decodeTemplates(body []byte) (TemplatesOutput, error) {
	var wire struct {
		Result   []Template `json:"result"`
		Metadata struct {
			Count int    `json:"count"`
			Next  string `json:"next"`
		} `json:"_metadata"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TemplatesOutput{}, err
	}
	if wire.Result == nil {
		return TemplatesOutput{}, fmt.Errorf("template result missing")
	}
	output := TemplatesOutput{Templates: wire.Result, Count: wire.Metadata.Count}
	if wire.Metadata.Next != "" {
		nextPageURL, err := url.Parse(wire.Metadata.Next)
		if err != nil || nextPageURL.Scheme != "https" && nextPageURL.Scheme != "http" {
			return TemplatesOutput{}, fmt.Errorf("template next-page URL invalid")
		}
		output.NextPageToken = nextPageURL.Query().Get("page_token")
		if output.NextPageToken == "" {
			return TemplatesOutput{}, fmt.Errorf("template next-page token missing")
		}
	}
	return output, nil
}
