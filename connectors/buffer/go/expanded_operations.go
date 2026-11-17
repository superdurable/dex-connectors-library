// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListOrganizationsInput requests organizations available to the API key.
type ListOrganizationsInput struct{}

// OrganizationIdentity contains one organization ID and name.
type OrganizationIdentity struct {
	// ID is Buffer's organization identifier.
	ID string `json:"id"`
	// Name is the organization display name.
	Name string `json:"name"`
}

// OrganizationsList contains the account's organization identities.
type OrganizationsList struct {
	// Organizations are those visible to the API key.
	Organizations []OrganizationIdentity `json:"organizations"`
}

// ListChannelsInput selects an organization.
type ListChannelsInput struct {
	// OrganizationID is returned by ListOrganizations.
	OrganizationID string `json:"organizationId"`
}

// ChannelsList contains the organization's visible channels.
type ChannelsList struct {
	// Channels are the channel IDs, names, and services.
	Channels []ChannelProfile `json:"channels"`
}

// GetPostInput selects one Buffer post.
type GetPostInput struct {
	// PostID is returned by create or list operations.
	PostID string `json:"postId"`
}

// ListPostsInput selects a bounded page of organization posts.
type ListPostsInput struct {
	// OrganizationID is returned by ListOrganizations.
	OrganizationID string `json:"organizationId"`
	// First is the page size from 1 to 100; zero uses 20.
	First int `json:"first"`
	// After is an opaque cursor; blank begins at the first page.
	After string `json:"after,omitempty"`
}

// PostProfile contains a post's selected public fields.
type PostProfile struct {
	// ID is the Buffer post ID.
	ID string `json:"id"`
	// Text is the post's text content.
	Text string `json:"text"`
	// ChannelID is the connected channel ID.
	ChannelID string `json:"channelId"`
}

// PostsList contains one page of posts and a next-page cursor.
type PostsList struct {
	// Posts are the selected post fields in this page.
	Posts []PostProfile `json:"posts"`
	// EndCursor is passed as After to continue when HasNextPage is true.
	EndCursor string `json:"endCursor,omitempty"`
	// HasNextPage reports whether another page is available.
	HasNextPage bool `json:"hasNextPage"`
}

// CreatePostInput queues a post with text or public media on a connected channel.
type CreatePostInput struct {
	// ChannelID is the target channel ID.
	ChannelID string `json:"channelId"`
	// Text is post content; at least one asset is required when blank.
	Text string `json:"text"`
	// Assets are publicly hosted images, videos, or documents.
	Assets []PostAsset `json:"assets,omitempty"`
}

// EditPostInput replaces the text of one Buffer post.
type EditPostInput struct {
	// PostID is the post to edit.
	PostID string `json:"postId"`
	// Text is replacement content; at least one asset is required when blank.
	Text string `json:"text"`
	// Assets replace attached media when nonnil.
	Assets []PostAsset `json:"assets,omitempty"`
}

// DeletePostInput selects one post for deletion from Buffer.
type DeletePostInput struct {
	// PostID is the post to delete.
	PostID string `json:"postId"`
}

// PostDeleteResult identifies the post Buffer deleted.
type PostDeleteResult struct {
	// ID is the deleted Buffer post ID.
	ID string `json:"id"`
}

// ListOrganizationsOperation retrieves organization identities.
type ListOrganizationsOperation struct{ client *Client }

// ListChannelsOperation retrieves channels in one organization.
type ListChannelsOperation struct{ client *Client }

// GetPostOperation retrieves one post.
type GetPostOperation struct{ client *Client }

// ListPostsOperation retrieves one page of posts.
type ListPostsOperation struct{ client *Client }

// CreatePostOperation queues one text-only post.
type CreatePostOperation struct{ client *Client }

// EditPostOperation replaces post text.
type EditPostOperation struct{ client *Client }

// DeletePostOperation removes a post from Buffer.
type DeletePostOperation struct{ client *Client }

// ListOrganizations binds organization discovery to this client.
func (client *Client) ListOrganizations() ListOrganizationsOperation {
	return ListOrganizationsOperation{client}
}

// ListChannels binds channel discovery to this client.
func (client *Client) ListChannels() ListChannelsOperation { return ListChannelsOperation{client} }

// GetPost binds post lookup to this client.
func (client *Client) GetPost() GetPostOperation { return GetPostOperation{client} }

// ListPosts binds post listing to this client.
func (client *Client) ListPosts() ListPostsOperation { return ListPostsOperation{client} }

// CreatePost binds post creation to this client.
func (client *Client) CreatePost() CreatePostOperation { return CreatePostOperation{client} }

// EditPost binds post editing to this client.
func (client *Client) EditPost() EditPostOperation { return EditPostOperation{client} }

// DeletePost binds post deletion to this client.
func (client *Client) DeletePost() DeletePostOperation { return DeletePostOperation{client} }

// Definition returns the generated organization discovery contract.
func (ListOrganizationsOperation) Definition() sdkgo.QueryDefinition {
	return ListOrganizationsDefinition
}

// Definition returns the generated channel discovery contract.
func (ListChannelsOperation) Definition() sdkgo.QueryDefinition { return ListChannelsDefinition }

// Definition returns the generated post lookup contract.
func (GetPostOperation) Definition() sdkgo.QueryDefinition { return GetPostDefinition }

// Definition returns the generated post listing contract.
func (ListPostsOperation) Definition() sdkgo.QueryDefinition { return ListPostsDefinition }

// Definition returns the generated post creation contract.
func (CreatePostOperation) Definition() sdkgo.MutationDefinition { return CreatePostDefinition }

// Definition returns the generated post edit contract.
func (EditPostOperation) Definition() sdkgo.MutationDefinition { return EditPostDefinition }

// Definition returns the generated post deletion contract.
func (DeletePostOperation) Definition() sdkgo.MutationDefinition { return DeletePostDefinition }

// IdempotencyKey supplies a stable identity for bounded post creation attempts.
func (CreatePostOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across absolute post edits.
func (EditPostOperation) IdempotencyKey(callID sdkgo.CallID, _ EditPostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across post deletion attempts.
func (DeletePostOperation) IdempotencyKey(callID sdkgo.CallID, _ DeletePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

const listOrganizationsGraphQL = "query ListOrganizations { account { organizations { id name } } }"
const listChannelsGraphQL = "query ListChannels($input: ChannelsInput!) { channels(input: $input) { id name service } }"
const getPostGraphQL = "query GetPost($input: PostInput!) { post(input: $input) { id text channelId } }"
const listPostsGraphQL = "query ListPosts($input: PostsInput!, $first: Int, $after: String) { posts(input: $input, first: $first, after: $after) { pageInfo { endCursor hasNextPage } edges { node { id text channelId } } } }"
const createPostGraphQL = "mutation CreatePost($input: CreatePostInput!) { createPost(input: $input) { __typename ... on PostActionSuccess { post { id text channelId } } } }"
const editPostGraphQL = "mutation EditPost($input: EditPostInput!) { editPost(input: $input) { __typename ... on PostActionSuccess { post { id text channelId } } } }"
const deletePostGraphQL = "mutation DeletePost($input: DeletePostInput!) { deletePost(input: $input) { __typename } }"

// Invoke lists organizations without exposing owner email.
func (operation ListOrganizationsOperation) Invoke(call sdkgo.Call, _ ListOrganizationsInput) sdkgo.QueryAttempt[OrganizationsList] {
	return invokeBufferQuery(call, operation.client, "listOrganizations", listOrganizationsGraphQL, struct{}{}, ListOrganizationsBranchListed, "", ListOrganizationsBranchProviderRejected, ListOrganizationsBranchInvalidResponse, ListOrganizationsBranchDefect, func(body []byte) (OrganizationsList, bool, error) {
		var wire struct {
			Data *struct {
				Account *struct {
					Organizations []OrganizationIdentity `json:"organizations"`
				} `json:"account"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil || wire.Data.Account == nil || wire.Data.Account.Organizations == nil {
			return OrganizationsList{}, false, errors.New("invalid organizations")
		}
		for _, org := range wire.Data.Account.Organizations {
			if org.ID == "" || org.Name == "" {
				return OrganizationsList{}, false, errors.New("missing organization identity")
			}
		}
		return OrganizationsList{Organizations: wire.Data.Account.Organizations}, false, nil
	})
}

// Invoke lists channels visible in an organization.
func (operation ListChannelsOperation) Invoke(call sdkgo.Call, input ListChannelsInput) sdkgo.QueryAttempt[ChannelsList] {
	if !validGraphQLID(input.OrganizationID) {
		return bufferQueryBranch(ListChannelsBranchDefect, ChannelsList{}, "listChannels", sdkgo.FailureValidation, "organization ID is invalid")
	}
	return invokeBufferQuery(call, operation.client, "listChannels", listChannelsGraphQL, map[string]any{"input": map[string]any{"organizationId": input.OrganizationID}}, ListChannelsBranchListed, "", ListChannelsBranchProviderRejected, ListChannelsBranchInvalidResponse, ListChannelsBranchDefect, func(body []byte) (ChannelsList, bool, error) {
		var wire struct {
			Data *struct {
				Channels []ChannelProfile `json:"channels"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil || wire.Data.Channels == nil {
			return ChannelsList{}, false, errors.New("invalid channels")
		}
		for _, channel := range wire.Data.Channels {
			if channel.ID == "" || channel.Name == "" || channel.Service == "" {
				return ChannelsList{}, false, errors.New("missing channel identity")
			}
		}
		return ChannelsList{Channels: wire.Data.Channels}, false, nil
	})
}

// Invoke reads one post without assets or author details.
func (operation GetPostOperation) Invoke(call sdkgo.Call, input GetPostInput) sdkgo.QueryAttempt[PostProfile] {
	if !validGraphQLID(input.PostID) {
		return bufferQueryBranch(GetPostBranchDefect, PostProfile{}, "getPost", sdkgo.FailureValidation, "post ID is invalid")
	}
	return invokeBufferQuery(call, operation.client, "getPost", getPostGraphQL, map[string]any{"input": map[string]any{"id": input.PostID}}, GetPostBranchFound, GetPostBranchNotFound, GetPostBranchProviderRejected, GetPostBranchInvalidResponse, GetPostBranchDefect, func(body []byte) (PostProfile, bool, error) {
		var wire struct {
			Data *struct {
				Post *PostProfile `json:"post"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
			return PostProfile{}, false, errors.New("invalid post")
		}
		if wire.Data.Post == nil {
			return PostProfile{}, true, nil
		}
		if wire.Data.Post.ID != input.PostID || wire.Data.Post.ChannelID == "" {
			return PostProfile{}, false, errors.New("post identity mismatch")
		}
		return *wire.Data.Post, false, nil
	})
}

// Invoke reads one page of posts and the next-page cursor.
func (operation ListPostsOperation) Invoke(call sdkgo.Call, input ListPostsInput) sdkgo.QueryAttempt[PostsList] {
	if !validGraphQLID(input.OrganizationID) || input.First < 0 || input.First > 100 || len(input.After) > 512 {
		return bufferQueryBranch(ListPostsBranchDefect, PostsList{}, "listPosts", sdkgo.FailureValidation, "organization, page size, or cursor is invalid")
	}
	if input.First == 0 {
		input.First = 20
	}
	return invokeBufferQuery(call, operation.client, "listPosts", listPostsGraphQL, map[string]any{"input": map[string]any{"organizationId": input.OrganizationID}, "first": input.First, "after": input.After}, ListPostsBranchListed, "", ListPostsBranchProviderRejected, ListPostsBranchInvalidResponse, ListPostsBranchDefect, func(body []byte) (PostsList, bool, error) {
		var wire struct {
			Data *struct {
				Posts *struct {
					PageInfo struct {
						EndCursor   string `json:"endCursor"`
						HasNextPage bool   `json:"hasNextPage"`
					} `json:"pageInfo"`
					Edges []struct {
						Node PostProfile `json:"node"`
					} `json:"edges"`
				} `json:"posts"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil || wire.Data.Posts == nil || wire.Data.Posts.Edges == nil {
			return PostsList{}, false, errors.New("invalid posts page")
		}
		posts := make([]PostProfile, 0, len(wire.Data.Posts.Edges))
		for _, edge := range wire.Data.Posts.Edges {
			if edge.Node.ID == "" || edge.Node.ChannelID == "" {
				return PostsList{}, false, errors.New("missing post identity")
			}
			posts = append(posts, edge.Node)
		}
		if wire.Data.Posts.PageInfo.HasNextPage && wire.Data.Posts.PageInfo.EndCursor == "" {
			return PostsList{}, false, errors.New("missing next cursor")
		}
		return PostsList{Posts: posts, EndCursor: wire.Data.Posts.PageInfo.EndCursor, HasNextPage: wire.Data.Posts.PageInfo.HasNextPage}, false, nil
	})
}

// Invoke queues one text-only post with bounded resend after uncertain dispatch.
func (operation CreatePostOperation) Invoke(call sdkgo.Call, input CreatePostInput) sdkgo.MutationAttempt[PostProfile] {
	const name = "createPost"
	if !validGraphQLID(input.ChannelID) || strings.TrimSpace(input.Text) == "" && len(input.Assets) == 0 {
		return bufferMutationBranch(CreatePostBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, "channel ID and text or assets are required")
	}
	mediaInputs, mediaErr := bufferPostAssetInputs(input.Assets)
	if mediaErr != nil {
		return bufferMutationBranch(CreatePostBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, mediaErr.Error())
	}
	fence, err := singledispatch.NewFence(call, CreatePostDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return bufferMutationBranch(CreatePostBranchDefect, PostProfile{}, name, sdkgo.FailureLocalDefect, "post dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(PostProfile{}, bufferFailure(name, sdkgo.FailureTransport, "an earlier post may have been created"), sdkgo.Receipt{})
	}
	postInput := map[string]any{"channelId": input.ChannelID, "text": input.Text, "mode": "addToQueue", "schedulingType": "automatic"}
	if mediaInputs != nil {
		postInput["assets"] = mediaInputs
	}
	variables := map[string]any{"input": postInput}
	var response providerResponse
	err = fence.Dispatch(func(ctx context.Context) error {
		response = operation.client.executeGraphQLWithContext(ctx, call, createPostGraphQL, variables)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, sdkgo.FailureAvailability, "post dispatch was not recorded or sent"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, sdkgo.FailureAvailability, "post retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, kind, "Buffer asked to retry the post"), response.retryAfter)
	}
	if err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 || hasUnconfirmedCreatePostResponse(response) {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(PostProfile{}, bufferFailure(name, sdkgo.FailureTransport, "post outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, sdkgo.FailureTransport, "post outcome is unknown"), 0)
	}
	return classifyPostMutation(response, name, "createPost", input.ChannelID, CreatePostBranchCreated, CreatePostBranchProviderRejected, CreatePostBranchProviderRejected, CreatePostBranchDefect)
}

func hasUnconfirmedCreatePostResponse(response providerResponse) bool {
	if response.status != http.StatusOK {
		return false
	}
	var wire struct {
		Data *struct {
			CreatePost *struct {
				TypeName string      `json:"__typename"`
				Post     PostProfile `json:"post"`
			} `json:"createPost"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return true
	}
	if len(wire.Errors) != 0 {
		return false
	}
	if wire.Data == nil || wire.Data.CreatePost == nil || wire.Data.CreatePost.TypeName == "" {
		return true
	}
	if wire.Data.CreatePost.TypeName != "PostActionSuccess" {
		return false
	}
	return wire.Data.CreatePost.Post.ID == "" || wire.Data.CreatePost.Post.ChannelID == ""
}

// Invoke replaces the text of one existing post.
func (operation EditPostOperation) Invoke(call sdkgo.Call, input EditPostInput) sdkgo.MutationAttempt[PostProfile] {
	const name = "editPost"
	if !validGraphQLID(input.PostID) || strings.TrimSpace(input.Text) == "" && len(input.Assets) == 0 {
		return bufferMutationBranch(EditPostBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, "post ID and text or assets are required")
	}
	mediaInputs, mediaErr := bufferPostAssetInputs(input.Assets)
	if mediaErr != nil {
		return bufferMutationBranch(EditPostBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, mediaErr.Error())
	}
	postInput := map[string]any{"id": input.PostID, "text": input.Text}
	if mediaInputs != nil {
		postInput["assets"] = mediaInputs
	}
	response := operation.client.executeGraphQL(call, editPostGraphQL, map[string]any{"input": postInput})
	return classifyPostMutation(response, name, "editPost", "", EditPostBranchUpdated, EditPostBranchProviderRejected, sdkgo.BranchID("invalidResponse"), EditPostBranchDefect)
}

// Invoke deletes one post from Buffer; a published network post remains online.
func (operation DeletePostOperation) Invoke(call sdkgo.Call, input DeletePostInput) sdkgo.MutationAttempt[PostDeleteResult] {
	const name = "deletePost"
	if !validGraphQLID(input.PostID) {
		return bufferMutationBranch(DeletePostBranchDefect, PostDeleteResult{}, name, sdkgo.FailureValidation, "post ID is invalid")
	}
	response := operation.client.executeGraphQL(call, deletePostGraphQL, map[string]any{"input": map[string]any{"id": input.PostID}})
	if attempt := classifyBufferMutationResponse[PostDeleteResult](response, name, DeletePostBranchProviderRejected, sdkgo.BranchID("invalidResponse"), DeletePostBranchDefect); attempt != nil {
		return *attempt
	}
	var wire struct {
		Data *struct {
			DeletePost *struct {
				TypeName string `json:"__typename"`
			} `json:"deletePost"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || wire.Data.DeletePost == nil {
		return bufferMutationBranch(sdkgo.BranchID("invalidResponse"), PostDeleteResult{}, name, sdkgo.FailureProtocol, "Buffer returned invalid delete JSON")
	}
	if len(wire.Errors) != 0 || wire.Data.DeletePost.TypeName != "DeletePostSuccess" {
		return bufferMutationBranch(DeletePostBranchProviderRejected, PostDeleteResult{}, name, sdkgo.FailureProviderRejection, "Buffer rejected post deletion")
	}
	return sdkgo.NewMutationBranch(DeletePostBranchDeleted, PostDeleteResult{ID: input.PostID}, nil, sdkgo.Receipt{})
}

func invokeBufferQuery[T any](call sdkgo.Call, client *Client, name, query string, variables any, found, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, bool, error)) sdkgo.QueryAttempt[T] {
	var zero T
	response := client.executeGraphQL(call, query, variables)
	if errors.Is(response.err, errCredential) {
		return bufferQueryBranch(defect, zero, name, sdkgo.FailureAuthentication, "Buffer credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](bufferFailure(name, sdkgo.FailureTransport, "Buffer request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](bufferFailure(name, sdkgo.FailureRateLimit, "Buffer rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](bufferFailure(name, sdkgo.FailureAvailability, "Buffer is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return bufferQueryBranch(rejected, zero, name, sdkgo.FailureProviderRejection, "Buffer rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return bufferQueryBranch(invalid, zero, name, sdkgo.FailureResponseTooLarge, "Buffer response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](bufferFailure(name, sdkgo.FailureTransport, "Buffer response was interrupted"), 0)
	}
	var envelope struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &envelope) != nil {
		return bufferQueryBranch(invalid, zero, name, sdkgo.FailureProtocol, "Buffer returned invalid JSON")
	}
	if len(envelope.Errors) != 0 {
		switch classifyGraphQLErrors(envelope.Errors) {
		case graphQLErrorNotFound:
			if notFound != "" {
				return bufferQueryBranch(notFound, zero, name, sdkgo.FailureNotFound, "Buffer did not find the resource")
			}
		case graphQLErrorRateLimited:
			return sdkgo.NewQueryRetry[T](bufferFailure(name, sdkgo.FailureRateLimit, "Buffer rate limited the query"), response.retryAfter)
		case graphQLErrorUnavailable:
			return sdkgo.NewQueryRetry[T](bufferFailure(name, sdkgo.FailureAvailability, "Buffer query is unavailable"), response.retryAfter)
		}
		return bufferQueryBranch(rejected, zero, name, sdkgo.FailureProviderRejection, "Buffer rejected the GraphQL query")
	}
	value, missing, err := decode(response.body)
	if err != nil {
		return bufferQueryBranch(invalid, zero, name, sdkgo.FailureProtocol, "Buffer returned invalid result JSON")
	}
	if missing && notFound != "" {
		return bufferQueryBranch(notFound, zero, name, sdkgo.FailureNotFound, "Buffer did not find the resource")
	}
	return sdkgo.NewQueryBranch(found, value, nil, sdkgo.Receipt{})
}

func classifyPostMutation(response providerResponse, name, field, channelID string, success, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[PostProfile] {
	if attempt := classifyBufferMutationResponse[PostProfile](response, name, rejected, invalid, defect); attempt != nil {
		return *attempt
	}
	var wire struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []json.RawMessage          `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil {
		return bufferMutationBranch(invalid, PostProfile{}, name, sdkgo.FailureProtocol, "Buffer returned invalid post JSON")
	}
	if len(wire.Errors) != 0 {
		return bufferMutationBranch(rejected, PostProfile{}, name, sdkgo.FailureProviderRejection, "Buffer rejected the GraphQL mutation")
	}
	var payload struct {
		TypeName string      `json:"__typename"`
		Post     PostProfile `json:"post"`
	}
	if json.Unmarshal(wire.Data[field], &payload) != nil || payload.TypeName == "" {
		return bufferMutationBranch(invalid, PostProfile{}, name, sdkgo.FailureProtocol, "Buffer returned invalid post payload")
	}
	if payload.TypeName != "PostActionSuccess" {
		return bufferMutationBranch(rejected, PostProfile{}, name, sdkgo.FailureProviderRejection, "Buffer rejected the post")
	}
	if payload.Post.ID == "" || payload.Post.ChannelID == "" || channelID != "" && payload.Post.ChannelID != channelID {
		return bufferMutationBranch(invalid, PostProfile{}, name, sdkgo.FailureProtocol, "Buffer returned an invalid post identity")
	}
	return sdkgo.NewMutationBranch(success, payload.Post, nil, sdkgo.Receipt{})
}

func classifyBufferMutationResponse[T any](response providerResponse, name string, rejected, invalid, defect sdkgo.BranchID) *sdkgo.MutationAttempt[T] {
	var zero T
	var attempt sdkgo.MutationAttempt[T]
	switch {
	case errors.Is(response.err, errCredential):
		attempt = bufferMutationBranch(defect, zero, name, sdkgo.FailureAuthentication, "Buffer credentials are unavailable or invalid")
	case errors.Is(response.err, errRequest):
		attempt = sdkgo.NewMutationRetry[T](bufferFailure(name, sdkgo.FailureTransport, "Buffer request failed"), 0)
	case response.status == http.StatusTooManyRequests:
		attempt = sdkgo.NewMutationRetry[T](bufferFailure(name, sdkgo.FailureRateLimit, "Buffer rate limited the request"), response.retryAfter)
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		attempt = sdkgo.NewMutationRetry[T](bufferFailure(name, sdkgo.FailureAvailability, "Buffer is temporarily unavailable"), response.retryAfter)
	case response.status != http.StatusOK:
		attempt = bufferMutationBranch(rejected, zero, name, sdkgo.FailureProviderRejection, "Buffer rejected HTTP "+strconv.Itoa(response.status))
	case errors.Is(response.err, providerhttp.ErrBodyTooLarge):
		attempt = bufferMutationBranch(invalid, zero, name, sdkgo.FailureResponseTooLarge, "Buffer response exceeded the configured limit")
	case response.err != nil:
		attempt = sdkgo.NewMutationRetry[T](bufferFailure(name, sdkgo.FailureTransport, "Buffer response was interrupted"), 0)
	default:
		return nil
	}
	return &attempt
}

func bufferQueryBranch[T any](branch sdkgo.BranchID, value T, name string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := bufferFailure(name, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func bufferMutationBranch[T any](branch sdkgo.BranchID, value T, name string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := bufferFailure(name, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func bufferFailure(name string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "buffer", Operation: name, Message: message}
}
