// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, newQueryBranch, newQueryRetry, newMutationBranch, newMutationRetry, newMutationUncertain, type Call, type CallID, type CredentialProvider, type CredentialRefreshDriver, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence } from '@superdurable/connectors-sdk/singledispatch';
import { ListSocialProfilesDefinition, ListScheduledPostsDefinition, SchedulePostDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials, ListSocialProfilesInput, SocialProfile, SocialProfilesPage, ListScheduledPostsInput, ScheduledPost, ScheduledPostsPage, SchedulePostInput, ScheduledPostReceipt } from './generated/models.js';

const emptyProfiles = (): SocialProfilesPage => ({ profiles: [] });
const emptyScheduledPosts = (): ScheduledPostsPage => ({ posts: [], nextCursor: '' });
const emptyReceipt = (): ScheduledPostReceipt => ({ id: '', state: '', socialProfileId: '' });
const numericID = /^[0-9]{1,40}$/;
const utcInstant = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/;

/** Injectable transport and OAuth refresh dependencies. */
export interface HootsuiteClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}

/** One bounded Hootsuite exchange with positive send evidence. */
export interface HootsuiteExchange {
  /** Provider HTTP status, or zero before response headers. */ status: number;
  /** Bounded response body retained only in process memory. */ body: Uint8Array;
  /** Provider Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local error class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
  /** Whether request bytes may have reached Hootsuite. */ requestSent: boolean;
}

/** Hootsuite client with call-time OAuth resolution and bounded responses. */
export class HootsuiteClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting Hootsuite. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: HootsuiteClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Hootsuite configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://platform.hootsuite.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Hootsuite request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Hootsuite', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the social profile Query. */ listSocialProfiles(): ListSocialProfilesOperation { return new ListSocialProfilesOperation(this); }
  /** Bind the scheduled message Query. */ listScheduledPosts(): ListScheduledPostsOperation { return new ListScheduledPostsOperation(this); }
  /** Bind the scheduled message Mutation. */ schedulePost(): SchedulePostOperation { return new SchedulePostOperation(this); }

  /** Resolve a header-safe access token for the current call. */
  async resolveToken(call: Call): Promise<string | HootsuiteExchange> {
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      const token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      return token && isHeaderSafeCredential(token) ? token : failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
  }

  /** Read one bounded provider response for a Query. */
  async get(call: Call, path: string): Promise<HootsuiteExchange> {
    const token = await this.resolveToken(call);
    if (typeof token !== 'string') return token;
    return this.send('GET', path, token, call.context.cancellationSignal);
  }

  /** Send one authenticated request; the caller owns dispatch fencing for writes. */
  async send(method: string, path: string, token: string, signal: AbortSignal, body?: Uint8Array): Promise<HootsuiteExchange> {
    const request: ProviderRequest = {
      url: this.baseURL + path, method,
      headers: {
        Accept: 'application/json', Authorization: `Bearer ${token}`,
        'User-Agent': 'Dex Connector (https://superdurable.io)',
        ...(body === undefined ? {} : { 'Content-Type': 'application/json;charset=utf-8' }),
      },
      ...(body === undefined ? {} : { body }),
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return {
        ...failedExchange('tooLarge'), status: error.status, requestSent: true,
        retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()),
      };
      if (error instanceof ProviderTransportError) return { ...failedExchange('request'), requestSent: !error.isProvablyNotSent };
      return failedExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, requestSent: true, retryAfterMs };
    if (response.body.length > this.maxResponseBytes) return { ...failedExchange('tooLarge'), status: response.status, requestSent: true, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs, requestSent: true };
  }
}

/** Read visible Hootsuite social profile identities. */
export class ListSocialProfilesOperation implements Query<ListSocialProfilesInput, SocialProfilesPage> {
  /** Bind the provider client. */ constructor(private readonly client: HootsuiteClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListSocialProfilesDefinition; }
  /** Project one bounded provider response. */
  async invoke(call: Call, input: ListSocialProfilesInput): Promise<QueryAttempt<SocialProfilesPage>> {
    if (!input || typeof input !== 'object') return queryBranch('listSocialProfiles', 'defect', 'VALIDATION', 'social profile input is invalid', emptyProfiles());
    const exchange = await this.client.get(call, '/v1/socialProfiles');
    const decision = queryFailure('listSocialProfiles', exchange, emptyProfiles());
    if (decision) return decision;
    const document = parseJSON(exchange.body);
    if (!isObject(document) || !Array.isArray(document.data) || document.data.length > 1000) return queryBranch('listSocialProfiles', 'invalidResponse', 'PROTOCOL', 'Hootsuite returned invalid social profile JSON', emptyProfiles());
    const profiles: SocialProfile[] = [];
    for (const item of document.data) {
      if (!isObject(item) || typeof item.id !== 'string' || !item.id || typeof item.type !== 'string' || !item.type || typeof item.socialNetworkUsername !== 'string' || !item.socialNetworkUsername) return queryBranch('listSocialProfiles', 'invalidResponse', 'PROTOCOL', 'Hootsuite returned incomplete social profile metadata', emptyProfiles());
      profiles.push({ id: item.id, type: item.type, username: item.socialNetworkUsername });
    }
    return newQueryBranch('listed', { profiles });
  }
}

/** Read one page of scheduled outbound messages. */
export class ListScheduledPostsOperation implements Query<ListScheduledPostsInput, ScheduledPostsPage> {
  /** Bind the provider client. */ constructor(private readonly client: HootsuiteClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListScheduledPostsDefinition; }
  /** Read one UTC interval of at most four weeks. */
  async invoke(call: Call, input: ListScheduledPostsInput): Promise<QueryAttempt<ScheduledPostsPage>> {
    const start = utcTime(input?.startTime);
    const end = utcTime(input?.endTime);
    if (start === undefined || end === undefined || end <= start || end - start > 28 * 24 * 60 * 60 * 1000 || typeof input.socialProfileId !== 'string' && input.socialProfileId !== undefined || typeof input.cursor !== 'string' && input.cursor !== undefined || (input.socialProfileId?.length ?? 0) > 40 || (input.cursor?.length ?? 0) > 4096 || !safeQueryValue(input.socialProfileId ?? '') || !safeQueryValue(input.cursor ?? '')) return queryBranch('listScheduledPosts', 'defect', 'VALIDATION', 'scheduled post interval or filter is invalid', emptyScheduledPosts());
    const query = new URLSearchParams({ startTime: input.startTime, endTime: input.endTime, state: 'SCHEDULED', limit: '100' });
    if (input.socialProfileId) query.set('socialProfileIds', input.socialProfileId);
    if (input.cursor) query.set('cursor', input.cursor);
    const exchange = await this.client.get(call, `/v1/messages?${query}`);
    const decision = queryFailure('listScheduledPosts', exchange, emptyScheduledPosts());
    if (decision) return decision;
    const document = parseJSON(exchange.body);
    if (!isObject(document) || !Array.isArray(document.data) || document.data.length > 100) return queryBranch('listScheduledPosts', 'invalidResponse', 'PROTOCOL', 'Hootsuite returned invalid scheduled message JSON', emptyScheduledPosts());
    const posts: ScheduledPost[] = [];
    for (const item of document.data) {
      if (!isObject(item) || typeof item.id !== 'string' || !item.id || item.state !== 'SCHEDULED' || typeof item.scheduledSendTime !== 'string' || !item.scheduledSendTime || !isObject(item.socialProfile) || typeof item.socialProfile.id !== 'string' || !item.socialProfile.id) return queryBranch('listScheduledPosts', 'invalidResponse', 'PROTOCOL', 'Hootsuite returned incomplete scheduled message metadata', emptyScheduledPosts());
      posts.push({ id: item.id, state: item.state, socialProfileId: item.socialProfile.id, scheduledSendTime: item.scheduledSendTime });
    }
    const metadata = document.metadata ?? {};
    if (!isObject(metadata)) return queryBranch('listScheduledPosts', 'invalidResponse', 'PROTOCOL', 'Hootsuite returned an invalid page cursor', emptyScheduledPosts());
    const cursor = metadata.cursor ?? {};
    if (!isObject(cursor)) return queryBranch('listScheduledPosts', 'invalidResponse', 'PROTOCOL', 'Hootsuite returned an invalid page cursor', emptyScheduledPosts());
    const nextCursor = cursor.next ?? '';
    if (typeof nextCursor !== 'string' || nextCursor.length > 4096 || !safeQueryValue(nextCursor)) return queryBranch('listScheduledPosts', 'invalidResponse', 'PROTOCOL', 'Hootsuite returned an invalid page cursor', emptyScheduledPosts());
    return newQueryBranch('listed', { posts, nextCursor });
  }
}

/** Schedule one text post behind a durable single-dispatch fence. */
export class SchedulePostOperation implements Mutation<SchedulePostInput, ScheduledPostReceipt> {
  /** Bind the provider client. */ constructor(private readonly client: HootsuiteClient) {}
  /** Return the generated Mutation definition. */ definition(): MutationDefinition { return SchedulePostDefinition; }
  /** Record the Call ID; Hootsuite does not deduplicate this key. */ idempotencyKey(callID: CallID): string { return callID; }
  /** Send once, returning uncertainty when the provider may have accepted the post. */
  async invoke(call: Call, input: SchedulePostInput): Promise<MutationAttempt<ScheduledPostReceipt>> {
    const operation = 'schedulePost';
    const empty = emptyReceipt();
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, SchedulePostDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', empty, {}, failure(operation, 'LOCAL_DEFECT', 'Hootsuite dispatch fence is unavailable')); }
    if (fence.decide().kind !== 'sendAllowed') return newMutationUncertain(empty, failure(operation, 'TRANSPORT', 'Hootsuite schedule outcome cannot be confirmed; inspect Planner before resending'));
    if (!input || typeof input.text !== 'string' || !input.text.trim() || Buffer.from(input.text, 'utf8').toString('utf8') !== input.text || Buffer.byteLength(input.text, 'utf8') > 10_000 || typeof input.socialProfileId !== 'string' || !numericID.test(input.socialProfileId) || utcTime(input.scheduledSendTime) === undefined) return newMutationBranch('defect', empty, {}, failure(operation, 'VALIDATION', 'scheduled message text, profile ID, or UTC time is invalid'));
    const token = await this.client.resolveToken(call);
    if (typeof token !== 'string') {
      if (token.error === 'credential') return newMutationBranch('defect', empty, {}, failure(operation, 'AUTHENTICATION', 'connection credentials require reauthorization'));
      return newMutationRetry(failure(operation, 'TRANSPORT', 'connection refresh failed'));
    }
    const body = Buffer.from(JSON.stringify({ text: input.text, socialProfileIds: [input.socialProfileId], scheduledSendTime: input.scheduledSendTime }));
    let exchange: HootsuiteExchange;
    try { exchange = await fence.dispatch(signal => this.client.send('POST', '/v1/messages', token, signal, body)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(failure(operation, 'TRANSPORT', 'Hootsuite dispatch did not start'));
      return newMutationUncertain(empty, failure(operation, 'TRANSPORT', 'Hootsuite schedule outcome is unknown'));
    }
    if (exchange.error === 'request') {
      if (!exchange.requestSent) {
        try { await fence.recordCanResend(); return newMutationRetry(failure(operation, 'TRANSPORT', 'Hootsuite could not be reached before sending')); }
        catch { /* The claim marker still blocks a second send. */ }
      }
      return newMutationUncertain(empty, failure(operation, 'TRANSPORT', 'Hootsuite schedule outcome is unknown'));
    }
    if (exchange.status === 429 || exchange.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Hootsuite retry checkpoint is unavailable')); }
      const kind = exchange.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY';
      return newMutationRetry(failure(operation, kind, 'Hootsuite did not schedule the message'), exchange.retryAfterMs);
    }
    if (exchange.status === 408 || exchange.status >= 500) return newMutationUncertain(empty, failure(operation, 'AVAILABILITY', 'Hootsuite schedule outcome is unknown'));
    if (exchange.status >= 400 && exchange.status < 500) return newMutationBranch('providerRejected', empty, {}, failure(operation, 'PROVIDER_REJECTION', `Hootsuite rejected HTTP ${exchange.status}`));
    if (exchange.status !== 200) return newMutationUncertain(empty, failure(operation, 'PROTOCOL', 'Hootsuite returned an unexpected schedule response'));
    if (exchange.error) return newMutationUncertain(empty, failure(operation, 'TRANSPORT', 'Hootsuite accepted the request but its response could not be read'));
    const document = parseJSON(exchange.body);
    const data = isObject(document) ? document.data : undefined;
    if (!Array.isArray(data) || data.length !== 1 || !isObject(data[0]) || typeof data[0].id !== 'string' || !data[0].id || typeof data[0].state !== 'string' || !data[0].state) return newMutationUncertain(empty, failure(operation, 'PROTOCOL', 'Hootsuite accepted the request but returned no usable message ID'));
    const socialProfile = data[0].socialProfile ?? {};
    if (!isObject(socialProfile) || (socialProfile.id && socialProfile.id !== input.socialProfileId)) return newMutationUncertain(empty, failure(operation, 'PROTOCOL', 'Hootsuite returned an unexpected social profile'));
    try { await fence.recordProviderId(data[0].id); }
    catch { /* The claim marker still blocks a second send. */ }
    return newMutationBranch('scheduled', { id: data[0].id, state: data[0].state, socialProfileId: input.socialProfileId });
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'hootsuite', operation, kind, message }; }
function queryBranch<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function queryRetry<Output>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Output> { return newQueryRetry(failure(operation, kind, message), delay); }
function queryFailure<Output>(operation: string, exchange: HootsuiteExchange, empty: Output): QueryAttempt<Output> | undefined {
  if (exchange.error === 'credential') return queryBranch(operation, 'defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid', empty);
  if (exchange.error === 'request' || exchange.error === 'refresh') return queryRetry(operation, 'TRANSPORT', 'Hootsuite request failed');
  if (exchange.status === 429) return queryRetry(operation, 'RATE_LIMIT', 'Hootsuite rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return queryRetry(operation, 'AVAILABILITY', 'Hootsuite is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return queryBranch(operation, 'providerRejected', 'PROVIDER_REJECTION', `Hootsuite rejected HTTP ${exchange.status}`, empty);
  if (exchange.error === 'tooLarge') return queryBranch(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'Hootsuite response exceeded the configured limit', empty);
  return undefined;
}
function failedExchange(error: NonNullable<HootsuiteExchange['error']>): HootsuiteExchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error, requestSent: false }; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function safeQueryValue(value: string): boolean { return ![...value].some(character => { const code = character.charCodeAt(0); return code < 32 || code === 127; }); }
function utcTime(value: unknown): number | undefined { if (typeof value !== 'string' || !utcInstant.test(value)) return undefined; const parsed = Date.parse(value); return Number.isNaN(parsed) ? undefined : parsed; }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
