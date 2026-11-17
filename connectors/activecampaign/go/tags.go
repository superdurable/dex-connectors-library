// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListTagsInput selects one page of contact tag definitions.
type ListTagsInput struct {
	// Offset is the zero-based number of tags to skip.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum tag count; zero uses 20 and 100 is the maximum.
	Limit int `json:"limit,omitempty"`
}

// GetTagInput selects a tag by its numeric ActiveCampaign ID.
type GetTagInput struct {
	// TagID is the numeric ID from the Tags page or API.
	TagID string `json:"tagId"`
}

// TagMetadata contains a tag definition without links or contact relationships.
type TagMetadata struct {
	// ID is the stable provider tag ID.
	ID string `json:"id"`
	// Name is the tag's display name.
	Name string `json:"name"`
	// Description is the tag's optional human-readable description.
	Description string `json:"description"`
	// TagType is always contact for these operations.
	TagType string `json:"tagType"`
}

// TagsPage is one offset-based page of contact tag definitions.
type TagsPage struct {
	// Tags contains the selected tag definitions.
	Tags []TagMetadata `json:"tags"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
	// Total is the provider's matching tag count.
	Total int `json:"total"`
}

// ListTagsOperation retrieves one page of contact tag definitions.
type ListTagsOperation struct{ client *Client }

// GetTagOperation retrieves one tag definition by ID.
type GetTagOperation struct{ client *Client }

// ListTags binds the list Query to this client.
func (client *Client) ListTags() ListTagsOperation { return ListTagsOperation{client} }

// GetTag binds the single-tag Query to this client.
func (client *Client) GetTag() GetTagOperation { return GetTagOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListTagsOperation) Definition() sdkgo.QueryDefinition { return ListTagsDefinition }

// Definition returns the manifest-generated single-tag contract.
func (GetTagOperation) Definition() sdkgo.QueryDefinition { return GetTagDefinition }

// Invoke reads one page of contact tag definitions.
func (operation ListTagsOperation) Invoke(call sdkgo.Call, input ListTagsInput) sdkgo.QueryAttempt[TagsPage] {
	branches := queryBranches{found: ListTagsBranchListed, rejected: ListTagsBranchProviderRejected, invalid: ListTagsBranchInvalidResponse, defect: ListTagsBranchDefect}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listTags", branches.defect, TagsPage{}, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeQuery(call, operation.client, "listTags", "/tags?"+query.Encode(), branches, func(body []byte) (TagsPage, error) { return decodeTagsPage(body, input) })
}

// Invoke reads one contact tag definition.
func (operation GetTagOperation) Invoke(call sdkgo.Call, input GetTagInput) sdkgo.QueryAttempt[TagMetadata] {
	branches := queryBranches{found: GetTagBranchFound, notFound: GetTagBranchNotFound, rejected: GetTagBranchProviderRejected, invalid: GetTagBranchInvalidResponse, defect: GetTagBranchDefect}
	if !numericID.MatchString(input.TagID) {
		return queryBranch("getTag", branches.defect, TagMetadata{}, sdkgo.FailureValidation, "tag ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getTag", "/tags/"+input.TagID, branches, decodeTag)
}

type tagWire struct {
	ID          string `json:"id"`
	Name        string `json:"tag"`
	Description string `json:"description"`
	TagType     string `json:"tagType"`
}

func decodeTagsPage(body []byte, input ListTagsInput) (TagsPage, error) {
	var wire struct {
		Tags []tagWire `json:"tags"`
		Meta struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TagsPage{}, err
	}
	if wire.Tags == nil {
		return TagsPage{}, fmt.Errorf("tag collection missing")
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total < 0 {
		return TagsPage{}, fmt.Errorf("tag count missing or invalid")
	}
	page := TagsPage{Tags: make([]TagMetadata, 0, len(wire.Tags)), Offset: input.Offset, Limit: input.Limit, Total: total}
	for _, item := range wire.Tags {
		metadata, err := tagMetadata(item)
		if err != nil {
			return TagsPage{}, err
		}
		page.Tags = append(page.Tags, metadata)
	}
	return page, nil
}

func decodeTag(body []byte) (TagMetadata, error) {
	var wire struct {
		Tag *tagWire `json:"tag"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TagMetadata{}, err
	}
	if wire.Tag == nil {
		return TagMetadata{}, fmt.Errorf("tag missing")
	}
	return tagMetadata(*wire.Tag)
}

func tagMetadata(item tagWire) (TagMetadata, error) {
	if !numericID.MatchString(item.ID) || item.Name == "" || item.TagType != "contact" {
		return TagMetadata{}, fmt.Errorf("contact tag identity missing or invalid")
	}
	return TagMetadata{ID: item.ID, Name: item.Name, Description: item.Description, TagType: item.TagType}, nil
}
