// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package topicposts demonstrates the Reddit listSubredditPosts and searchPosts Steps in a runnable Dex Flow
// that collects recent posts about one topic in one subreddit.
package topicposts

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/reddit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "RedditTopicPosts"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName = "reddit"
	// SearchTimeWindow is how far back the topic search reaches.
	SearchTimeWindow = reddit.PostTimeWindowWeek
)

const (
	listSubredditPostsStepType = "ListSubredditPostsReddit"
	searchPostsStepType        = "SearchPostsReddit"
)

var (
	topicRequestAttribute   = dex.DefineAttribute[TopicPostsRequest]("reddit-topic-request")
	recentMentionsAttribute = dex.DefineAttribute[[]reddit.ListedPost]("reddit-topic-recent-mentions")
	topicPostsAttribute     = dex.DefineAttribute[TopicPostsOutput]("reddit-topic-posts")
)

// TopicPostsRequest is entered in Dex Web Start Flow, such as {"subreddit":"golang","topic":"generics"}.
type TopicPostsRequest struct {
	// Subreddit is one community name without r/, such as golang.
	Subreddit string `json:"subreddit"`
	// Topic is the search text, such as generics. Newest posts also match when their title or self text
	// contains it, ignoring case.
	Topic string `json:"topic"`
}

// TopicPostsOutput lists the posts about the topic that the Flow found.
type TopicPostsOutput struct {
	// Posts are the newest-page mentions and the past week's search matches, newest first, each post once,
	// without posts that moderators pinned.
	Posts []reddit.ListedPost `json:"posts"`
	// SearchAfter continues the search to older matches when non-blank.
	SearchAfter string `json:"searchAfter,omitempty"`
}

// Flow lists a subreddit's newest posts, searches it for the topic, and completes with the combined matches.
type Flow struct {
	dex.FlowDefaults
	connection reddit.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection reddit.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow type.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the request, recent mention, and result Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{topicRequestAttribute, recentMentionsAttribute, topicPostsAttribute}}
}

// GetSteps registers the start, provider, and result Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTopicPosts{}),
		dex.DefineStep(reddit.NewListSubredditPostsStep(reddit.ListSubredditPostsStepConfig[reddit.ListSubredditPostsInput]{
			StepType: listSubredditPostsStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "reddit", GroupLabel: "Reddit", Explanation: "Read the subreddit's newest page of posts."},
			MapToOperationInput: MapToListSubredditPostsInput,
			Listed:              sdkgo.GoTo(recordRecentMentions{}),
		})),
		dex.DefineStep(recordRecentMentions{}),
		dex.DefineStep(reddit.NewSearchPostsStep(reddit.SearchPostsStepConfig[reddit.SearchPostsInput]{
			StepType: searchPostsStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "reddit", GroupLabel: "Reddit", Explanation: "Search the subreddit's past week of posts for the topic, newest first."},
			MapToOperationInput: MapToSearchPostsInput,
			Searched:            sdkgo.GoTo(completeTopicPosts{}),
		})),
		dex.DefineStep(completeTopicPosts{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored request, newest-page mentions, and result when available.
// dex:field attribute-key:reddit-topic-request value-type:json editable:false description:"Reddit topic request"
// dex:field attribute-key:reddit-topic-recent-mentions value-type:array editable:false description:"Newest posts that mention the topic"
// dex:field attribute-key:reddit-topic-posts value-type:json editable:false description:"Reddit posts about the topic"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, mentions, output, err := topicInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"reddit-topic-request": request, "reddit-topic-recent-mentions": mentions, "reddit-topic-posts": output,
	}}, nil
}

// GetDexDisplay returns the stored request, newest-page mentions, and result when available.
// dex:field attribute-key:reddit-topic-request value-type:json editable:false description:"Reddit topic request"
// dex:field attribute-key:reddit-topic-recent-mentions value-type:array editable:false description:"Newest posts that mention the topic"
// dex:field attribute-key:reddit-topic-posts value-type:json editable:false description:"Reddit posts about the topic"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, mentions, output, err := topicInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"reddit-topic-request": request, "reddit-topic-recent-mentions": mentions, "reddit-topic-posts": output,
	}}, nil
}

func topicInspection(ctx dex.Context) (TopicPostsRequest, []reddit.ListedPost, TopicPostsOutput, error) {
	request, err := optionalAttribute(ctx, topicRequestAttribute)
	if err != nil {
		return TopicPostsRequest{}, nil, TopicPostsOutput{}, err
	}
	mentions, err := optionalAttribute(ctx, recentMentionsAttribute)
	if err != nil {
		return TopicPostsRequest{}, nil, TopicPostsOutput{}, err
	}
	output, err := optionalAttribute(ctx, topicPostsAttribute)
	return request, mentions, output, err
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		var zero T
		return zero, nil
	}
	return value, err
}

// MapToListSubredditPostsInput passes the listing selection built by the start Step to Reddit.
func MapToListSubredditPostsInput(input reddit.ListSubredditPostsInput) reddit.ListSubredditPostsInput {
	return input
}

// MapToSearchPostsInput passes the search built from the stored request to Reddit.
func MapToSearchPostsInput(input reddit.SearchPostsInput) reddit.SearchPostsInput {
	return input
}

// SelectTopicMentions returns the posts that are not pinned and whose title or self text contains topic,
// ignoring case, in their listing order.
func SelectTopicMentions(posts []reddit.ListedPost, topic string) []reddit.ListedPost {
	topic = strings.ToLower(topic)
	mentions := []reddit.ListedPost{}
	for _, post := range posts {
		if !post.IsStickied && (strings.Contains(strings.ToLower(post.Title), topic) || strings.Contains(strings.ToLower(post.SelfText), topic)) {
			mentions = append(mentions, post)
		}
	}
	return mentions
}

// MergeTopicPosts combines mentions and search matches without pinned posts, keeping each post ID once,
// newest first. Reddit can repeat a post across pages and across the listing and the search.
func MergeTopicPosts(mentions []reddit.ListedPost, matches []reddit.ListedPost) []reddit.ListedPost {
	merged := []reddit.ListedPost{}
	seen := map[string]bool{}
	for _, post := range slices.Concat(mentions, matches) {
		if post.IsStickied || seen[post.ID] {
			continue
		}
		seen[post.ID] = true
		merged = append(merged, post)
	}
	slices.SortStableFunc(merged, func(left, right reddit.ListedPost) int {
		return cmp.Or(right.CreatedAt.Compare(left.CreatedAt), strings.Compare(left.ID, right.ID))
	})
	return merged
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate and store the topic request, then read the subreddit's newest posts."
type startTopicPosts struct{ dex.StepDefaults }

func (startTopicPosts) GetStepType() string { return "StartRedditTopicPosts" }
func (startTopicPosts) WaitFor(dex.Context, TopicPostsRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTopicPosts) Execute(ctx dex.Context, request TopicPostsRequest) (*dex.StepDecision, error) {
	if strings.TrimSpace(request.Subreddit) == "" || strings.TrimSpace(request.Topic) == "" {
		return dex.ForceFail("a subreddit name and a non-blank topic are required"), nil
	}
	if err := topicRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	listing := reddit.ListSubredditPostsInput{Subreddit: request.Subreddit, Sort: reddit.SubredditPostSortNew}
	return dex.GoTo(sdkgo.StepRef[reddit.ListSubredditPostsInput](listSubredditPostsStepType), listing), nil
}

// dex:group group-id:reddit group-label:"Reddit"
// dex:explanation text:"Keep the newest posts that mention the topic, then search the subreddit for it."
type recordRecentMentions struct {
	dex.StepDefaultsNoWaitFor[reddit.ListSubredditPostsResult]
}

func (recordRecentMentions) GetStepType() string { return "RecordRedditRecentMentions" }
func (recordRecentMentions) Execute(ctx dex.Context, result reddit.ListSubredditPostsResult) (*dex.StepDecision, error) {
	request, err := topicRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := recentMentionsAttribute.Set(ctx, SelectTopicMentions(result.Value.Posts, request.Topic)); err != nil {
		return nil, err
	}
	search := reddit.SearchPostsInput{
		Query: request.Topic, Subreddit: request.Subreddit, Sort: reddit.SearchPostSortNew, TimeWindow: SearchTimeWindow,
	}
	return dex.GoTo(sdkgo.StepRef[reddit.SearchPostsInput](searchPostsStepType), search), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Combine the mentions with the search matches and complete with them."
type completeTopicPosts struct {
	dex.StepDefaultsNoWaitFor[reddit.SearchPostsResult]
}

func (completeTopicPosts) GetStepType() string { return "CompleteRedditTopicPosts" }
func (completeTopicPosts) Execute(ctx dex.Context, result reddit.SearchPostsResult) (*dex.StepDecision, error) {
	mentions, err := recentMentionsAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	output := TopicPostsOutput{Posts: MergeTopicPosts(mentions, result.Value.Posts), SearchAfter: result.Value.After}
	if err := topicPostsAttribute.Set(ctx, output); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(output), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
