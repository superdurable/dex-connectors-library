// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TemplateVariable declares one reusable template value.
type TemplateVariable struct {
	// Key is the placeholder name.
	Key string `json:"key"`
	// Type is string, number, boolean, object, or list.
	Type string `json:"type"`
	// FallbackValue is optional JSON used when a send omits this variable.
	FallbackValue json.RawMessage `json:"fallback_value,omitempty"`
}

// CreateTemplateInput creates an unpublished email template.
type CreateTemplateInput struct {
	// Name is the template name used to confirm an uncertain create.
	Name string `json:"name"`
	// Alias is an optional stable lookup name.
	Alias string `json:"alias,omitempty"`
	// From is an optional verified sender.
	From string `json:"from,omitempty"`
	// Subject is an optional subject.
	Subject string `json:"subject,omitempty"`
	// ReplyTo contains optional reply addresses.
	ReplyTo []string `json:"replyTo,omitempty"`
	// HTML is the required HTML body.
	HTML string `json:"html"`
	// Text is the optional plain-text body.
	Text string `json:"text,omitempty"`
	// Variables declares optional placeholders.
	Variables []TemplateVariable `json:"variables,omitempty"`
}

// GetTemplateInput selects a template by ID or alias.
type GetTemplateInput struct {
	// ID is the template ID or alias.
	ID string `json:"id"`
}

// ListTemplatesInput selects one template page.
type ListTemplatesInput struct {
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive template cursor.
	After string `json:"after,omitempty"`
}

// UpdateTemplateInput replaces selected absolute template fields.
type UpdateTemplateInput struct {
	// ID is the template ID or alias.
	ID string `json:"id"`
	// Name replaces the template name when non-nil.
	Name *string `json:"name,omitempty"`
	// Alias replaces the lookup alias when non-nil.
	Alias *string `json:"alias,omitempty"`
	// From replaces the verified sender when non-nil.
	From *string `json:"from,omitempty"`
	// Subject replaces the subject when non-nil.
	Subject *string `json:"subject,omitempty"`
	// ReplyTo replaces reply addresses when non-nil.
	ReplyTo *[]string `json:"replyTo,omitempty"`
	// HTML replaces the HTML body when non-nil.
	HTML *string `json:"html,omitempty"`
	// Text replaces the plain-text body when non-nil.
	Text *string `json:"text,omitempty"`
	// Variables replaces all placeholders when non-nil.
	Variables *[]TemplateVariable `json:"variables,omitempty"`
}

// DeleteTemplateInput selects a template for deletion.
type DeleteTemplateInput struct {
	// ID is the template ID or alias.
	ID string `json:"id"`
}

// PublishTemplateInput selects a draft for publication.
type PublishTemplateInput struct {
	// ID is the template ID or alias.
	ID string `json:"id"`
}

// DuplicateTemplateInput selects a template to copy.
type DuplicateTemplateInput struct {
	// ID is the source template ID or alias.
	ID string `json:"id"`
}

// TemplateReference identifies a created or changed template.
type TemplateReference struct {
	// ID is the stable provider template ID.
	ID string `json:"id"`
}

// Template contains safe template metadata without message bodies or fallbacks.
type Template struct {
	// ID is the template ID.
	ID string `json:"id"`
	// Name is the template name.
	Name string `json:"name"`
	// Alias is the optional lookup alias.
	Alias string `json:"alias"`
	// From is the optional verified sender.
	From string `json:"from"`
	// Subject is the optional subject.
	Subject string `json:"subject"`
	// Status is draft or published.
	Status string `json:"status"`
	// CurrentVersionID is the active version ID.
	CurrentVersionID string `json:"current_version_id"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is the most recent update timestamp.
	UpdatedAt string `json:"updated_at"`
	// PublishedAt is the optional publication timestamp.
	PublishedAt string `json:"published_at"`
	// HasUnpublishedVersions reports whether a draft differs from the published version.
	HasUnpublishedVersions bool `json:"has_unpublished_versions"`
}

// TemplatePage contains one template page.
type TemplatePage struct {
	// Data contains safe template metadata.
	Data []Template `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedTemplate records a deletion acknowledgement.
type DeletedTemplate struct {
	// ID is the deleted template ID.
	ID string `json:"id"`
	// Deleted confirms the deletion.
	Deleted bool `json:"deleted"`
}

// CreateTemplateOperation creates a draft with bounded resend.
type CreateTemplateOperation struct{ client *Client }

// GetTemplateOperation retrieves template metadata.
type GetTemplateOperation struct{ client *Client }

// ListTemplatesOperation lists template metadata.
type ListTemplatesOperation struct{ client *Client }

// UpdateTemplateOperation changes absolute template fields.
type UpdateTemplateOperation struct{ client *Client }

// DeleteTemplateOperation removes a template.
type DeleteTemplateOperation struct{ client *Client }

// PublishTemplateOperation publishes a draft.
type PublishTemplateOperation struct{ client *Client }

// DuplicateTemplateOperation copies a template with bounded resend.
type DuplicateTemplateOperation struct{ client *Client }

// CreateTemplate binds template creation to this client.
func (client *Client) CreateTemplate() CreateTemplateOperation {
	return CreateTemplateOperation{client}
}

// GetTemplate binds template retrieval to this client.
func (client *Client) GetTemplate() GetTemplateOperation { return GetTemplateOperation{client} }

// ListTemplates binds template listing to this client.
func (client *Client) ListTemplates() ListTemplatesOperation { return ListTemplatesOperation{client} }

// UpdateTemplate binds template updates to this client.
func (client *Client) UpdateTemplate() UpdateTemplateOperation {
	return UpdateTemplateOperation{client}
}

// DeleteTemplate binds template deletion to this client.
func (client *Client) DeleteTemplate() DeleteTemplateOperation {
	return DeleteTemplateOperation{client}
}

// PublishTemplate binds template publication to this client.
func (client *Client) PublishTemplate() PublishTemplateOperation {
	return PublishTemplateOperation{client}
}

// DuplicateTemplate binds template duplication to this client.
func (client *Client) DuplicateTemplate() DuplicateTemplateOperation {
	return DuplicateTemplateOperation{client}
}

// Definition returns the generated createTemplate contract.
func (CreateTemplateOperation) Definition() sdkgo.MutationDefinition { return CreateTemplateDefinition }

// Definition returns the generated getTemplate contract.
func (GetTemplateOperation) Definition() sdkgo.QueryDefinition { return GetTemplateDefinition }

// Definition returns the generated listTemplates contract.
func (ListTemplatesOperation) Definition() sdkgo.QueryDefinition { return ListTemplatesDefinition }

// Definition returns the generated updateTemplate contract.
func (UpdateTemplateOperation) Definition() sdkgo.MutationDefinition { return UpdateTemplateDefinition }

// Definition returns the generated deleteTemplate contract.
func (DeleteTemplateOperation) Definition() sdkgo.MutationDefinition { return DeleteTemplateDefinition }

// Definition returns the generated publishTemplate contract.
func (PublishTemplateOperation) Definition() sdkgo.MutationDefinition {
	return PublishTemplateDefinition
}

// Definition returns the generated duplicateTemplate contract.
func (DuplicateTemplateOperation) Definition() sdkgo.MutationDefinition {
	return DuplicateTemplateDefinition
}

// IdempotencyKey correlates possible draft creation attempts.
func (CreateTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute updates.
func (UpdateTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates publication attempts.
func (PublishTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates possible duplication attempts.
func (DuplicateTemplateOperation) IdempotencyKey(callID sdkgo.CallID, _ DuplicateTemplateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one unpublished template.
func (operation CreateTemplateOperation) Invoke(call sdkgo.Call, input CreateTemplateInput) sdkgo.MutationAttempt[TemplateReference] {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HTML) == "" {
		return mutationFailure[TemplateReference]("createTemplate", CreateTemplateBranchDefect, sdkgo.FailureValidation, "template name and HTML body are required")
	}
	fields := map[string]any{"name": input.Name, "html": input.HTML}
	if input.Alias != "" {
		fields["alias"] = input.Alias
	}
	if input.From != "" {
		fields["from"] = input.From
	}
	if input.Subject != "" {
		fields["subject"] = input.Subject
	}
	if len(input.ReplyTo) > 0 {
		fields["reply_to"] = input.ReplyTo
	}
	if input.Text != "" {
		fields["text"] = input.Text
	}
	if len(input.Variables) > 0 {
		fields["variables"] = input.Variables
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[TemplateReference]("createTemplate", CreateTemplateBranchDefect, sdkgo.FailureValidation, "template cannot be encoded")
	}
	request := resendLimitedCreate[TemplateReference]{client: operation.client, call: call, operation: "createTemplate", path: "/templates", body: body, timeout: CreateTemplateDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateTemplateBranchCreated, rejected: CreateTemplateBranchProviderRejected, defect: CreateTemplateBranchDefect, decode: decodeTemplateReference, readBack: func(call sdkgo.Call) (TemplateReference, readBackState) {
		return operation.readBackTemplate(call, input)
	}}
	return request.invoke()
}

// Invoke retrieves one template's metadata.
func (operation GetTemplateOperation) Invoke(call sdkgo.Call, input GetTemplateInput) sdkgo.QueryAttempt[Template] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Template]("getTemplate", GetTemplateBranchDefect, sdkgo.FailureValidation, "template ID is required")
	}
	return runQuery(operation.client, call, "getTemplate", "/templates/"+url.PathEscape(input.ID), GetTemplateBranchFound, GetTemplateBranchNotFound, GetTemplateBranchProviderRejected, GetTemplateBranchInvalidResponse, GetTemplateBranchDefect, decodeTemplate)
}

// Invoke lists one template page.
func (operation ListTemplatesOperation) Invoke(call sdkgo.Call, input ListTemplatesInput) sdkgo.QueryAttempt[TemplatePage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[TemplatePage]("listTemplates", ListTemplatesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listTemplates", "/templates"+query, ListTemplatesBranchListed, "", ListTemplatesBranchProviderRejected, ListTemplatesBranchInvalidResponse, ListTemplatesBranchDefect, decodeTemplatePage)
}

// Invoke replaces selected absolute draft fields.
func (operation UpdateTemplateOperation) Invoke(call sdkgo.Call, input UpdateTemplateInput) sdkgo.MutationAttempt[TemplateReference] {
	if strings.TrimSpace(input.ID) == "" || (input.Name == nil && input.Alias == nil && input.From == nil && input.Subject == nil && input.ReplyTo == nil && input.HTML == nil && input.Text == nil && input.Variables == nil) {
		return mutationFailure[TemplateReference]("updateTemplate", UpdateTemplateBranchDefect, sdkgo.FailureValidation, "template ID and at least one field are required")
	}
	fields := map[string]any{}
	for key, value := range map[string]*string{"name": input.Name, "alias": input.Alias, "from": input.From, "subject": input.Subject, "html": input.HTML, "text": input.Text} {
		if value != nil {
			fields[key] = *value
		}
	}
	if input.ReplyTo != nil {
		fields["reply_to"] = *input.ReplyTo
	}
	if input.Variables != nil {
		fields["variables"] = *input.Variables
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[TemplateReference]("updateTemplate", UpdateTemplateBranchDefect, sdkgo.FailureValidation, "template cannot be encoded")
	}
	return runMutation(operation.client, call, "updateTemplate", http.MethodPatch, "/templates/"+url.PathEscape(input.ID), body, "", "", UpdateTemplateBranchUpdated, UpdateTemplateBranchNotFound, UpdateTemplateBranchProviderRejected, UpdateTemplateBranchInvalidResponse, UpdateTemplateBranchDefect, decodeTemplateReference)
}

// Invoke deletes one template.
func (operation DeleteTemplateOperation) Invoke(call sdkgo.Call, input DeleteTemplateInput) sdkgo.MutationAttempt[DeletedTemplate] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedTemplate]("deleteTemplate", DeleteTemplateBranchDefect, sdkgo.FailureValidation, "template ID is required")
	}
	return runMutation(operation.client, call, "deleteTemplate", http.MethodDelete, "/templates/"+url.PathEscape(input.ID), nil, "", "", DeleteTemplateBranchDeleted, DeleteTemplateBranchNotFound, DeleteTemplateBranchProviderRejected, DeleteTemplateBranchInvalidResponse, DeleteTemplateBranchDefect, decodeDeletedTemplate)
}

// Invoke publishes a template draft.
func (operation PublishTemplateOperation) Invoke(call sdkgo.Call, input PublishTemplateInput) sdkgo.MutationAttempt[TemplateReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[TemplateReference]("publishTemplate", PublishTemplateBranchDefect, sdkgo.FailureValidation, "template ID is required")
	}
	return runMutation(operation.client, call, "publishTemplate", http.MethodPost, "/templates/"+url.PathEscape(input.ID)+"/publish", nil, "", "", PublishTemplateBranchPublished, PublishTemplateBranchNotFound, PublishTemplateBranchProviderRejected, PublishTemplateBranchInvalidResponse, PublishTemplateBranchDefect, decodeTemplateReference)
}

// Invoke duplicates a template into a new draft.
func (operation DuplicateTemplateOperation) Invoke(call sdkgo.Call, input DuplicateTemplateInput) sdkgo.MutationAttempt[TemplateReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[TemplateReference]("duplicateTemplate", DuplicateTemplateBranchDefect, sdkgo.FailureValidation, "template ID is required")
	}
	request := resendLimitedCreate[TemplateReference]{client: operation.client, call: call, operation: "duplicateTemplate", path: "/templates/" + url.PathEscape(input.ID) + "/duplicate", timeout: DuplicateTemplateDefinition.StepDefaults.ExecuteMethodTimeout, created: DuplicateTemplateBranchDuplicated, rejected: DuplicateTemplateBranchProviderRejected, defect: DuplicateTemplateBranchDefect, decode: decodeTemplateReference, readBack: func(sdkgo.Call) (TemplateReference, readBackState) { return TemplateReference{}, readBackAbsent }}
	return request.invoke()
}

func (operation CreateTemplateOperation) readBackTemplate(call sdkgo.Call, input CreateTemplateInput) (TemplateReference, readBackState) {
	cursor := ""
	var confirmed TemplateReference
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/templates"+query)
		if state != readBackMatched {
			return TemplateReference{}, readBackRetry
		}
		page, err := decodeTemplatePage(body)
		if err != nil {
			return TemplateReference{}, readBackRetry
		}
		for _, candidate := range page.Data {
			if candidate.Name != input.Name || (input.Alias != "" && candidate.Alias != input.Alias) {
				continue
			}
			detailBody, detailState := operation.client.readBackResource(call, "/templates/"+url.PathEscape(candidate.ID))
			if detailState != readBackMatched {
				return TemplateReference{}, readBackRetry
			}
			var detail struct {
				HTML      string             `json:"html"`
				Text      string             `json:"text"`
				From      string             `json:"from"`
				Subject   string             `json:"subject"`
				ReplyTo   []string           `json:"reply_to"`
				Variables []TemplateVariable `json:"variables"`
			}
			if json.Unmarshal(detailBody, &detail) != nil {
				return TemplateReference{}, readBackRetry
			}
			if detail.HTML == input.HTML && detail.Text == input.Text && detail.From == input.From && detail.Subject == input.Subject && slices.Equal(detail.ReplyTo, input.ReplyTo) && templateVariablesEqual(detail.Variables, input.Variables) {
				if confirmed.ID != "" {
					return TemplateReference{}, readBackConflicted
				}
				confirmed = TemplateReference{ID: candidate.ID}
			}
		}
		if !page.HasMore {
			if confirmed.ID != "" {
				return confirmed, readBackMatched
			}
			return TemplateReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return TemplateReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return TemplateReference{}, readBackRetry
}

func templateVariablesEqual(actual, expected []TemplateVariable) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index].Key != expected[index].Key || actual[index].Type != expected[index].Type {
			return false
		}
		if len(actual[index].FallbackValue) == 0 && len(expected[index].FallbackValue) == 0 {
			continue
		}
		var actualFallback, expectedFallback any
		if json.Unmarshal(actual[index].FallbackValue, &actualFallback) != nil || json.Unmarshal(expected[index].FallbackValue, &expectedFallback) != nil || !reflect.DeepEqual(actualFallback, expectedFallback) {
			return false
		}
	}
	return true
}

func decodeTemplateReference(body []byte) (TemplateReference, error) {
	var reference TemplateReference
	if err := json.Unmarshal(body, &reference); err != nil || reference.ID == "" {
		return TemplateReference{}, fmt.Errorf("invalid template reference")
	}
	return reference, nil
}
func decodeTemplate(body []byte) (Template, error) {
	var template Template
	if err := json.Unmarshal(body, &template); err != nil || template.ID == "" {
		return Template{}, fmt.Errorf("invalid template response")
	}
	return template, nil
}
func decodeTemplatePage(body []byte) (TemplatePage, error) {
	var page TemplatePage
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil {
		return TemplatePage{}, fmt.Errorf("invalid template page")
	}
	return page, nil
}
func decodeDeletedTemplate(body []byte) (DeletedTemplate, error) {
	var deleted DeletedTemplate
	if err := json.Unmarshal(body, &deleted); err != nil || deleted.ID == "" || !deleted.Deleted {
		return DeletedTemplate{}, fmt.Errorf("invalid template deletion response")
	}
	return deleted, nil
}
