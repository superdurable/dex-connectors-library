// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listMembersOperation = "listMembers"

	// DefaultListMembersPageSize is the page size used when ListMembersInput.PageSize is zero.
	DefaultListMembersPageSize = 100
	// MaxListMembersPageSize is Mailchimp's documented maximum count.
	MaxListMembersPageSize = 1000
)

// memberListFields limits each page to the fields Member holds, so a 1000-contact page stays small.
var memberListFields = strings.Join([]string{
	"members.id", "members.email_address", "members.contact_id", "members.full_name", "members.web_id",
	"members.email_type", "members.status", "members.merge_fields", "members.vip", "members.language",
	"members.timestamp_signup", "members.timestamp_opt", "members.last_changed", "members.tags_count",
	"members.tags", "members.list_id", "list_id", "total_items",
}, ",")

// ListMembersInput selects one page of an audience's contacts. Pages are ordered by last change,
// oldest first, and continue with Offset.
type ListMembersInput struct {
	// ListID is the audience ID, Mailchimp's list_id.
	ListID string `json:"listId"`
	// Status keeps only contacts in one status, including cleaned or archived; empty lists every status.
	Status MemberStatus `json:"status,omitempty"`
	// ChangedSince keeps only contacts whose information changed after this time; nil applies no bound.
	ChangedSince *time.Time `json:"changedSince,omitempty"`
	// OptedInSince keeps only contacts who opted in after this time; nil applies no bound.
	OptedInSince *time.Time `json:"optedInSince,omitempty"`
	// PageSize is the number of contacts to read, 1 to 1000; zero reads 100.
	PageSize int `json:"pageSize,omitempty"`
	// Offset is the number of matching contacts to skip; use the previous page's NextOffset.
	Offset int `json:"offset,omitempty"`
}

// ListMembersOutput is one page of contacts.
type ListMembersOutput struct {
	// Members are this page's contacts, oldest change first.
	Members []Member `json:"members"`
	// TotalItems is Mailchimp's count of every contact that matches the filters.
	TotalItems int `json:"totalItems"`
	// NextOffset is the Offset of the following page, or zero after the last page.
	NextOffset int `json:"nextOffset,omitempty"`
}

// ListMembersOperation is the listMembers Query.
type ListMembersOperation struct {
	client *Client
}

type listMembersWire struct {
	Members    []memberWire `json:"members"`
	TotalItems *int         `json:"total_items"`
}

// Definition returns the immutable connector operation definition.
func (ListMembersOperation) Definition() sdkgo.QueryDefinition { return ListMembersDefinition }

// Invoke reads GET /lists/{list_id}/members with count, offset, the status and time filters,
// sort_field=last_changed, sort_dir=ASC, and a fields list that keeps only what Member holds.
// Offset paging is a snapshot per page: a contact that changes while a Flow pages moves to the
// end, so a page can repeat one contact or shift past one; a later changedSince read catches it.
func (operation ListMembersOperation) Invoke(call sdkgo.Call, input ListMembersInput) sdkgo.QueryAttempt[ListMembersOutput] {
	if err := validateListMembersInput(input); err != nil {
		return sdkgo.NewQueryBranch(ListMembersBranchDefect, ListMembersOutput{}, mailchimpFailurePointer(sdkgo.FailureValidation, listMembersOperation, err.Error()), sdkgo.Receipt{})
	}
	connection, failure := operation.client.resolveConnection(call, listMembersOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListMembersBranchDefect, ListMembersOutput{}, failure, sdkgo.Receipt{})
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = DefaultListMembersPageSize
	}
	result := operation.client.exchange(call, connection, listMembersOperation, mailchimpRequest{
		method: http.MethodGet, path: "/lists/" + input.ListID + "/members", query: buildListMembersQuery(input, pageSize),
	})
	receipt := operation.client.receipt(call, result.response, input.ListID)
	if attempt, isTerminal := queryAttemptForExchange[ListMembersOutput](result, receipt, repeatableBranches{
		notFound: ListMembersBranchNotFound, providerRejected: ListMembersBranchProviderRejected,
		invalidResponse: ListMembersBranchInvalidResponse, defect: ListMembersBranchDefect,
	}); isTerminal {
		return attempt
	}
	output, err := decodeMemberPage(result.response.body, input.Offset, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListMembersBranchInvalidResponse, ListMembersOutput{}, mailchimpFailurePointer(sdkgo.FailureProtocol, listMembersOperation,
			"Mailchimp returned an invalid page of contacts: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListMembersBranchListed, output, nil, receipt)
}

func validateListMembersInput(input ListMembersInput) error {
	if err := validateResourceID("listId", input.ListID); err != nil {
		return err
	}
	if input.Status != "" && !IsReadableMemberStatus(input.Status) {
		return fmt.Errorf("status %q is not a Mailchimp contact status", input.Status)
	}
	if input.PageSize < 0 || input.PageSize > MaxListMembersPageSize {
		return fmt.Errorf("pageSize must be 1 to %d, or zero for %d", MaxListMembersPageSize, DefaultListMembersPageSize)
	}
	if input.Offset < 0 || input.Offset > math.MaxInt32 {
		return errors.New("offset must be a non-negative number of contacts")
	}
	for name, bound := range map[string]*time.Time{"changedSince": input.ChangedSince, "optedInSince": input.OptedInSince} {
		if bound != nil && bound.IsZero() {
			return fmt.Errorf("%s must be a time, or omitted for no bound", name)
		}
	}
	return nil
}

func buildListMembersQuery(input ListMembersInput, pageSize int) url.Values {
	query := url.Values{
		"count": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(input.Offset)},
		"sort_field": {"last_changed"}, "sort_dir": {"ASC"}, "fields": {memberListFields},
	}
	if input.Status != "" {
		query.Set("status", string(input.Status))
	}
	if input.ChangedSince != nil {
		query.Set("since_last_changed", formatMailchimpTime(*input.ChangedSince))
	}
	if input.OptedInSince != nil {
		query.Set("since_timestamp_opt", formatMailchimpTime(*input.OptedInSince))
	}
	return query
}

func decodeMemberPage(body []byte, offset int, pageSize int) (ListMembersOutput, error) {
	var page listMembersWire
	if err := json.Unmarshal(body, &page); err != nil {
		return ListMembersOutput{}, errors.New("the page is not valid JSON")
	}
	if page.TotalItems == nil || *page.TotalItems < 0 || len(page.Members) > pageSize {
		return ListMembersOutput{}, errors.New("the page has no total_items or more contacts than requested")
	}
	output := ListMembersOutput{Members: make([]Member, 0, len(page.Members)), TotalItems: *page.TotalItems}
	for _, wire := range page.Members {
		member, err := convertMember(wire, "")
		if err != nil {
			return ListMembersOutput{}, err
		}
		output.Members = append(output.Members, member)
	}
	if nextOffset := offset + len(output.Members); len(output.Members) != 0 && nextOffset < output.TotalItems {
		output.NextOffset = nextOffset
	}
	return output, nil
}
