// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postmark

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Template is a Postmark server template with its stable provider identity.
type Template struct {
	// TemplateID is the Postmark numeric template identity.
	TemplateID int64 `json:"TemplateId"`
	// Name is the server-visible template name.
	Name string `json:"Name"`
	// Alias is the optional stable template alias.
	Alias string `json:"Alias"`
	// Active indicates that the template can be used to send email.
	Active bool `json:"Active"`
	// Subject is the standard-template subject markup when returned.
	Subject string `json:"Subject,omitempty"`
	// TextBody is the plain-text markup when returned.
	TextBody string `json:"TextBody,omitempty"`
	// HtmlBody is the HTML markup when returned.
	HtmlBody string `json:"HtmlBody,omitempty"`
}

// TemplatePage contains one bounded page of server templates.
type TemplatePage struct {
	// TotalCount is the count across all pages.
	TotalCount int `json:"TotalCount"`
	// Templates contains the requested page.
	Templates []Template `json:"Templates"`
}

// ListTemplatesInput chooses a template page.
type ListTemplatesInput struct {
	// Count is the page size; zero selects 50.
	Count int `json:"count,omitempty"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset,omitempty"`
}

// GetTemplateInput identifies a template by numeric ID or alias.
type GetTemplateInput struct {
	// TemplateIDOrAlias is the server template ID or alias.
	TemplateIDOrAlias string `json:"templateIdOrAlias"`
}

// CreateTemplateInput describes one standard transactional template.
type CreateTemplateInput struct {
	// Name is the server-visible template name.
	Name string `json:"name"`
	// Subject is the template subject markup.
	Subject string `json:"subject"`
	// TextBody is plain-text markup; either it or HtmlBody is required.
	TextBody string `json:"textBody,omitempty"`
	// HtmlBody is HTML markup; either it or TextBody is required.
	HtmlBody string `json:"htmlBody,omitempty"`
}

// UpdateTemplateInput replaces the editable fields of one standard template.
type UpdateTemplateInput struct {
	// TemplateIDOrAlias identifies the existing template.
	TemplateIDOrAlias string `json:"templateIdOrAlias"`
	// Name is the complete replacement name.
	Name string `json:"name"`
	// Subject is the complete replacement subject markup.
	Subject string `json:"subject"`
	// TextBody is complete plain-text markup; either it or HtmlBody is required.
	TextBody string `json:"textBody,omitempty"`
	// HtmlBody is complete HTML markup; either it or TextBody is required.
	HtmlBody string `json:"htmlBody,omitempty"`
	// Alias optionally replaces the template alias; blank retains the current alias.
	Alias string `json:"alias,omitempty"`
}

// DeleteTemplateInput identifies a template to remove.
type DeleteTemplateInput struct {
	// TemplateIDOrAlias is the provider template ID or alias.
	TemplateIDOrAlias string `json:"templateIdOrAlias"`
}

// DeletedTemplate confirms that the template is absent.
type DeletedTemplate struct {
	// Deleted is true when Postmark deleted the template or it was already absent.
	Deleted bool `json:"deleted"`
}

// ListTemplatesOperation lists server templates.
type ListTemplatesOperation struct{ client *Client }

// GetTemplateOperation retrieves one server template.
type GetTemplateOperation struct{ client *Client }

// UpdateTemplateOperation replaces one server template.
type UpdateTemplateOperation struct{ client *Client }

// DeleteTemplateOperation removes one server template.
type DeleteTemplateOperation struct{ client *Client }

// ListTemplates binds the list operation to this client.
func (client *Client) ListTemplates() ListTemplatesOperation { return ListTemplatesOperation{client} }

// GetTemplate binds the detail operation to this client.
func (client *Client) GetTemplate() GetTemplateOperation { return GetTemplateOperation{client} }

// UpdateTemplate binds the replacement operation to this client.
func (client *Client) UpdateTemplate() UpdateTemplateOperation {
	return UpdateTemplateOperation{client}
}

// DeleteTemplate binds the delete operation to this client.
func (client *Client) DeleteTemplate() DeleteTemplateOperation {
	return DeleteTemplateOperation{client}
}

// Definition returns the generated listTemplates contract.
func (ListTemplatesOperation) Definition() sdkgo.QueryDefinition { return ListTemplatesDefinition }

// Definition returns the generated getTemplate contract.
func (GetTemplateOperation) Definition() sdkgo.QueryDefinition { return GetTemplateDefinition }

// Definition returns the generated updateTemplate contract.
func (UpdateTemplateOperation) Definition() sdkgo.MutationDefinition { return UpdateTemplateDefinition }

// Definition returns the generated deleteTemplate contract.
func (DeleteTemplateOperation) Definition() sdkgo.MutationDefinition { return DeleteTemplateDefinition }

// IdempotencyKey identifies one absolute replacement.
func (UpdateTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies one repeat-safe delete.
func (DeleteTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke requests a bounded template page.
func (operation ListTemplatesOperation) Invoke(call sdkgo.Call, input ListTemplatesInput) sdkgo.QueryAttempt[TemplatePage] {
	if input.Count == 0 {
		input.Count = 50
	}
	if input.Count < 1 || input.Count > 500 || input.Offset < 0 {
		return queryFailure[TemplatePage]("listTemplates", ListTemplatesBranchDefect, sdkgo.FailureValidation, "count must be 1 through 500 and offset nonnegative")
	}
	query := url.Values{"count": {strconv.Itoa(input.Count)}, "offset": {strconv.Itoa(input.Offset)}}
	return runQuery(operation.client, call, "listTemplates", "/templates?"+query.Encode(), ListTemplatesBranchListed, "", ListTemplatesBranchProviderRejected, ListTemplatesBranchInvalidResponse, ListTemplatesBranchDefect, func(body []byte) (TemplatePage, error) {
		var page TemplatePage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Templates == nil {
			err = fmt.Errorf("templates missing")
		}
		return page, err
	})
}

// Invoke retrieves one template by ID or alias.
func (operation GetTemplateOperation) Invoke(call sdkgo.Call, input GetTemplateInput) sdkgo.QueryAttempt[Template] {
	if strings.TrimSpace(input.TemplateIDOrAlias) == "" {
		return queryFailure[Template]("getTemplate", GetTemplateBranchDefect, sdkgo.FailureValidation, "template ID or alias is required")
	}
	return runQuery(operation.client, call, "getTemplate", "/templates/"+url.PathEscape(input.TemplateIDOrAlias), GetTemplateBranchFound, "", GetTemplateBranchProviderRejected, GetTemplateBranchInvalidResponse, GetTemplateBranchDefect, decodeTemplate)
}

// Invoke replaces a standard template's editable fields.
func (operation UpdateTemplateOperation) Invoke(call sdkgo.Call, input UpdateTemplateInput) sdkgo.MutationAttempt[Template] {
	if strings.TrimSpace(input.TemplateIDOrAlias) == "" || !validTemplateContent(input.Name, input.Subject, input.TextBody, input.HtmlBody) || input.Alias != "" && !templateAliasPattern.MatchString(input.Alias) {
		return mutationFailure[Template]("updateTemplate", UpdateTemplateBranchDefect, sdkgo.FailureValidation, "template ID, name, subject, and a body are required; alias must start with a letter and contain only letters, digits, periods, underscores, or hyphens")
	}
	requestFields := map[string]string{"Name": input.Name, "Subject": input.Subject, "TextBody": input.TextBody, "HtmlBody": input.HtmlBody}
	if input.Alias != "" {
		requestFields["Alias"] = input.Alias
	}
	body, err := json.Marshal(requestFields)
	if err != nil {
		return mutationFailure[Template]("updateTemplate", UpdateTemplateBranchDefect, sdkgo.FailureLocalDefect, "template request cannot be encoded")
	}
	return runMutation(operation.client, call, "updateTemplate", http.MethodPut, "/templates/"+url.PathEscape(input.TemplateIDOrAlias), body, "", "", UpdateTemplateBranchUpdated, "", UpdateTemplateBranchProviderRejected, UpdateTemplateBranchInvalidResponse, UpdateTemplateBranchDefect, decodeTemplate)
}

// Invoke removes a template; a missing template is already deleted.
func (operation DeleteTemplateOperation) Invoke(call sdkgo.Call, input DeleteTemplateInput) sdkgo.MutationAttempt[DeletedTemplate] {
	if strings.TrimSpace(input.TemplateIDOrAlias) == "" {
		return mutationFailure[DeletedTemplate]("deleteTemplate", DeleteTemplateBranchDefect, sdkgo.FailureValidation, "template ID or alias is required")
	}
	return operation.client.deleteTemplate(call, input.TemplateIDOrAlias)
}

func decodeTemplate(body []byte) (Template, error) {
	var template Template
	err := json.Unmarshal(body, &template)
	if err == nil && template.TemplateID <= 0 {
		err = fmt.Errorf("template ID missing")
	}
	return template, err
}

func validTemplateContent(name, subject, textBody, htmlBody string) bool {
	return strings.TrimSpace(name) != "" && strings.TrimSpace(subject) != "" && (strings.TrimSpace(textBody) != "" || strings.TrimSpace(htmlBody) != "")
}

var templateAliasPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)

func (client *Client) deleteTemplate(call sdkgo.Call, templateIDOrAlias string) sdkgo.MutationAttempt[DeletedTemplate] {
	request, err := client.newRequest(call, http.MethodDelete, "/templates/"+url.PathEscape(templateIDOrAlias), nil)
	if err != nil {
		return mutationFailure[DeletedTemplate]("deleteTemplate", DeleteTemplateBranchDefect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedTemplate](newFailure("deleteTemplate", sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[DeletedTemplate](newFailure("deleteTemplate", sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[DeletedTemplate](newFailure("deleteTemplate", sdkgo.FailureAvailability, "provider outcome is unknown"), 0)
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusUnprocessableEntity && response.Header.Get("X-PM-ApiErrorCode") == "1101":
		return sdkgo.NewMutationBranch(DeleteTemplateBranchDeleted, DeletedTemplate{Deleted: true}, nil, sdkgo.Receipt{})
	case response.StatusCode != http.StatusOK:
		return mutationFailure[DeletedTemplate]("deleteTemplate", DeleteTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected template deletion")
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedTemplate](newFailure("deleteTemplate", sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	var result struct {
		ErrorCode int `json:"ErrorCode"`
	}
	if err := json.Unmarshal(content, &result); err != nil || result.ErrorCode != 0 || len(content) == 0 {
		return mutationFailure[DeletedTemplate]("deleteTemplate", DeleteTemplateBranchDefect, sdkgo.FailureProtocol, "provider response is invalid")
	}
	return sdkgo.NewMutationBranch(DeleteTemplateBranchDeleted, DeletedTemplate{Deleted: true}, nil, sdkgo.Receipt{})
}
