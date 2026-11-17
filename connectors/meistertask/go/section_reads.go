// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SectionSummary contains the non-secret section identity and status.
type SectionSummary struct {
	// ID is the positive MeisterTask section ID.
	ID string `json:"id"`
	// Name is the section name.
	Name string `json:"name"`
	// Status is the provider's decimal status code.
	Status string `json:"status"`
	// ProjectID is the positive parent project ID.
	ProjectID string `json:"projectId"`
}

// SectionsPage contains one ordered page of section summaries.
type SectionsPage struct {
	// Sections are the sections visible to the token owner.
	Sections []SectionSummary `json:"sections"`
	// Page is the requested one-based page number.
	Page int `json:"page"`
}

// ListSectionsInput selects one page of sections.
type ListSectionsInput struct {
	// Page is one-based; zero selects the first page.
	Page int `json:"page"`
	// Items is the positive page size; zero uses the provider default.
	Items int `json:"items"`
	// Status is active, trashed, or all; blank uses active.
	Status string `json:"status"`
}

// GetSectionInput identifies one section.
type GetSectionInput struct {
	// SectionID is a positive decimal ID from MeisterTask Sections.
	SectionID string `json:"sectionId"`
}

// ListSectionsOperation reads visible sections.
type ListSectionsOperation struct{ client *Client }

// ListSections binds the Query to this client.
func (client *Client) ListSections() ListSectionsOperation { return ListSectionsOperation{client} }

// Definition returns the generated section-list Query contract.
func (ListSectionsOperation) Definition() sdkgo.QueryDefinition { return ListSectionsDefinition }

// Invoke requests one section page and sections only non-secret fields.
func (operation ListSectionsOperation) Invoke(call sdkgo.Call, input ListSectionsInput) sdkgo.QueryAttempt[SectionsPage] {
	if input.Page < 0 || input.Items < 0 || input.Items > 100 || (input.Status != "" && input.Status != "active" && input.Status != "trashed" && input.Status != "all") {
		return sectionQueryBranch("listSections", ListSectionsBranchDefect, SectionsPage{}, sdkgo.FailureValidation, "section page, size, or status is invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"page": {strconv.Itoa(page)}}
	if input.Items > 0 {
		query.Set("items", strconv.Itoa(input.Items))
	}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	response := operation.client.get(call, "/sections?"+query.Encode())
	if decision, handled := classifySectionQueryResponse("listSections", response, SectionsPage{}, "", ListSectionsBranchProviderRejected, ListSectionsBranchInvalidResponse, ListSectionsBranchDefect); handled {
		return decision
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(response.body, &wire); err != nil || wire == nil {
		return sectionQueryBranch("listSections", ListSectionsBranchInvalidResponse, SectionsPage{}, sdkgo.FailureProtocol, "MeisterTask returned invalid section list JSON")
	}
	sections := make([]SectionSummary, 0, len(wire))
	for _, raw := range wire {
		section, err := decodeSection(raw, "")
		if err != nil {
			return sectionQueryBranch("listSections", ListSectionsBranchInvalidResponse, SectionsPage{}, sdkgo.FailureProtocol, "MeisterTask returned incomplete section metadata")
		}
		sections = append(sections, section)
	}
	return sdkgo.NewQueryBranch(ListSectionsBranchListed, SectionsPage{Sections: sections, Page: page}, nil, sdkgo.Receipt{})
}

// GetSectionOperation reads one section.
type GetSectionOperation struct{ client *Client }

// GetSection binds the Query to this client.
func (client *Client) GetSection() GetSectionOperation { return GetSectionOperation{client} }

// Definition returns the generated section Query contract.
func (GetSectionOperation) Definition() sdkgo.QueryDefinition { return GetSectionDefinition }

// Invoke reads a section by ID.
func (operation GetSectionOperation) Invoke(call sdkgo.Call, input GetSectionInput) sdkgo.QueryAttempt[SectionSummary] {
	if !validTaskID(input.SectionID) {
		return sectionQueryBranch("getSection", GetSectionBranchDefect, SectionSummary{}, sdkgo.FailureValidation, "section ID must be a positive 32-bit decimal integer")
	}
	response := operation.client.get(call, "/sections/"+input.SectionID)
	if decision, handled := classifySectionQueryResponse("getSection", response, SectionSummary{}, GetSectionBranchNotFound, GetSectionBranchProviderRejected, GetSectionBranchInvalidResponse, GetSectionBranchDefect); handled {
		return decision
	}
	section, err := decodeSection(response.body, input.SectionID)
	if err != nil {
		return sectionQueryBranch("getSection", GetSectionBranchInvalidResponse, SectionSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid section JSON")
	}
	return sdkgo.NewQueryBranch(GetSectionBranchFound, section, nil, sdkgo.Receipt{})
}

func decodeSection(body []byte, requestedID string) (SectionSummary, error) {
	var wire struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Status    *int64 `json:"status"`
		ProjectID int64  `json:"project_id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SectionSummary{}, err
	}
	identifier := strconv.FormatInt(wire.ID, 10)
	if !validTaskID(identifier) || (requestedID != "" && requestedID != identifier) || wire.Name == "" || wire.Status == nil || (*wire.Status != 1 && *wire.Status != 2) || !validTaskID(strconv.FormatInt(wire.ProjectID, 10)) {
		return SectionSummary{}, errors.New("section identity, name, or status is invalid")
	}
	return SectionSummary{ID: identifier, Name: wire.Name, Status: strconv.FormatInt(*wire.Status, 10), ProjectID: strconv.FormatInt(wire.ProjectID, 10)}, nil
}

func classifySectionQueryResponse[Output any](operation string, response providerResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return sectionQueryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](sectionFailure(operation, sdkgo.FailureTransport, "MeisterTask request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](sectionFailure(operation, sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](sectionFailure(operation, sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return sectionQueryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "MeisterTask did not expose the section"), true
	}
	if response.status != http.StatusOK {
		return sectionQueryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sectionQueryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](sectionFailure(operation, sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func sectionFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: operation, Message: message}
}

func sectionQueryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := sectionFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
