// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetPostDefinition } from './generated/definitions.js';
import type { GetPostInput, Post } from './generated/models.js';
import type { RedditClient, RedditExchange } from './client.js';

const emptyPost = (): Post => ({ id: '', title: '', subreddit: '', permalink: '', score: 0 });
const postIdPattern = /^t3_[a-z0-9]{1,20}$/;

/** Read one post with an approved Reddit OAuth application. */
export class GetPostOperation implements Query<GetPostInput, Post> {
  /** Bind the provider client. */ constructor(private readonly client: RedditClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetPostDefinition; }
  /** Read one bounded listing and project selected public metadata. */
  async invoke(call: Call, input: GetPostInput): Promise<QueryAttempt<Post>> {
    if (!input || !postIdPattern.test(input.postId)) return branch('defect', 'VALIDATION', 'Reddit post ID must be a full t3_ identifier');
    const query = new URLSearchParams({ id: input.postId, raw_json: '1' });
    const exchange = await this.client.get(call, `/api/info?${query.toString()}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const data = isObject(document?.data) ? document.data : undefined;
    const children = data?.children;
    if (document?.kind !== 'Listing' || !Array.isArray(children) || children.length > 1)
      return branch('invalidResponse', 'PROTOCOL', 'Reddit returned invalid post JSON');
    if (children.length === 0) return newQueryBranch('missing', emptyPost());
    const child: unknown = children[0];
    const post = isObject(child) && isObject(child.data) ? child.data : undefined;
    if (!isObject(child) || child.kind !== 't3' || !post || post.name !== input.postId ||
      typeof post.title !== 'string' || !post.title || typeof post.subreddit !== 'string' || !post.subreddit ||
      typeof post.permalink !== 'string' || !post.permalink.startsWith('/r/') || Array.from(post.permalink).some(character => character.charCodeAt(0) === 0 || character === '\r' || character === '\n') ||
      typeof post.score !== 'number' || !Number.isSafeInteger(post.score))
      return branch('invalidResponse', 'PROTOCOL', 'Reddit returned invalid post JSON');
    return newQueryBranch('found', { id: input.postId, title: post.title, subreddit: post.subreddit, permalink: post.permalink, score: post.score });
  }
}

function classifyRead(exchange: RedditExchange): QueryAttempt<Post> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'Reddit OAuth credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Reddit request failed');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Reddit rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Reddit is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Reddit response exceeded the configured limit');
  if (exchange.error) return retry('TRANSPORT', 'Reddit response was interrupted');
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Reddit rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<Post> {
  return newQueryBranch(branchId, emptyPost(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Post> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'reddit', operation: 'getPost', message };
}
