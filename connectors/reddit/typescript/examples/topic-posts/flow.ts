// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListSubredditPostsStep, newSearchPostsStep, type Connection, type ListSubredditPostsInput, type ListedPost, type PostPage, type SearchPostsInput,
} from '@superdurable/connector-reddit';

/** Static Dex Web connection for the topic posts example. */
export const ConnectionName = 'reddit';
/** How far back the topic search reaches. */
export const SearchTimeWindow = 'week';

/** Start Flow input, such as {"subreddit":"golang","topic":"generics"}. */
export interface TopicPostsRequest {
  /** One community name without r/, such as golang. */ subreddit: string;
  /** Search text; newest posts also match when their title or self text contains it, ignoring case. */ topic: string;
}

/** Posts about the topic that the Flow found. */
export interface TopicPostsOutput {
  /** Newest-page mentions and the past week's search matches, newest first, each post once, without pinned posts. */ posts: ListedPost[];
  /** Continues the search to older matches when present. */ searchAfter?: string;
}

/** Stored request for inspecting the Flow. */
export const topicRequestAttribute = new Attribute<TopicPostsRequest>('reddit-topic-request', jsonCodec<TopicPostsRequest>());
/** Newest-page posts that mention the topic. */
export const recentMentionsAttribute = new Attribute<ListedPost[]>('reddit-topic-recent-mentions', jsonCodec<ListedPost[]>());
/** Combined result for inspecting the Flow. */
export const topicPostsAttribute = new Attribute<TopicPostsOutput>('reddit-topic-posts', jsonCodec<TopicPostsOutput>());

/** List a subreddit's newest posts, search it for the topic, and complete with the combined matches. */
export class TopicPostsFlow implements Flow<TopicPostsRequest> {
  private readonly start = new StartTopicPosts();
  private readonly record = new RecordRecentMentions();
  private readonly complete = new CompleteTopicPosts();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Reddit topic posts connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'RedditTopicPostsTypeScript'; }
  /** Register the start, provider, and result Steps. */
  getSteps(): StepList<TopicPostsRequest> {
    return StepList.startStep(this.start).otherSteps(
      newListSubredditPostsStep<ListSubredditPostsInput>({
        stepType: 'ListSubredditPostsReddit',
        annotations: { groupId: 'reddit', groupLabel: 'Reddit', explanation: "Read the subreddit's newest page of posts." },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input, listed: this.record,
      }),
      this.record,
      newSearchPostsStep<SearchPostsInput>({
        stepType: 'SearchPostsReddit',
        annotations: { groupId: 'reddit', groupLabel: 'Reddit', explanation: "Search the subreddit's past week of posts for the topic, newest first." },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input, searched: this.complete,
      }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [topicRequestAttribute, recentMentionsAttribute, topicPostsAttribute] }; }
}

/** Return the posts that are not pinned and whose title or self text contains topic, ignoring case, in listing order. */
export function selectTopicMentions(posts: readonly ListedPost[], topic: string): ListedPost[] {
  const needle = topic.toLowerCase();
  return posts.filter(post => !post.isStickied && (post.title.toLowerCase().includes(needle) || (post.selfText ?? '').toLowerCase().includes(needle)));
}

/** Combine mentions and search matches without pinned posts, keeping each post ID once, newest first. */
export function mergeTopicPosts(mentions: readonly ListedPost[], matches: readonly ListedPost[]): ListedPost[] {
  const seen = new Set<string>();
  const merged = [...mentions, ...matches].filter(post => {
    if (post.isStickied || seen.has(post.id)) return false;
    seen.add(post.id);
    return true;
  });
  // RFC 3339 UTC strings to the second order like their instants; ties keep the lower ID first.
  return merged.sort((left, right) => right.createdAt.localeCompare(left.createdAt) || left.id.localeCompare(right.id));
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate and store the topic request, then read the subreddit's newest posts."
class StartTopicPosts implements Step<TopicPostsRequest> {
  getStepType(): string { return 'StartRedditTopicPosts'; }
  execute(context: Context, request: TopicPostsRequest): StepDecision {
    if (!request?.subreddit?.trim() || !request.topic?.trim()) return forceFail('a subreddit name and a non-blank topic are required');
    topicRequestAttribute.set(context, request);
    const listing: ListSubredditPostsInput = { subreddit: request.subreddit, sort: 'new' };
    return goTo(new StepRef('ListSubredditPostsReddit'), listing);
  }
}

// dex:group group-id:reddit group-label:"Reddit"
// dex:explanation text:"Keep the newest posts that mention the topic, then search the subreddit for it."
class RecordRecentMentions implements Step<QueryResult<PostPage>> {
  readonly inputCodec = jsonCodec<QueryResult<PostPage>>();
  getStepType(): string { return 'RecordRedditRecentMentions'; }
  execute(context: Context, result: QueryResult<PostPage>): StepDecision {
    const request = topicRequestAttribute.get(context);
    recentMentionsAttribute.set(context, selectTopicMentions(result.value.posts ?? [], request.topic));
    const search: SearchPostsInput = { query: request.topic, subreddit: request.subreddit, sort: 'new', timeWindow: SearchTimeWindow };
    return goTo(new StepRef('SearchPostsReddit'), search);
  }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Combine the mentions with the search matches and complete with them."
class CompleteTopicPosts implements Step<QueryResult<PostPage>> {
  readonly inputCodec = jsonCodec<QueryResult<PostPage>>();
  getStepType(): string { return 'CompleteRedditTopicPosts'; }
  execute(context: Context, result: QueryResult<PostPage>): StepDecision {
    const output: TopicPostsOutput = {
      posts: mergeTopicPosts(recentMentionsAttribute.get(context), result.value.posts ?? []),
      ...(result.value.after ? { searchAfter: result.value.after } : {}),
    };
    topicPostsAttribute.set(context, output);
    return gracefulComplete(output);
  }
}
