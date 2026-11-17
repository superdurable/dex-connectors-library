// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	getEmailTemplateOperationID   = "getEmailTemplate"
	listEmailTemplatesOperationID = "listEmailTemplates"
)

// GetEmailTemplateInput names one regional SES template.
type GetEmailTemplateInput struct {
	// TemplateName is the exact template name in the SES Console.
	TemplateName string `json:"templateName"`
}

// GetEmailTemplateOperation implements the template read.
type GetEmailTemplateOperation struct{ client *Client }

// GetEmailTemplate binds the Query to this client.
func (client *Client) GetEmailTemplate() GetEmailTemplateOperation {
	return GetEmailTemplateOperation{client}
}

// Definition returns the generated Query contract.
func (GetEmailTemplateOperation) Definition() sdkgo.QueryDefinition {
	return GetEmailTemplateDefinition
}

// Invoke reads the subject and text/HTML bodies of one stored template.
func (operation GetEmailTemplateOperation) Invoke(call sdkgo.Call, input GetEmailTemplateInput) sdkgo.QueryAttempt[EmailTemplate] {
	if err := validateTemplateName(input.TemplateName); err != nil {
		return templateQueryBranch[EmailTemplate](getEmailTemplateOperationID, GetEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	response := operation.client.request(call, http.MethodGet, "/v2/email/templates/"+url.PathEscape(input.TemplateName), nil)
	if attempt, isFailure := templateQueryFailure[EmailTemplate](getEmailTemplateOperationID, response, GetEmailTemplateBranchMissing, GetEmailTemplateBranchProviderRejected, GetEmailTemplateBranchInvalidResponse, GetEmailTemplateBranchDefect); isFailure {
		return attempt
	}
	value, err := decodeEmailTemplate(input.TemplateName, response.body)
	if err != nil {
		return templateQueryBranch[EmailTemplate](getEmailTemplateOperationID, GetEmailTemplateBranchInvalidResponse, sdkgo.FailureProtocol, err.Error())
	}
	return sdkgo.NewQueryBranch(GetEmailTemplateBranchFound, value, nil, sdkgo.Receipt{})
}

// ListEmailTemplatesInput requests one bounded page of regional template names.
type ListEmailTemplatesInput struct {
	// NextToken resumes a previous page; blank starts at the beginning.
	NextToken string `json:"nextToken,omitempty"`
	// PageSize is 1 to 100; zero uses 10.
	PageSize int `json:"pageSize,omitempty"`
}

// EmailTemplateSummary identifies one template and its creation time.
type EmailTemplateSummary struct {
	// Name is the template name.
	Name string `json:"name"`
	// CreatedAt is the SES creation timestamp.
	CreatedAt time.Time `json:"createdAt"`
}

// EmailTemplatePage is one bounded page and its continuation token.
type EmailTemplatePage struct {
	// Templates holds at most PageSize entries.
	Templates []EmailTemplateSummary `json:"templates"`
	// NextToken continues the listing; blank means the final page.
	NextToken string `json:"nextToken,omitempty"`
}

// ListEmailTemplatesOperation implements regional template listing.
type ListEmailTemplatesOperation struct{ client *Client }

// ListEmailTemplates binds the Query to this client.
func (client *Client) ListEmailTemplates() ListEmailTemplatesOperation {
	return ListEmailTemplatesOperation{client}
}

// Definition returns the generated Query contract.
func (ListEmailTemplatesOperation) Definition() sdkgo.QueryDefinition {
	return ListEmailTemplatesDefinition
}

// Invoke lists one page without placing any template body in the Result.
func (operation ListEmailTemplatesOperation) Invoke(call sdkgo.Call, input ListEmailTemplatesInput) sdkgo.QueryAttempt[EmailTemplatePage] {
	if input.PageSize == 0 {
		input.PageSize = 10
	}
	if input.PageSize < 1 || input.PageSize > 100 || len(input.NextToken) > 4096 || strings.ContainsAny(input.NextToken, "\r\n") {
		return templateQueryBranch[EmailTemplatePage](listEmailTemplatesOperationID, ListEmailTemplatesBranchDefect, sdkgo.FailureValidation, "SES template page size or token is invalid")
	}
	query := url.Values{"PageSize": {strconv.Itoa(input.PageSize)}}
	if input.NextToken != "" {
		query.Set("NextToken", input.NextToken)
	}
	response := operation.client.request(call, http.MethodGet, "/v2/email/templates?"+query.Encode(), nil)
	if attempt, isFailure := templateQueryFailure[EmailTemplatePage](listEmailTemplatesOperationID, response, "", ListEmailTemplatesBranchProviderRejected, ListEmailTemplatesBranchInvalidResponse, ListEmailTemplatesBranchDefect); isFailure {
		return attempt
	}
	var wire struct {
		NextToken         string `json:"NextToken"`
		TemplatesMetadata []struct {
			TemplateName     string  `json:"TemplateName"`
			CreatedTimestamp float64 `json:"CreatedTimestamp"`
		} `json:"TemplatesMetadata"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.TemplatesMetadata == nil || len(wire.TemplatesMetadata) > input.PageSize || len(wire.NextToken) > 4096 {
		return templateQueryBranch[EmailTemplatePage](listEmailTemplatesOperationID, ListEmailTemplatesBranchInvalidResponse, sdkgo.FailureProtocol, "SES returned invalid template list JSON")
	}
	page := EmailTemplatePage{Templates: make([]EmailTemplateSummary, 0, len(wire.TemplatesMetadata)), NextToken: wire.NextToken}
	for _, item := range wire.TemplatesMetadata {
		if validateTemplateName(item.TemplateName) != nil || item.CreatedTimestamp <= 0 || item.CreatedTimestamp > 1e11 {
			return templateQueryBranch[EmailTemplatePage](listEmailTemplatesOperationID, ListEmailTemplatesBranchInvalidResponse, sdkgo.FailureProtocol, "SES returned invalid template metadata")
		}
		page.Templates = append(page.Templates, EmailTemplateSummary{Name: item.TemplateName, CreatedAt: time.Unix(int64(item.CreatedTimestamp), 0).UTC()})
	}
	return sdkgo.NewQueryBranch(ListEmailTemplatesBranchPage, page, nil, sdkgo.Receipt{})
}

func templateQueryFailure[T any](operationID string, response providerResponse, missing, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return templateQueryBranch[T](operationID, defect, sdkgo.FailureAuthentication, "SES credentials are unavailable or response repeated a credential"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](sesFailure(operationID, sdkgo.FailureTransport, "SES template request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewQueryRetry[T](sesFailure(operationID, kind, "SES template request is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && missing != "" {
		return sdkgo.NewQueryBranch(missing, *new(T), nil, sdkgo.Receipt{}), true
	}
	if response.status != http.StatusOK {
		return templateQueryBranch[T](operationID, rejected, sdkgo.FailureProviderRejection, "SES rejected template request HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return templateQueryBranch[T](operationID, invalid, sdkgo.FailureResponseTooLarge, "SES template response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](sesFailure(operationID, sdkgo.FailureTransport, "SES template response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func templateQueryBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sesFailure(operationID, kind, message)
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}
