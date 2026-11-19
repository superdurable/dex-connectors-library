// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func decodeWrittenPost(body []byte) (CreatedPost, error) {
	value, err := decodeContentItem[CreatedPost](body)
	if err != nil || value.ID <= 0 {
		return CreatedPost{}, fmt.Errorf("invalid post response")
	}
	return value, nil
}
func decodeWrittenPage(body []byte) (Page, error) {
	value, err := decodeContentItem[Page](body)
	if err != nil || value.ID <= 0 {
		return Page{}, fmt.Errorf("invalid page response")
	}
	return value, nil
}
func decodeWrittenUser(body []byte) (User, error) {
	value, err := decodeContentItem[User](body)
	if err != nil || value.ID <= 0 {
		return User{}, fmt.Errorf("invalid user response")
	}
	return value, nil
}

// UpdatePostOperation writes one WordPress post.
type UpdatePostOperation struct{ client *Client }

// UpdatePost returns the post Mutation bound to this client.
func (client *Client) UpdatePost() UpdatePostOperation { return UpdatePostOperation{client} }

// Definition returns the generated updatePost contract.
func (UpdatePostOperation) Definition() sdkgo.MutationDefinition { return UpdatePostDefinition }

// IdempotencyKey records the stable call identity.
func (UpdatePostOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the provider post.
func (operation UpdatePostOperation) Invoke(call sdkgo.Call, input UpdatePostInput) sdkgo.MutationAttempt[CreatedPost] {
	if input.PostID <= 0 || strings.TrimSpace(input.Title) == "" || input.Status != "" && input.Status != "draft" && input.Status != "publish" {
		return contentWriteBranch[CreatedPost]("updatePost", UpdatePostBranchDefect, sdkgo.FailureValidation, "post title or status is invalid")
	}
	payload, err := encodeContentPayload(map[string]any{"title": input.Title, "content": input.Content, "status": input.Status})
	if err != nil || int64(len(payload)) > operation.client.maxPostBytes {
		return contentWriteBranch[CreatedPost]("updatePost", UpdatePostBranchDefect, sdkgo.FailureValidation, "post request is too large")
	}
	return writeContent(operation.client, call, "updatePost", http.MethodPost, "/wp-json/wp/v2/posts/"+strconv.FormatInt(input.PostID, 10), "/wp-json/wp/v2/posts/", payload, 0,
		contentWriteBranches{success: UpdatePostBranchUpdated, notFound: UpdatePostBranchNotFound, rejected: UpdatePostBranchProviderRejected, invalid: UpdatePostBranchInvalidResponse, defect: UpdatePostBranchDefect}, decodeWrittenPost, func(value CreatedPost) int64 { return value.ID })
}

// DeletePostOperation writes one WordPress post.
type DeletePostOperation struct{ client *Client }

// DeletePost returns the post Mutation bound to this client.
func (client *Client) DeletePost() DeletePostOperation { return DeletePostOperation{client} }

// Definition returns the generated deletePost contract.
func (DeletePostOperation) Definition() sdkgo.MutationDefinition { return DeletePostDefinition }

// IdempotencyKey records the stable call identity.
func (DeletePostOperation) IdempotencyKey(callID sdkgo.CallID, _ DeletePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the provider post.
func (operation DeletePostOperation) Invoke(call sdkgo.Call, input DeletePostInput) sdkgo.MutationAttempt[CreatedPost] {
	if input.PostID <= 0 {
		return contentWriteBranch[CreatedPost]("deletePost", DeletePostBranchDefect, sdkgo.FailureValidation, "postId must be positive")
	}
	var payload []byte
	return writeContent(operation.client, call, "deletePost", http.MethodDelete, "/wp-json/wp/v2/posts/"+strconv.FormatInt(input.PostID, 10), "/wp-json/wp/v2/posts/", payload, 0,
		contentWriteBranches{success: DeletePostBranchDeleted, notFound: DeletePostBranchNotFound, rejected: DeletePostBranchProviderRejected, invalid: DeletePostBranchInvalidResponse, defect: DeletePostBranchDefect}, decodeWrittenPost, func(value CreatedPost) int64 { return value.ID })
}

// CreatePageOperation writes one WordPress page.
type CreatePageOperation struct{ client *Client }

// CreatePage returns the page Mutation bound to this client.
func (client *Client) CreatePage() CreatePageOperation { return CreatePageOperation{client} }

// Definition returns the generated createPage contract.
func (CreatePageOperation) Definition() sdkgo.MutationDefinition { return CreatePageDefinition }

// IdempotencyKey records the stable call identity.
func (CreatePageOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the provider page.
func (operation CreatePageOperation) Invoke(call sdkgo.Call, input CreatePageInput) sdkgo.MutationAttempt[Page] {
	if strings.TrimSpace(input.Title) == "" || input.Status != "" && input.Status != "draft" && input.Status != "publish" {
		return contentWriteBranch[Page]("createPage", CreatePageBranchDefect, sdkgo.FailureValidation, "page title or status is invalid")
	}
	if input.Status == "" {
		input.Status = "draft"
	}
	payload, err := encodeContentPayload(map[string]any{"title": input.Title, "content": input.Content, "status": input.Status})
	if err != nil || int64(len(payload)) > operation.client.maxPostBytes {
		return contentWriteBranch[Page]("createPage", CreatePageBranchDefect, sdkgo.FailureValidation, "page request is too large")
	}
	return writeContent(operation.client, call, "createPage", http.MethodPost, "/wp-json/wp/v2/pages", "/wp-json/wp/v2/pages/", payload, CreatePageDefinition.StepDefaults.ExecuteMethodTimeout,
		contentWriteBranches{success: CreatePageBranchCreated, notFound: CreatePageBranchNotFound, rejected: CreatePageBranchProviderRejected, invalid: CreatePageBranchInvalidResponse, defect: CreatePageBranchDefect}, decodeWrittenPage, func(value Page) int64 { return value.ID })
}

// UpdatePageOperation writes one WordPress page.
type UpdatePageOperation struct{ client *Client }

// UpdatePage returns the page Mutation bound to this client.
func (client *Client) UpdatePage() UpdatePageOperation { return UpdatePageOperation{client} }

// Definition returns the generated updatePage contract.
func (UpdatePageOperation) Definition() sdkgo.MutationDefinition { return UpdatePageDefinition }

// IdempotencyKey records the stable call identity.
func (UpdatePageOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the provider page.
func (operation UpdatePageOperation) Invoke(call sdkgo.Call, input UpdatePageInput) sdkgo.MutationAttempt[Page] {
	if input.PageID <= 0 || strings.TrimSpace(input.Title) == "" || input.Status != "" && input.Status != "draft" && input.Status != "publish" {
		return contentWriteBranch[Page]("updatePage", UpdatePageBranchDefect, sdkgo.FailureValidation, "page title or status is invalid")
	}
	payload, err := encodeContentPayload(map[string]any{"title": input.Title, "content": input.Content, "status": input.Status})
	if err != nil || int64(len(payload)) > operation.client.maxPostBytes {
		return contentWriteBranch[Page]("updatePage", UpdatePageBranchDefect, sdkgo.FailureValidation, "page request is too large")
	}
	return writeContent(operation.client, call, "updatePage", http.MethodPost, "/wp-json/wp/v2/pages/"+strconv.FormatInt(input.PageID, 10), "/wp-json/wp/v2/pages/", payload, 0,
		contentWriteBranches{success: UpdatePageBranchUpdated, notFound: UpdatePageBranchNotFound, rejected: UpdatePageBranchProviderRejected, invalid: UpdatePageBranchInvalidResponse, defect: UpdatePageBranchDefect}, decodeWrittenPage, func(value Page) int64 { return value.ID })
}

// DeletePageOperation writes one WordPress page.
type DeletePageOperation struct{ client *Client }

// DeletePage returns the page Mutation bound to this client.
func (client *Client) DeletePage() DeletePageOperation { return DeletePageOperation{client} }

// Definition returns the generated deletePage contract.
func (DeletePageOperation) Definition() sdkgo.MutationDefinition { return DeletePageDefinition }

// IdempotencyKey records the stable call identity.
func (DeletePageOperation) IdempotencyKey(callID sdkgo.CallID, _ DeletePageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the provider page.
func (operation DeletePageOperation) Invoke(call sdkgo.Call, input DeletePageInput) sdkgo.MutationAttempt[Page] {
	if input.PageID <= 0 {
		return contentWriteBranch[Page]("deletePage", DeletePageBranchDefect, sdkgo.FailureValidation, "pageId must be positive")
	}
	var payload []byte
	return writeContent(operation.client, call, "deletePage", http.MethodDelete, "/wp-json/wp/v2/pages/"+strconv.FormatInt(input.PageID, 10), "/wp-json/wp/v2/pages/", payload, 0,
		contentWriteBranches{success: DeletePageBranchDeleted, notFound: DeletePageBranchNotFound, rejected: DeletePageBranchProviderRejected, invalid: DeletePageBranchInvalidResponse, defect: DeletePageBranchDefect}, decodeWrittenPage, func(value Page) int64 { return value.ID })
}

// UpdateUserOperation writes one WordPress user.
type UpdateUserOperation struct{ client *Client }

// UpdateUser returns the user Mutation bound to this client.
func (client *Client) UpdateUser() UpdateUserOperation { return UpdateUserOperation{client} }

// Definition returns the generated updateUser contract.
func (UpdateUserOperation) Definition() sdkgo.MutationDefinition { return UpdateUserDefinition }

// IdempotencyKey records the stable call identity.
func (UpdateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the provider user.
func (operation UpdateUserOperation) Invoke(call sdkgo.Call, input UpdateUserInput) sdkgo.MutationAttempt[User] {
	if input.UserID <= 0 || strings.TrimSpace(input.Name) == "" {
		return contentWriteBranch[User]("updateUser", UpdateUserBranchDefect, sdkgo.FailureValidation, "userId and name are required")
	}
	payload, err := encodeContentPayload(map[string]any{"name": input.Name})
	if err != nil || int64(len(payload)) > operation.client.maxPostBytes {
		return contentWriteBranch[User]("updateUser", UpdateUserBranchDefect, sdkgo.FailureValidation, "user request is too large")
	}
	return writeContent(operation.client, call, "updateUser", http.MethodPost, "/wp-json/wp/v2/users/"+strconv.FormatInt(input.UserID, 10), "/wp-json/wp/v2/users/", payload, 0,
		contentWriteBranches{success: UpdateUserBranchUpdated, notFound: UpdateUserBranchNotFound, rejected: UpdateUserBranchProviderRejected, invalid: UpdateUserBranchInvalidResponse, defect: UpdateUserBranchDefect}, decodeWrittenUser, func(value User) int64 { return value.ID })
}
