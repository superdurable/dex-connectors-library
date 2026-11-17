// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropboxsign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTemplatesInput selects one bounded page of accessible templates.
type ListTemplatesInput struct {
	// Page is one-based; zero selects the first page.
	Page int `json:"page,omitempty"`
	// PageSize is from 1 to 100; zero selects 20.
	PageSize int `json:"pageSize,omitempty"`
	// Query is an optional search expression, up to 1000 characters.
	Query string `json:"query,omitempty"`
}

// TemplateIdentityInput selects one template by provider ID.
type TemplateIdentityInput struct {
	// TemplateID is an ID returned by ListTemplates.
	TemplateID string `json:"templateId"`
}

// TemplateMetadata contains the fields needed to select a template and assign signers.
type TemplateMetadata struct {
	// ID is the provider template ID.
	ID string `json:"id"`
	// Title is the private template title.
	Title string `json:"title"`
	// SignerRoles are role names required when sending with this template.
	SignerRoles []string `json:"signerRoles"`
}

// TemplatesPage contains one page of accessible templates.
type TemplatesPage struct {
	// Templates contains IDs, titles, and signer roles.
	Templates []TemplateMetadata `json:"templates"`
	// Page is the one-based returned page number.
	Page int `json:"page"`
	// TotalPages is the number of available pages.
	TotalPages int `json:"totalPages"`
}

// DeletedTemplate confirms a template is absent.
type DeletedTemplate struct {
	// TemplateID is the selected template ID.
	TemplateID string `json:"templateId"`
	// Deleted is true after deletion or confirmed absence.
	Deleted bool `json:"deleted"`
}

// ListTemplatesOperation reads one template page.
type ListTemplatesOperation struct{ client *Client }

// GetTemplateOperation reads one template.
type GetTemplateOperation struct{ client *Client }

// DeleteTemplateOperation permanently deletes one template.
type DeleteTemplateOperation struct{ client *Client }

// ListTemplates binds the page Query to this client.
func (client *Client) ListTemplates() ListTemplatesOperation { return ListTemplatesOperation{client} }

// GetTemplate binds the template Query to this client.
func (client *Client) GetTemplate() GetTemplateOperation { return GetTemplateOperation{client} }

// DeleteTemplate binds the deletion Mutation to this client.
func (client *Client) DeleteTemplate() DeleteTemplateOperation {
	return DeleteTemplateOperation{client}
}

// Definition returns the generated template list contract.
func (ListTemplatesOperation) Definition() sdkgo.QueryDefinition { return ListTemplatesDefinition }

// Definition returns the generated template retrieval contract.
func (GetTemplateOperation) Definition() sdkgo.QueryDefinition { return GetTemplateDefinition }

// Definition returns the generated template deletion contract.
func (DeleteTemplateOperation) Definition() sdkgo.MutationDefinition { return DeleteTemplateDefinition }

// IdempotencyKey records the Call ID for repeatable deletion.
func (DeleteTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ TemplateIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one bounded template page.
func (operation ListTemplatesOperation) Invoke(call sdkgo.Call, input ListTemplatesInput) sdkgo.QueryAttempt[TemplatesPage] {
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > 100 || len(input.Query) > 1000 || !utf8.ValidString(input.Query) || strings.ContainsAny(input.Query, "\r\n\x00") {
		return listTemplatesFailure(ListTemplatesBranchDefect, sdkgo.FailureValidation, "template page options are invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "page_size": {strconv.Itoa(input.PageSize)}}
	if input.Query != "" {
		query.Set("query", input.Query)
	}
	response := operation.client.get(call, "/template/list?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return listTemplatesFailure(ListTemplatesBranchDefect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TemplatesPage](signatureExpansionFailure("listTemplates", sdkgo.FailureTransport, "template request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TemplatesPage](signatureExpansionFailure("listTemplates", templateRetryKind(response.status), "template listing is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listTemplatesFailure(ListTemplatesBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listTemplatesFailure(ListTemplatesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "template list exceeded the response limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TemplatesPage](signatureExpansionFailure("listTemplates", sdkgo.FailureTransport, "template response was interrupted"), 0)
	}
	page, err := decodeTemplatesPage(response.body)
	if err != nil {
		return listTemplatesFailure(ListTemplatesBranchInvalidResponse, sdkgo.FailureProtocol, "template page is invalid")
	}
	return sdkgo.NewQueryBranch(ListTemplatesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one template's title and signer roles.
func (operation GetTemplateOperation) Invoke(call sdkgo.Call, input TemplateIdentityInput) sdkgo.QueryAttempt[TemplateMetadata] {
	if !validSignatureRequestID(input.TemplateID) {
		return getTemplateFailure(GetTemplateBranchDefect, sdkgo.FailureValidation, "template ID is invalid")
	}
	response := operation.client.get(call, "/template/"+url.PathEscape(input.TemplateID))
	if errors.Is(response.err, errCredential) {
		return getTemplateFailure(GetTemplateBranchDefect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TemplateMetadata](signatureExpansionFailure("getTemplate", sdkgo.FailureTransport, "template request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TemplateMetadata](signatureExpansionFailure("getTemplate", templateRetryKind(response.status), "template retrieval is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return getTemplateFailure(GetTemplateBranchNotFound, sdkgo.FailureNotFound, "template is unavailable")
	}
	if response.status != http.StatusOK {
		return getTemplateFailure(GetTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return getTemplateFailure(GetTemplateBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "template exceeded the response limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TemplateMetadata](signatureExpansionFailure("getTemplate", sdkgo.FailureTransport, "template response was interrupted"), 0)
	}
	var wire struct {
		Template json.RawMessage `json:"template"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return getTemplateFailure(GetTemplateBranchInvalidResponse, sdkgo.FailureProtocol, "template JSON is invalid")
	}
	template, err := decodeTemplateMetadata(wire.Template)
	if err != nil || template.ID != input.TemplateID {
		return getTemplateFailure(GetTemplateBranchInvalidResponse, sdkgo.FailureProtocol, "template identity is invalid")
	}
	return sdkgo.NewQueryBranch(GetTemplateBranchFound, template, nil, sdkgo.Receipt{})
}

// Invoke permanently deletes one template; an absent template is already deleted.
func (operation DeleteTemplateOperation) Invoke(call sdkgo.Call, input TemplateIdentityInput) sdkgo.MutationAttempt[DeletedTemplate] {
	if !validSignatureRequestID(input.TemplateID) {
		return deleteTemplateFailure(DeleteTemplateBranchDefect, sdkgo.FailureValidation, "template ID is invalid")
	}
	response := operation.client.send(call, http.MethodPost, "/template/delete/"+url.PathEscape(input.TemplateID), nil, "")
	if errors.Is(response.err, errCredential) {
		return deleteTemplateFailure(DeleteTemplateBranchDefect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeletedTemplate](signatureExpansionFailure("deleteTemplate", sdkgo.FailureTransport, "template deletion request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedTemplate](signatureExpansionFailure("deleteTemplate", templateRetryKind(response.status), "template deletion is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusNotFound {
		return deleteTemplateFailure(DeleteTemplateBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[DeletedTemplate](signatureExpansionFailure("deleteTemplate", sdkgo.FailureTransport, "template deletion response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteTemplateBranchDeleted, DeletedTemplate{TemplateID: input.TemplateID, Deleted: true}, nil, sdkgo.Receipt{})
}

func decodeTemplatesPage(body []byte) (TemplatesPage, error) {
	var wire struct {
		Templates []json.RawMessage `json:"templates"`
		ListInfo  struct {
			Page     int `json:"page"`
			NumPages int `json:"num_pages"`
		} `json:"list_info"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TemplatesPage{}, err
	}
	if wire.Templates == nil || wire.ListInfo.Page < 1 || wire.ListInfo.NumPages < 0 {
		return TemplatesPage{}, fmt.Errorf("template page is incomplete")
	}
	page := TemplatesPage{Templates: make([]TemplateMetadata, 0, len(wire.Templates)), Page: wire.ListInfo.Page, TotalPages: wire.ListInfo.NumPages}
	for _, raw := range wire.Templates {
		template, err := decodeTemplateMetadata(raw)
		if err != nil {
			return TemplatesPage{}, err
		}
		page.Templates = append(page.Templates, template)
	}
	return page, nil
}

func decodeTemplateMetadata(body []byte) (TemplateMetadata, error) {
	var wire struct {
		ID          string `json:"template_id"`
		Title       string `json:"title"`
		SignerRoles []struct {
			Name string `json:"name"`
		} `json:"signer_roles"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TemplateMetadata{}, err
	}
	if !validSignatureRequestID(wire.ID) || strings.TrimSpace(wire.Title) == "" || wire.SignerRoles == nil {
		return TemplateMetadata{}, fmt.Errorf("template identity or roles are incomplete")
	}
	template := TemplateMetadata{ID: wire.ID, Title: wire.Title, SignerRoles: make([]string, 0, len(wire.SignerRoles))}
	for _, role := range wire.SignerRoles {
		if !validSignerName(role.Name) {
			return TemplateMetadata{}, fmt.Errorf("template signer role is invalid")
		}
		template.SignerRoles = append(template.SignerRoles, role.Name)
	}
	return template, nil
}

func templateRetryKind(status int) sdkgo.FailureKind {
	if status == http.StatusTooManyRequests {
		return sdkgo.FailureRateLimit
	}
	return sdkgo.FailureAvailability
}
func listTemplatesFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TemplatesPage] {
	failure := signatureExpansionFailure("listTemplates", kind, message)
	return sdkgo.NewQueryBranch(branch, TemplatesPage{}, &failure, sdkgo.Receipt{})
}
func getTemplateFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TemplateMetadata] {
	failure := signatureExpansionFailure("getTemplate", kind, message)
	return sdkgo.NewQueryBranch(branch, TemplateMetadata{}, &failure, sdkgo.Receipt{})
}
func deleteTemplateFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedTemplate] {
	failure := signatureExpansionFailure("deleteTemplate", kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedTemplate{}, &failure, sdkgo.Receipt{})
}
