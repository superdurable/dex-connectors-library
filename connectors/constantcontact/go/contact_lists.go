// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ContactListMetadata contains the public settings of one contact list.
type ContactListMetadata struct {
	// ListID is the V3 list UUID.
	ListID string `json:"listId"`
	// Name is the unique list name.
	Name string `json:"name"`
	// Description is the optional list description.
	Description string `json:"description,omitempty"`
	// Favorite controls whether the provider highlights this list.
	Favorite bool `json:"favorite"`
}

// GetContactListInput identifies one contact list.
type GetContactListInput struct {
	// ListID is the V3 list UUID.
	ListID string `json:"listId"`
}

// ListContactListsInput selects a bounded collection of contact lists.
type ListContactListsInput struct {
	// Limit is 1 through 1000; zero uses Constant Contact's default.
	Limit int `json:"limit,omitempty"`
	// Name optionally matches the full list name.
	Name string `json:"name,omitempty"`
}

// ContactListsPage contains the returned contact lists.
type ContactListsPage struct {
	// Lists are the returned contact lists.
	Lists []ContactListMetadata `json:"lists"`
}

// CreateContactListInput gives the complete settings for a new list.
type CreateContactListInput struct {
	// Name is a unique non-empty list name.
	Name string `json:"name"`
	// Description is optional and at most 255 characters.
	Description string `json:"description,omitempty"`
	// Favorite highlights the list in Constant Contact.
	Favorite bool `json:"favorite"`
}

// UpdateContactListInput replaces the settings of an existing list.
type UpdateContactListInput struct {
	// ListID is the V3 list UUID.
	ListID string `json:"listId"`
	// Name is the new unique list name.
	Name string `json:"name"`
	// Description replaces the existing description; blank clears it.
	Description string `json:"description"`
	// Favorite replaces the current favorite state.
	Favorite bool `json:"favorite"`
}

// DeleteContactListInput identifies one contact list to remove.
type DeleteContactListInput struct {
	// ListID is the V3 list UUID.
	ListID string `json:"listId"`
}

// DeleteContactListOutput identifies the accepted asynchronous deletion.
type DeleteContactListOutput struct {
	// ListID is the requested list UUID.
	ListID string `json:"listId"`
	// ActivityID identifies the provider activity to inspect for completion.
	ActivityID string `json:"activityId"`
}

// GetContactListOperation reads one list.
type GetContactListOperation struct{ client *Client }

// ListContactListsOperation reads a page of lists.
type ListContactListsOperation struct{ client *Client }

// CreateContactListOperation creates a list with a unique name.
type CreateContactListOperation struct{ client *Client }

// UpdateContactListOperation replaces one list's settings.
type UpdateContactListOperation struct{ client *Client }

// DeleteContactListOperation requests asynchronous list deletion.
type DeleteContactListOperation struct{ client *Client }

// GetContactList binds the Query to this client.
func (client *Client) GetContactList() GetContactListOperation {
	return GetContactListOperation{client}
}

// ListContactLists binds the Query to this client.
func (client *Client) ListContactLists() ListContactListsOperation {
	return ListContactListsOperation{client}
}

// CreateContactList binds the Mutation to this client.
func (client *Client) CreateContactList() CreateContactListOperation {
	return CreateContactListOperation{client}
}

// UpdateContactList binds the Mutation to this client.
func (client *Client) UpdateContactList() UpdateContactListOperation {
	return UpdateContactListOperation{client}
}

// DeleteContactList binds the Mutation to this client.
func (client *Client) DeleteContactList() DeleteContactListOperation {
	return DeleteContactListOperation{client}
}

// Definition returns the generated Query contract.
func (GetContactListOperation) Definition() sdkgo.QueryDefinition { return GetContactListDefinition }

// Definition returns the generated Query contract.
func (ListContactListsOperation) Definition() sdkgo.QueryDefinition {
	return ListContactListsDefinition
}

// Definition returns the generated Mutation contract.
func (CreateContactListOperation) Definition() sdkgo.MutationDefinition {
	return CreateContactListDefinition
}

// Definition returns the generated Mutation contract.
func (UpdateContactListOperation) Definition() sdkgo.MutationDefinition {
	return UpdateContactListDefinition
}

// Definition returns the generated Mutation contract.
func (DeleteContactListOperation) Definition() sdkgo.MutationDefinition {
	return DeleteContactListDefinition
}

// IdempotencyKey correlates attempts; a repeated name cannot create a second list.
func (CreateContactListOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts of an absolute list replacement.
func (UpdateContactListOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts of a repeatable deletion request.
func (DeleteContactListOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one contact list by ID.
func (operation GetContactListOperation) Invoke(call sdkgo.Call, input GetContactListInput) sdkgo.QueryAttempt[ContactListMetadata] {
	if !contactIDPattern.MatchString(input.ListID) {
		return listQueryBranch[ContactListMetadata]("getContactList", GetContactListBranchDefect, sdkgo.FailureValidation, "list ID must be a UUID")
	}
	response := operation.client.get(call, "/contact_lists/"+input.ListID)
	if kind, failure := classifyListResponse("getContactList", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewQueryRetry[ContactListMetadata](failure, response.retryAfter)
		}
		branch := GetContactListBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = GetContactListBranchNotFound
		}
		if kind == listResponseInvalid {
			branch = GetContactListBranchInvalidResponse
		}
		if kind == listResponseDefect {
			branch = GetContactListBranchDefect
		}
		return sdkgo.NewQueryBranch(branch, ContactListMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listQueryBranch[ContactListMetadata]("getContactList", GetContactListBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	list, err := decodeContactList(response.body)
	if err != nil {
		return listQueryBranch[ContactListMetadata]("getContactList", GetContactListBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid list")
	}
	return sdkgo.NewQueryBranch(GetContactListBranchFound, list, nil, sdkgo.Receipt{})
}

// Invoke reads one bounded page of contact lists.
func (operation ListContactListsOperation) Invoke(call sdkgo.Call, input ListContactListsInput) sdkgo.QueryAttempt[ContactListsPage] {
	if input.Limit < 0 || input.Limit > 1000 || len(input.Name) > 255 || strings.ContainsAny(input.Name, "\r\n") {
		return listQueryBranch[ContactListsPage]("listContactLists", ListContactListsBranchDefect, sdkgo.FailureValidation, "invalid list page input")
	}
	parameters := url.Values{}
	if input.Limit > 0 {
		parameters.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Name != "" {
		parameters.Set("name", input.Name)
	}
	path := "/contact_lists"
	if len(parameters) > 0 {
		path += "?" + parameters.Encode()
	}
	response := operation.client.get(call, path)
	if kind, failure := classifyListResponse("listContactLists", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewQueryRetry[ContactListsPage](failure, response.retryAfter)
		}
		branch := ListContactListsBranchProviderRejected
		if kind == listResponseInvalid {
			branch = ListContactListsBranchInvalidResponse
		}
		if kind == listResponseDefect {
			branch = ListContactListsBranchDefect
		}
		return sdkgo.NewQueryBranch(branch, ContactListsPage{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listQueryBranch[ContactListsPage]("listContactLists", ListContactListsBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Lists []json.RawMessage `json:"lists"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil || envelope.Lists == nil {
		return listQueryBranch[ContactListsPage]("listContactLists", ListContactListsBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned invalid list JSON")
	}
	page := ContactListsPage{Lists: make([]ContactListMetadata, 0, len(envelope.Lists))}
	for _, item := range envelope.Lists {
		list, err := decodeContactList(item)
		if err != nil {
			return listQueryBranch[ContactListsPage]("listContactLists", ListContactListsBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid list")
		}
		page.Lists = append(page.Lists, list)
	}
	return sdkgo.NewQueryBranch(ListContactListsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates one list using a unique name.
func (operation CreateContactListOperation) Invoke(call sdkgo.Call, input CreateContactListInput) sdkgo.MutationAttempt[ContactListMetadata] {
	if !validContactListSettings(input.Name, input.Description) {
		return listMutationBranch[ContactListMetadata]("createContactList", CreateContactListBranchDefect, sdkgo.FailureValidation, "invalid list name or description")
	}
	payload, err := contactListPayload(input.Name, input.Description, input.Favorite)
	if err != nil {
		return listMutationBranch[ContactListMetadata]("createContactList", CreateContactListBranchDefect, sdkgo.FailureLocalDefect, "list request could not be encoded")
	}
	response := operation.client.send(call, http.MethodPost, "/contact_lists", payload)
	if kind, failure := classifyListResponse("createContactList", response); kind != listResponseSuccess {
		if kind == listResponseRetry || kind == listResponseInvalid {
			return sdkgo.NewMutationRetry[ContactListMetadata](failure, response.retryAfter)
		}
		branch := CreateContactListBranchProviderRejected
		if response.status == http.StatusConflict {
			branch = CreateContactListBranchAlreadyExists
		}
		if kind == listResponseDefect {
			branch = CreateContactListBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, ContactListMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return listMutationBranch[ContactListMetadata]("createContactList", CreateContactListBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	list, err := decodeContactList(response.body)
	if err != nil {
		return sdkgo.NewMutationRetry[ContactListMetadata](sdkgo.Failure{Kind: sdkgo.FailureProtocol, Provider: "constantcontact", Operation: "createContactList", Message: "Constant Contact returned an invalid list"}, 0)
	}
	return sdkgo.NewMutationBranch(CreateContactListBranchCreated, list, nil, sdkgo.Receipt{})
}

// Invoke replaces one list's name, description, and favorite state.
func (operation UpdateContactListOperation) Invoke(call sdkgo.Call, input UpdateContactListInput) sdkgo.MutationAttempt[ContactListMetadata] {
	if !contactIDPattern.MatchString(input.ListID) || !validContactListSettings(input.Name, input.Description) {
		return listMutationBranch[ContactListMetadata]("updateContactList", UpdateContactListBranchDefect, sdkgo.FailureValidation, "invalid list ID, name, or description")
	}
	payload, err := contactListPayload(input.Name, input.Description, input.Favorite)
	if err != nil {
		return listMutationBranch[ContactListMetadata]("updateContactList", UpdateContactListBranchDefect, sdkgo.FailureLocalDefect, "list request could not be encoded")
	}
	response := operation.client.send(call, http.MethodPut, "/contact_lists/"+input.ListID, payload)
	if kind, failure := classifyListResponse("updateContactList", response); kind != listResponseSuccess {
		if kind == listResponseRetry || kind == listResponseInvalid {
			return sdkgo.NewMutationRetry[ContactListMetadata](failure, response.retryAfter)
		}
		branch := UpdateContactListBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateContactListBranchNotFound
		}
		if kind == listResponseDefect {
			branch = UpdateContactListBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, ContactListMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listMutationBranch[ContactListMetadata]("updateContactList", UpdateContactListBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	list, err := decodeContactList(response.body)
	if err != nil {
		return sdkgo.NewMutationRetry[ContactListMetadata](sdkgo.Failure{Kind: sdkgo.FailureProtocol, Provider: "constantcontact", Operation: "updateContactList", Message: "Constant Contact returned an invalid list"}, 0)
	}
	return sdkgo.NewMutationBranch(UpdateContactListBranchUpdated, list, nil, sdkgo.Receipt{})
}

// Invoke requests an asynchronous list deletion and returns the activity ID.
func (operation DeleteContactListOperation) Invoke(call sdkgo.Call, input DeleteContactListInput) sdkgo.MutationAttempt[DeleteContactListOutput] {
	if !contactIDPattern.MatchString(input.ListID) {
		return listMutationBranch[DeleteContactListOutput]("deleteContactList", DeleteContactListBranchDefect, sdkgo.FailureValidation, "list ID must be a UUID")
	}
	response := operation.client.send(call, http.MethodDelete, "/contact_lists/"+input.ListID, nil)
	if kind, failure := classifyListResponse("deleteContactList", response); kind != listResponseSuccess {
		if kind == listResponseRetry || kind == listResponseInvalid {
			return sdkgo.NewMutationRetry[DeleteContactListOutput](failure, response.retryAfter)
		}
		branch := DeleteContactListBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = DeleteContactListBranchNotFound
		}
		if kind == listResponseDefect {
			branch = DeleteContactListBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, DeleteContactListOutput{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusAccepted && response.status != http.StatusOK {
		return listMutationBranch[DeleteContactListOutput]("deleteContactList", DeleteContactListBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	var activity struct {
		ActivityID string `json:"activity_id"`
	}
	if err := json.Unmarshal(response.body, &activity); err != nil || !contactIDPattern.MatchString(activity.ActivityID) {
		return sdkgo.NewMutationRetry[DeleteContactListOutput](sdkgo.Failure{Kind: sdkgo.FailureProtocol, Provider: "constantcontact", Operation: "deleteContactList", Message: "Constant Contact returned an invalid activity"}, 0)
	}
	return sdkgo.NewMutationBranch(DeleteContactListBranchAccepted, DeleteContactListOutput{ListID: input.ListID, ActivityID: activity.ActivityID}, nil, sdkgo.Receipt{})
}

type listResponseKind uint8

const (
	listResponseSuccess listResponseKind = iota
	listResponseRetry
	listResponseInvalid
	listResponseDefect
	listResponseRejected
)

func classifyListResponse(operation string, response providerResponse) (listResponseKind, sdkgo.Failure) {
	failure := sdkgo.Failure{Provider: "constantcontact", Operation: operation}
	switch {
	case errors.Is(response.err, errCredential):
		failure.Kind, failure.Message = sdkgo.FailureAuthentication, "Constant Contact credentials are unavailable or invalid"
		return listResponseDefect, failure
	case errors.Is(response.err, errRefresh):
		failure.Kind, failure.Message = sdkgo.FailureAvailability, "Constant Contact credentials could not be refreshed"
		return listResponseRetry, failure
	case errors.Is(response.err, errRequest):
		failure.Kind, failure.Message = sdkgo.FailureTransport, "Constant Contact request failed"
		return listResponseRetry, failure
	case response.status == http.StatusTooManyRequests:
		failure.Kind, failure.Message = sdkgo.FailureRateLimit, "Constant Contact rate limited the request"
		return listResponseRetry, failure
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		failure.Kind, failure.Message = sdkgo.FailureAvailability, "Constant Contact is temporarily unavailable"
		return listResponseRetry, failure
	case errors.Is(response.err, providerhttp.ErrBodyTooLarge):
		failure.Kind, failure.Message = sdkgo.FailureResponseTooLarge, "Constant Contact response exceeded the configured limit"
		return listResponseInvalid, failure
	case response.err != nil:
		failure.Kind, failure.Message = sdkgo.FailureTransport, "Constant Contact response was interrupted"
		return listResponseRetry, failure
	case response.status >= 400:
		failure.Kind, failure.Message = sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status)
		if response.status == http.StatusNotFound {
			failure.Kind = sdkgo.FailureNotFound
		}
		return listResponseRejected, failure
	default:
		return listResponseSuccess, failure
	}
}

func validContactListSettings(name string, description string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 255 && len(description) <= 255
}

func contactListPayload(name string, description string, favorite bool) ([]byte, error) {
	return json.Marshal(struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Favorite    bool   `json:"favorite"`
	}{name, description, favorite})
}

func decodeContactList(body []byte) (ContactListMetadata, error) {
	var list struct {
		ListID      string `json:"list_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Favorite    bool   `json:"favorite"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return ContactListMetadata{}, err
	}
	if !contactIDPattern.MatchString(list.ListID) || strings.TrimSpace(list.Name) == "" {
		return ContactListMetadata{}, errors.New("list identity missing")
	}
	return ContactListMetadata{ListID: list.ListID, Name: list.Name, Description: list.Description, Favorite: list.Favorite}, nil
}

func listQueryBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "constantcontact", Operation: operation, Message: message}
	var output T
	return sdkgo.NewQueryBranch(branch, output, &failure, sdkgo.Receipt{})
}

func listMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "constantcontact", Operation: operation, Message: message}
	var output T
	return sdkgo.NewMutationBranch(branch, output, &failure, sdkgo.Receipt{})
}
