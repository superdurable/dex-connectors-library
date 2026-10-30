// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// maximumFormPageSize is the largest page_size GET /forms accepts.
	maximumFormPageSize = 200
	// maximumFormPage bounds the page number a Flow can request.
	maximumFormPage = 10000
	// maximumFormSearchBytes bounds the search text sent to Typeform.
	maximumFormSearchBytes = 256
	// maximumFieldNestingDepth bounds how deep group fields can nest in a form definition.
	maximumFieldNestingDepth = 4
)

// ListFormsInput selects one page of the account's forms.
type ListFormsInput struct {
	// Search returns only forms whose title contains this text; blank lists every form.
	Search string `json:"search,omitempty"`
	// WorkspaceID lists only the forms of one workspace, such as the ID in
	// https://admin.typeform.com/accounts/<account>/workspaces/<workspaceId>; blank lists every workspace.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// Page is the 1-based page number; zero reads the first page.
	Page int `json:"page,omitempty"`
	// PageSize is 1 to 200 forms per page; zero uses Typeform's default of 10.
	PageSize int `json:"pageSize,omitempty"`
}

// FormPage is one page of the account's forms.
type FormPage struct {
	// Forms are the forms on this page, in Typeform's order.
	Forms []FormSummary `json:"forms"`
	// Page is the page number that was read.
	Page int `json:"page"`
	// PageCount is how many pages the listing has.
	PageCount int `json:"pageCount"`
	// TotalItems is how many forms match the request.
	TotalItems int `json:"totalItems"`
	// NextPage is the page number to read next, or zero on the last page.
	NextPage int `json:"nextPage,omitempty"`
}

// FormSummary describes one form in a listing.
type FormSummary struct {
	// ID is the form ID, the part after /to/ in the form link.
	ID string `json:"id"`
	// Title is the form's title.
	Title string `json:"title"`
	// IsPublic is false for a form that does not accept responses from its public link.
	IsPublic bool `json:"isPublic"`
	// DisplayURL is the public link respondents open.
	DisplayURL string `json:"displayUrl,omitempty"`
	// CreatedAt is when the form was created, in UTC.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// LastUpdatedAt is when the form last changed, in UTC.
	LastUpdatedAt time.Time `json:"lastUpdatedAt,omitzero"`
}

// GetFormInput names the form to read.
type GetFormInput struct {
	// FormID is the form ID, such as the formPicker unit's formId or u6nXL7 from
	// https://form.typeform.com/to/u6nXL7.
	FormID string `json:"formId"`
}

// Form is one form's question structure.
type Form struct {
	// ID is the form ID.
	ID string `json:"id"`
	// Title is the form's title.
	Title string `json:"title"`
	// Language is the form's language code, such as en.
	Language string `json:"language,omitempty"`
	// Fields lists every question in form order. A group's questions follow the group, with
	// GroupFieldID set to the group's ID.
	Fields []FormField `json:"fields"`
	// HiddenFields are the names of the form's hidden fields, which each FormResponse's HiddenFields keys.
	HiddenFields []string `json:"hiddenFields,omitempty"`
	// DisplayURL is the public link respondents open.
	DisplayURL string `json:"displayUrl,omitempty"`
}

// FormField is one question of a form.
type FormField struct {
	// ID is the field ID that every FormAnswer's FieldID names.
	ID string `json:"id"`
	// Ref is the field's ref. A ref set through the Create API is stable; Typeform documents the ref it
	// generates for a form built in its editor as non-persistent, so match such answers by ID.
	Ref string `json:"ref,omitempty"`
	// Title is the question text, which can hold recall placeholders in double braces.
	Title string `json:"title"`
	// Type is the question type, such as short_text, email, multiple_choice, or group.
	Type string `json:"type"`
	// IsRequired is true when the respondent must answer the question.
	IsRequired bool `json:"isRequired"`
	// GroupFieldID is the ID of the group or contact_info field that contains this question; blank at the
	// top level.
	GroupFieldID string `json:"groupFieldId,omitempty"`
	// Choices lists the answer choices of a choice question.
	Choices []FormFieldChoice `json:"choices,omitempty"`
}

// FormFieldChoice is one answer choice of a question.
type FormFieldChoice struct {
	// ID is the choice ID.
	ID string `json:"id,omitempty"`
	// Ref is the choice ref.
	Ref string `json:"ref,omitempty"`
	// Label is the choice text.
	Label string `json:"label"`
}

// ListFormsOperation implements the listForms Query. Build it with Client.ListForms.
type ListFormsOperation struct{ client *Client }

// GetFormOperation implements the getForm Query. Build it with Client.GetForm.
type GetFormOperation struct{ client *Client }

type typeformFormLinks struct {
	Display string `json:"display"`
}

type typeformFormListItem struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	CreatedAt     string `json:"created_at"`
	LastUpdatedAt string `json:"last_updated_at"`
	Settings      struct {
		IsPublic *bool `json:"is_public"`
	} `json:"settings"`
	Links typeformFormLinks `json:"_links"`
}

type typeformFormList struct {
	TotalItems *int                   `json:"total_items"`
	PageCount  *int                   `json:"page_count"`
	Items      []typeformFormListItem `json:"items"`
}

type typeformForm struct {
	ID       string              `json:"id"`
	Title    string              `json:"title"`
	Language string              `json:"language"`
	Fields   []typeformFormField `json:"fields"`
	Hidden   []string            `json:"hidden"`
	Links    typeformFormLinks   `json:"_links"`
}

type typeformFormField struct {
	ID         string `json:"id"`
	Ref        string `json:"ref"`
	Title      string `json:"title"`
	Type       string `json:"type"`
	Properties struct {
		Choices []typeformFormFieldChoice `json:"choices"`
		Fields  []typeformFormField       `json:"fields"`
	} `json:"properties"`
	Validations struct {
		Required bool `json:"required"`
	} `json:"validations"`
}

type typeformFormFieldChoice struct {
	ID    string `json:"id"`
	Ref   string `json:"ref"`
	Label string `json:"label"`
}

// Definition returns the immutable listForms operation definition.
func (ListFormsOperation) Definition() sdkgo.QueryDefinition {
	return ListFormsDefinition
}

// Invoke reads GET /forms for one page.
func (operation ListFormsOperation) Invoke(call sdkgo.Call, input ListFormsInput) sdkgo.QueryAttempt[FormPage] {
	operationID := ListFormsDefinition.Operation.OperationID
	client := operation.client
	query, page, err := input.query()
	if err != nil {
		return queryAttempt[FormPage](client, validationOutcome(operationID, err), "")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return queryAttempt[FormPage](client, credentialOutcome(operationID, err), "")
	}
	response, err := client.sendTypeformRequest(call.Context, credentials, http.MethodGet, "/forms", query, nil)
	branches := failureBranches{providerRejected: ListFormsBranchProviderRejected, invalidResponse: ListFormsBranchInvalidResponse}
	if outcome, isTerminal := classifyExchange(client, operationID, response, err, branches); isTerminal {
		return queryAttempt[FormPage](client, outcome, "")
	}
	var decoded typeformFormList
	if err := decodeTypeformJSON(response.body, &decoded); err != nil {
		return queryAttempt[FormPage](client, malformedOutcome(operationID, ListFormsBranchInvalidResponse), "")
	}
	formPage, err := decoded.convert(page)
	if err != nil {
		return queryAttempt[FormPage](client, malformedOutcome(operationID, ListFormsBranchInvalidResponse), "")
	}
	return sdkgo.NewQueryBranch(ListFormsBranchListed, formPage, nil, client.typeformReceipt(""))
}

// Definition returns the immutable getForm operation definition.
func (GetFormOperation) Definition() sdkgo.QueryDefinition {
	return GetFormDefinition
}

// Invoke reads GET /forms/{form_id} and flattens group questions after their group.
func (operation GetFormOperation) Invoke(call sdkgo.Call, input GetFormInput) sdkgo.QueryAttempt[Form] {
	operationID := GetFormDefinition.Operation.OperationID
	client := operation.client
	if err := validateFormID(input.FormID); err != nil {
		return queryAttempt[Form](client, validationOutcome(operationID, err), "")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return queryAttempt[Form](client, credentialOutcome(operationID, err), "")
	}
	response, err := client.sendTypeformRequest(call.Context, credentials, http.MethodGet, "/forms/"+input.FormID, nil, nil)
	branches := failureBranches{
		notFound: GetFormBranchNotFound, providerRejected: GetFormBranchProviderRejected, invalidResponse: GetFormBranchInvalidResponse,
	}
	if outcome, isTerminal := classifyExchange(client, operationID, response, err, branches); isTerminal {
		return queryAttempt[Form](client, outcome, input.FormID)
	}
	var decoded typeformForm
	if err := decodeTypeformJSON(response.body, &decoded); err != nil {
		return queryAttempt[Form](client, malformedOutcome(operationID, GetFormBranchInvalidResponse), input.FormID)
	}
	form, err := decoded.convert()
	if err != nil || form.ID != input.FormID {
		return queryAttempt[Form](client, malformedOutcome(operationID, GetFormBranchInvalidResponse), input.FormID)
	}
	return sdkgo.NewQueryBranch(GetFormBranchFound, form, nil, client.typeformReceipt(form.ID))
}

// query validates the input and returns the request query and the page it reads.
func (input ListFormsInput) query() (url.Values, int, error) {
	switch {
	case len(input.Search) > maximumFormSearchBytes || !utf8.ValidString(input.Search):
		return nil, 0, fmt.Errorf("search must be valid UTF-8 of at most %d bytes", maximumFormSearchBytes)
	case input.WorkspaceID != "" && !typeformIDPattern.MatchString(input.WorkspaceID):
		return nil, 0, fmt.Errorf("workspaceId must be a Typeform workspace ID")
	case input.Page < 0 || input.Page > maximumFormPage:
		return nil, 0, fmt.Errorf("page must be from 1 through %d, or 0 for the first page", maximumFormPage)
	case input.PageSize < 0 || input.PageSize > maximumFormPageSize:
		return nil, 0, fmt.Errorf("pageSize must be from 1 through %d, or 0 for Typeform's default of 10", maximumFormPageSize)
	}
	page := max(input.Page, 1)
	query := url.Values{"page": {strconv.Itoa(page)}}
	if input.PageSize > 0 {
		query.Set("page_size", strconv.Itoa(input.PageSize))
	}
	if input.Search != "" {
		query.Set("search", input.Search)
	}
	if input.WorkspaceID != "" {
		query.Set("workspace_id", input.WorkspaceID)
	}
	return query, page, nil
}

func (list typeformFormList) convert(page int) (FormPage, error) {
	if list.TotalItems == nil || list.PageCount == nil || *list.TotalItems < 0 || *list.PageCount < 0 {
		return FormPage{}, errTypeformResponseMalformed
	}
	formPage := FormPage{Forms: make([]FormSummary, 0, len(list.Items)), Page: page, PageCount: *list.PageCount, TotalItems: *list.TotalItems}
	for _, item := range list.Items {
		if !typeformIDPattern.MatchString(item.ID) {
			return FormPage{}, errTypeformResponseMalformed
		}
		createdAt, err := parseOptionalTypeformTimestamp(item.CreatedAt)
		if err != nil {
			return FormPage{}, err
		}
		lastUpdatedAt, err := parseOptionalTypeformTimestamp(item.LastUpdatedAt)
		if err != nil {
			return FormPage{}, err
		}
		formPage.Forms = append(formPage.Forms, FormSummary{
			ID: item.ID, Title: item.Title, IsPublic: item.Settings.IsPublic == nil || *item.Settings.IsPublic,
			DisplayURL: item.Links.Display, CreatedAt: createdAt, LastUpdatedAt: lastUpdatedAt,
		})
	}
	if page < formPage.PageCount {
		formPage.NextPage = page + 1
	}
	return formPage, nil
}

func (form typeformForm) convert() (Form, error) {
	if !typeformIDPattern.MatchString(form.ID) {
		return Form{}, errTypeformResponseMalformed
	}
	converted := Form{ID: form.ID, Title: form.Title, Language: form.Language, HiddenFields: form.Hidden, DisplayURL: form.Links.Display}
	fields, err := appendFormFields(nil, form.Fields, "", 1)
	if err != nil {
		return Form{}, err
	}
	converted.Fields = fields
	return converted, nil
}

// appendFormFields flattens fields depth first, so each group's questions follow the group.
func appendFormFields(fields []FormField, source []typeformFormField, groupFieldID string, depth int) ([]FormField, error) {
	if depth > maximumFieldNestingDepth {
		return nil, errTypeformResponseMalformed
	}
	for _, field := range source {
		if field.ID == "" || field.Type == "" {
			return nil, errTypeformResponseMalformed
		}
		converted := FormField{
			ID: field.ID, Ref: field.Ref, Title: field.Title, Type: field.Type,
			IsRequired: field.Validations.Required, GroupFieldID: groupFieldID,
		}
		for _, choice := range field.Properties.Choices {
			converted.Choices = append(converted.Choices, FormFieldChoice{ID: choice.ID, Ref: choice.Ref, Label: choice.Label})
		}
		fields = append(fields, converted)
		var err error
		if fields, err = appendFormFields(fields, field.Properties.Fields, field.ID, depth+1); err != nil {
			return nil, err
		}
	}
	if fields == nil {
		fields = []FormField{}
	}
	return fields, nil
}
