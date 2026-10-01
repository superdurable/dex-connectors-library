// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listTimeOffRequestsOperation = "listTimeOffRequests"

	// DefaultTimeOffPageSize is the page size listTimeOffRequests uses when PageSize is zero.
	DefaultTimeOffPageSize = 100
	// MaxTimeOffPageSize is the largest PageSize listTimeOffRequests accepts; BambooHR allows 1000.
	MaxTimeOffPageSize = 200
)

// TimeOffRequestStatus is a BambooHR time off request status as the List Time Off Requests
// endpoint returns it. A request that a later edit replaced is never returned.
type TimeOffRequestStatus string

const (
	// TimeOffRequestStatusRequested is a request awaiting approval.
	TimeOffRequestStatusRequested TimeOffRequestStatus = "REQUESTED"
	// TimeOffRequestStatusApproved is an approved request.
	TimeOffRequestStatusApproved TimeOffRequestStatus = "APPROVED"
	// TimeOffRequestStatusDenied is a denied request.
	TimeOffRequestStatusDenied TimeOffRequestStatus = "DENIED"
	// TimeOffRequestStatusCanceled is a canceled request.
	TimeOffRequestStatusCanceled TimeOffRequestStatus = "CANCELED"
)

// TimeOffRequestStatuses returns BambooHR's four time off request statuses.
func TimeOffRequestStatuses() []TimeOffRequestStatus {
	return []TimeOffRequestStatus{
		TimeOffRequestStatusRequested, TimeOffRequestStatusApproved, TimeOffRequestStatusDenied, TimeOffRequestStatusCanceled,
	}
}

// TimeOffUnit is the unit BambooHR measures a request's amount in, HOURS or DAYS.
type TimeOffUnit string

const (
	// TimeOffUnitHours measures the request in hours.
	TimeOffUnitHours TimeOffUnit = "HOURS"
	// TimeOffUnitDays measures the request in days.
	TimeOffUnitDays TimeOffUnit = "DAYS"
)

// ListTimeOffRequestsInput selects one page of time off requests that overlap a date window.
type ListTimeOffRequestsInput struct {
	// StartDate is the first day of the window, written YYYY-MM-DD. It is required.
	StartDate string `json:"startDate"`
	// EndDate is the last day of the window, written YYYY-MM-DD, on or after StartDate. It is required.
	EndDate string `json:"endDate"`
	// EmployeeID keeps one employee's requests; empty lists every employee the connection can view.
	EmployeeID string `json:"employeeId,omitempty"`
	// Statuses keeps requests in any of these statuses; empty keeps every status.
	Statuses []TimeOffRequestStatus `json:"statuses,omitempty"`
	// Page is the 1-based page to read; zero reads page 1.
	Page int `json:"page,omitempty"`
	// PageSize is 1 to 200 requests per page; zero uses DefaultTimeOffPageSize.
	PageSize int `json:"pageSize,omitempty"`
}

// TimeOffRequest is one time off request. Employee and manager notes are not returned, so free
// text about an absence never enters Step state.
type TimeOffRequest struct {
	// ID is BambooHR's time off request ID.
	ID int64 `json:"id"`
	// EmployeeID is the internal ID of the employee who requested the time off.
	EmployeeID string `json:"employeeId"`
	// CategoryID is BambooHR's time off category (type) ID.
	CategoryID int64 `json:"categoryId"`
	// StartDate is the first day off, written YYYY-MM-DD.
	StartDate string `json:"startDate"`
	// EndDate is the last day off, written YYYY-MM-DD.
	EndDate string `json:"endDate"`
	// Amount is the time requested, measured in Unit.
	Amount float64 `json:"amount"`
	// Unit is HOURS or DAYS.
	Unit TimeOffUnit `json:"unit"`
	// Status is BambooHR's request status.
	Status TimeOffRequestStatus `json:"status"`
	// RequestedAt is when the request was made, in UTC.
	RequestedAt time.Time `json:"requestedAt"`
	// StatusUpdatedAt is when the status last changed, or nil when it never changed.
	StatusUpdatedAt *time.Time `json:"statusUpdatedAt,omitempty"`
	// DailyAmounts is the amount requested on each day, in Unit.
	DailyAmounts []TimeOffDailyAmount `json:"dailyAmounts,omitempty"`
}

// TimeOffDailyAmount is the amount of a request that falls on one day.
type TimeOffDailyAmount struct {
	// Date is the day, written YYYY-MM-DD.
	Date string `json:"date"`
	// Amount is the time requested on that day, in the request's Unit.
	Amount float64 `json:"amount"`
}

// ListTimeOffRequestsOutput is one page of time off requests.
type ListTimeOffRequestsOutput struct {
	// Requests holds the page's requests in BambooHR's default order, newest request first.
	Requests []TimeOffRequest `json:"requests"`
	// Page is the page read.
	Page int `json:"page"`
	// TotalItems is BambooHR's count of matching requests across all pages.
	TotalItems int `json:"totalItems"`
	// NextPage is the page to read next, or zero after the last page.
	NextPage int `json:"nextPage,omitempty"`
}

// ListTimeOffRequestsOperation is the listTimeOffRequests Query.
type ListTimeOffRequestsOperation struct {
	client *Client
}

type timeOffPageWire struct {
	Data []timeOffRequestWire `json:"data"`
	Meta *struct {
		Page       int `json:"page"`
		PageSize   int `json:"pageSize"`
		TotalPages int `json:"totalPages"`
		TotalItems int `json:"totalItems"`
	} `json:"meta"`
}

type timeOffRequestWire struct {
	ID              *int64     `json:"id"`
	EmployeeID      *int64     `json:"employeeId"`
	CategoryID      int64      `json:"categoryId"`
	StartDate       string     `json:"startDate"`
	EndDate         string     `json:"endDate"`
	Amount          float64    `json:"amount"`
	Unit            string     `json:"unit"`
	Status          string     `json:"status"`
	RequestedAt     time.Time  `json:"requestedAt"`
	StatusUpdatedAt *time.Time `json:"statusUpdatedAt"`
	DailyAmounts    []struct {
		Date   string  `json:"date"`
		Amount float64 `json:"amount"`
	} `json:"dailyAmounts"`
}

// Definition returns the immutable connector operation definition.
func (ListTimeOffRequestsOperation) Definition() sdkgo.QueryDefinition {
	return ListTimeOffRequestsDefinition
}

// Invoke reads one page of GET /api/v1/time-off/requests with an OData filter for requests that
// start on or before EndDate and end on or after StartDate.
func (operation ListTimeOffRequestsOperation) Invoke(call sdkgo.Call, input ListTimeOffRequestsInput) sdkgo.QueryAttempt[ListTimeOffRequestsOutput] {
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = DefaultTimeOffPageSize
	}
	if err := validateListTimeOffRequestsInput(input); err != nil {
		return sdkgo.NewQueryBranch(ListTimeOffRequestsBranchDefect, ListTimeOffRequestsOutput{}, bambooHRFailurePointer(sdkgo.FailureValidation, listTimeOffRequestsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, listTimeOffRequestsOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListTimeOffRequestsBranchDefect, ListTimeOffRequestsOutput{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{
		"filter":   {BuildTimeOffRequestFilter(input)},
		"page":     {strconv.Itoa(input.Page)},
		"pageSize": {strconv.Itoa(input.PageSize)},
	}
	result := operation.client.exchange(call, credentials, listTimeOffRequestsOperation, bambooHRRequest{method: http.MethodGet, path: "/time-off/requests", query: query})
	receipt := operation.client.receipt(call, input.EmployeeID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeLimitExceeded, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[ListTimeOffRequestsOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(ListTimeOffRequestsBranchInvalidResponse, ListTimeOffRequestsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(ListTimeOffRequestsBranchDefect, ListTimeOffRequestsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListTimeOffRequestsBranchProviderRejected, ListTimeOffRequestsOutput{}, &result.failure, receipt)
	}
	output, err := decodeTimeOffPage(result.response.body, input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListTimeOffRequestsBranchInvalidResponse, ListTimeOffRequestsOutput{},
			bambooHRFailurePointer(sdkgo.FailureProtocol, listTimeOffRequestsOperation, "BambooHR returned an invalid time off page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListTimeOffRequestsBranchListed, output, nil, receipt)
}

// BuildTimeOffRequestFilter returns the OData filter listTimeOffRequests sends, such as
// startDate le '2026-10-26' and endDate ge '2026-10-13' and employeeId eq 123 and
// status in ('requested', 'approved'). Statuses are written in lowercase, as BambooHR's
// filter examples write them. Every value is validated before it reaches the filter.
func BuildTimeOffRequestFilter(input ListTimeOffRequestsInput) string {
	clauses := []string{"startDate le '" + input.EndDate + "'", "endDate ge '" + input.StartDate + "'"}
	if input.EmployeeID != "" {
		clauses = append(clauses, "employeeId eq "+input.EmployeeID)
	}
	switch len(input.Statuses) {
	case 0:
	case 1:
		clauses = append(clauses, "status eq '"+strings.ToLower(string(input.Statuses[0]))+"'")
	default:
		quoted := make([]string, 0, len(input.Statuses))
		for _, status := range input.Statuses {
			quoted = append(quoted, "'"+strings.ToLower(string(status))+"'")
		}
		clauses = append(clauses, "status in ("+strings.Join(quoted, ", ")+")")
	}
	return strings.Join(clauses, " and ")
}

func validateListTimeOffRequestsInput(input ListTimeOffRequestsInput) error {
	if err := errors.Join(validateCivilDate("startDate", input.StartDate), validateCivilDate("endDate", input.EndDate)); err != nil {
		return err
	}
	if input.EndDate < input.StartDate {
		return errors.New("endDate must be on or after startDate")
	}
	if input.EmployeeID != "" {
		if err := validateEmployeeID("employeeId", input.EmployeeID); err != nil {
			return err
		}
	}
	for _, status := range input.Statuses {
		if !slices.Contains(TimeOffRequestStatuses(), status) {
			return fmt.Errorf("statuses entry %q must be REQUESTED, APPROVED, DENIED, or CANCELED", status)
		}
	}
	if input.Page < 1 || input.Page > math.MaxInt32 {
		return errors.New("page must be a positive page number")
	}
	if input.PageSize < 1 || input.PageSize > MaxTimeOffPageSize {
		return fmt.Errorf("pageSize must be 1 to %d", MaxTimeOffPageSize)
	}
	return nil
}

func decodeTimeOffPage(body []byte, input ListTimeOffRequestsInput) (ListTimeOffRequestsOutput, error) {
	var page timeOffPageWire
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil || page.Meta == nil {
		return ListTimeOffRequestsOutput{}, errors.New("the response is not a time off page")
	}
	if len(page.Data) > input.PageSize || page.Meta.TotalItems < 0 || page.Meta.TotalPages < 0 {
		return ListTimeOffRequestsOutput{}, errors.New("the page size or totals are inconsistent")
	}
	output := ListTimeOffRequestsOutput{Requests: make([]TimeOffRequest, 0, len(page.Data)), Page: input.Page, TotalItems: page.Meta.TotalItems}
	for index, wire := range page.Data {
		request, err := decodeTimeOffRequest(wire)
		if err != nil {
			return ListTimeOffRequestsOutput{}, fmt.Errorf("request %d: %w", index, err)
		}
		output.Requests = append(output.Requests, request)
	}
	if input.Page < page.Meta.TotalPages {
		output.NextPage = input.Page + 1
	}
	return output, nil
}

func decodeTimeOffRequest(wire timeOffRequestWire) (TimeOffRequest, error) {
	if wire.ID == nil || *wire.ID < 1 || wire.EmployeeID == nil || *wire.EmployeeID < 1 {
		return TimeOffRequest{}, errors.New("the request or employee ID is missing")
	}
	if validateCivilDate("startDate", wire.StartDate) != nil || validateCivilDate("endDate", wire.EndDate) != nil {
		return TimeOffRequest{}, errors.New("a date is not YYYY-MM-DD")
	}
	status := TimeOffRequestStatus(strings.ToUpper(wire.Status))
	unit := TimeOffUnit(strings.ToUpper(wire.Unit))
	if !slices.Contains(TimeOffRequestStatuses(), status) || (unit != TimeOffUnitHours && unit != TimeOffUnitDays) {
		return TimeOffRequest{}, errors.New("the status or unit is not one BambooHR documents")
	}
	request := TimeOffRequest{
		ID: *wire.ID, EmployeeID: strconv.FormatInt(*wire.EmployeeID, 10), CategoryID: wire.CategoryID,
		StartDate: wire.StartDate, EndDate: wire.EndDate, Amount: wire.Amount, Unit: unit, Status: status,
		RequestedAt: wire.RequestedAt.UTC(),
	}
	if wire.StatusUpdatedAt != nil {
		updatedAt := wire.StatusUpdatedAt.UTC()
		request.StatusUpdatedAt = &updatedAt
	}
	for _, daily := range wire.DailyAmounts {
		if validateCivilDate("date", daily.Date) != nil {
			return TimeOffRequest{}, errors.New("a daily amount date is not YYYY-MM-DD")
		}
		request.DailyAmounts = append(request.DailyAmounts, TimeOffDailyAmount{Date: daily.Date, Amount: daily.Amount})
	}
	return request, nil
}
