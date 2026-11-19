// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListFormsInput selects a cursor page of Kit forms.
type ListFormsInput struct {
	// PerPage bounds the page size; zero uses 20.
	PerPage int `json:"perPage,omitempty"`
	// After is the prior page's opaque end cursor.
	After string `json:"after,omitempty"`
}

// ListFormSubscribersInput selects a cursor page of one form's subscribers.
type ListFormSubscribersInput struct {
	// FormID is the numeric Kit form ID.
	FormID int64 `json:"formId"`
	// PerPage bounds the page size; zero uses 20.
	PerPage int `json:"perPage,omitempty"`
	// After is the prior page's opaque end cursor.
	After string `json:"after,omitempty"`
}

// AddSubscriberToFormInput identifies a form, subscriber, and signup referrer.
type AddSubscriberToFormInput struct {
	// FormID is the numeric Kit form ID.
	FormID int64 `json:"formId"`
	// SubscriberID is an existing subscriber's numeric ID.
	SubscriberID int64 `json:"subscriberId"`
	// Referrer is the absolute HTTP(S) URL where the signup originated.
	Referrer string `json:"referrer"`
}

// FormMetadata contains a form's stable ID, name, and type.
type FormMetadata struct {
	// ID is the numeric Kit form ID.
	ID int64 `json:"id"`
	// Name is the display name.
	Name string `json:"name"`
	// Type distinguishes embedded forms and hosted landing pages.
	Type string `json:"type"`
}

// FormsPage contains one cursor page of Kit forms.
type FormsPage struct {
	// Forms are the returned forms.
	Forms []FormMetadata `json:"forms"`
	// NextCursor is nonempty when another page exists.
	NextCursor string `json:"nextCursor"`
}

// ListFormsOperation lists forms and landing pages.
type ListFormsOperation struct{ client *Client }

// ListFormSubscribersOperation lists subscribers of one form.
type ListFormSubscribersOperation struct{ client *Client }

// AddSubscriberToFormOperation adds an existing subscriber to a form.
type AddSubscriberToFormOperation struct{ client *Client }

// ListForms binds form listing to this client.
func (client *Client) ListForms() ListFormsOperation { return ListFormsOperation{client} }

// ListFormSubscribers binds form subscriber listing to this client.
func (client *Client) ListFormSubscribers() ListFormSubscribersOperation {
	return ListFormSubscribersOperation{client}
}

// AddSubscriberToForm binds form signup to this client.
func (client *Client) AddSubscriberToForm() AddSubscriberToFormOperation {
	return AddSubscriberToFormOperation{client}
}

// Definition returns the generated query contract.
func (ListFormsOperation) Definition() sdkgo.QueryDefinition { return ListFormsDefinition }

// Definition returns the generated query contract.
func (ListFormSubscribersOperation) Definition() sdkgo.QueryDefinition {
	return ListFormSubscribersDefinition
}

// Definition returns the generated mutation contract.
func (AddSubscriberToFormOperation) Definition() sdkgo.MutationDefinition {
	return AddSubscriberToFormDefinition
}

// IdempotencyKey derives a stable call identity across retries.
func (AddSubscriberToFormOperation) IdempotencyKey(callID sdkgo.CallID, _ AddSubscriberToFormInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one cursor page of forms and landing pages.
func (operation ListFormsOperation) Invoke(call sdkgo.Call, input ListFormsInput) sdkgo.QueryAttempt[FormsPage] {
	branches := queryBranches{found: ListFormsBranchListed, rejected: ListFormsBranchProviderRejected, invalid: ListFormsBranchInvalidResponse, defect: ListFormsBranchDefect}
	if input.PerPage < 0 || input.PerPage > 1000 || len(input.After) > 2048 {
		return queryBranch("listForms", branches.defect, FormsPage{}, sdkgo.FailureValidation, "cursor or perPage is invalid")
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return invokeQuery(call, operation.client, "listForms", "/forms?"+query.Encode(), branches, decodeFormsPage)
}

// Invoke reads one cursor page of a form's subscribers.
func (operation ListFormSubscribersOperation) Invoke(call sdkgo.Call, input ListFormSubscribersInput) sdkgo.QueryAttempt[SubscribersPage] {
	branches := queryBranches{found: ListFormSubscribersBranchListed, notFound: ListFormSubscribersBranchNotFound, rejected: ListFormSubscribersBranchProviderRejected, invalid: ListFormSubscribersBranchInvalidResponse, defect: ListFormSubscribersBranchDefect}
	if input.FormID <= 0 || input.PerPage < 0 || input.PerPage > 1000 || len(input.After) > 2048 {
		return queryBranch("listFormSubscribers", branches.defect, SubscribersPage{}, sdkgo.FailureValidation, "form ID, cursor, or perPage is invalid")
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}, "slim": {"true"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	path := "/forms/" + strconv.FormatInt(input.FormID, 10) + "/subscribers?" + query.Encode()
	return invokeQuery(call, operation.client, "listFormSubscribers", path, branches, func(body []byte) (SubscribersPage, error) {
		return decodeSubscribersPage(body, ListSubscribersInput{After: input.After, PerPage: input.PerPage})
	})
}

// Invoke adds an existing subscriber to an active form.
func (operation AddSubscriberToFormOperation) Invoke(call sdkgo.Call, input AddSubscriberToFormInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	referrer, err := url.Parse(input.Referrer)
	if input.FormID <= 0 || input.SubscriberID <= 0 || err != nil || (referrer.Scheme != "https" && referrer.Scheme != "http") || referrer.Host == "" || referrer.User != nil || len(input.Referrer) > 2048 {
		return kitMutationBranch("addSubscriberToForm", AddSubscriberToFormBranchDefect, SubscriberMetadata{}, sdkgo.FailureValidation, "positive IDs and an absolute HTTP(S) referrer are required")
	}
	body, _ := json.Marshal(map[string]string{"referrer": input.Referrer})
	path := "/forms/" + strconv.FormatInt(input.FormID, 10) + "/subscribers/" + strconv.FormatInt(input.SubscriberID, 10)
	return invokeKitMutation(call, operation.client, "addSubscriberToForm", http.MethodPost, path, body,
		mutationBranches{success: AddSubscriberToFormBranchAdded, notFound: AddSubscriberToFormBranchNotFound, rejected: AddSubscriberToFormBranchProviderRejected, invalid: AddSubscriberToFormBranchInvalidResponse, defect: AddSubscriberToFormBranchDefect}, decodeSubscriber)
}

func decodeFormsPage(body []byte) (FormsPage, error) {
	var wire struct {
		Forms      []FormMetadata `json:"forms"`
		Pagination *struct {
			HasNextPage bool   `json:"has_next_page"`
			EndCursor   string `json:"end_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Forms == nil || wire.Pagination == nil {
		return FormsPage{}, fmt.Errorf("invalid form page")
	}
	page := FormsPage{Forms: wire.Forms}
	if wire.Pagination.HasNextPage {
		if wire.Pagination.EndCursor == "" {
			return FormsPage{}, fmt.Errorf("missing form cursor")
		}
		page.NextCursor = wire.Pagination.EndCursor
	}
	for _, form := range page.Forms {
		if form.ID <= 0 || form.Name == "" || (form.Type != "embed" && form.Type != "hosted") {
			return FormsPage{}, fmt.Errorf("invalid form metadata")
		}
	}
	return page, nil
}
