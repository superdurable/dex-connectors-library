// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import {
  newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry,
  type Call, type CredentialProvider, type Failure, type Mutation, type MutationAttempt,
  type Query, type QueryAttempt,
} from '@superdurable/connectors-sdk';
import {
  BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL,
  type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { CaptureEventDefinition, ListInsightsDefinition } from './generated/definitions.js';
import type {
  CaptureEventInput, CapturedEvent, Config, Credentials, InsightMetadata, InsightsPage, ListInsightsInput,
} from './generated/models.js';

const emptyEvent: CapturedEvent = { eventId: '', accepted: false };
const emptyPage: InsightsPage = { insights: [], hasMore: false, offset: 0, limit: 0 };

/** Injectable provider transport and clock for deterministic replay. */
export interface PostHogClientOptions {
  /** Provider sender; omitted uses the SDK's redirect-safe transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for Retry-After dates. */
  now?: () => Date;
}

type Exchange = { response: ProviderResponse; tooLarge: boolean } | { transportFailure: true };

/** PostHog public capture and private insight API client. */
export class PostHogClient {
  /** Public event capture API base URL. */ readonly captureUrl: string;
  /** Private insight API base URL. */ readonly appUrl: string;
  /** Complete response bound in bytes. */ readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting the provider. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: PostHogClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('PostHog configuration and credential source are required');
    this.captureUrl = validateBaseURL(config.captureUrl || 'https://us.i.posthog.com');
    this.appUrl = validateBaseURL(config.appUrl || 'https://us.posthog.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('PostHog timeout and response limit must be positive integers');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind a deduplicated event Mutation. */
  captureEvent(): CaptureEventOperation { return new CaptureEventOperation(this); }
  /** Bind a bounded saved insight Query. */
  listInsights(): ListInsightsOperation { return new ListInsightsOperation(this); }

  /** Resolve a project token for an individual provider attempt. */
  async projectToken(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const token = credentials.projectToken.reveal();
      return token && isHeaderSafeCredential(token) ? token : undefined;
    } catch { return undefined; }
  }

  /** Resolve a scoped personal API key for an individual provider attempt. */
  async personalApiKey(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const key = credentials.personalApiKey?.reveal();
      return key && isHeaderSafeCredential(key) ? key : undefined;
    } catch { return undefined; }
  }

  /** Send one bounded provider request, preserving response status on body overflow. */
  async exchange(call: Call, method: 'GET' | 'POST', url: string,
    headers: Record<string, string>, body?: Uint8Array): Promise<Exchange> {
    const request: ProviderRequest = {
      url, method, headers, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: url.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
      ...(body ? { body } : {}),
    };
    try { return { response: await this.send(request), tooLarge: false }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { transportFailure: true };
    }
  }

  /** Parse the provider's Retry-After header in milliseconds. */
  retryAfter(response: ProviderResponse): number {
    const entry = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', this.now());
  }
}

/** Send a stable PostHog capture event. */
export class CaptureEventOperation implements Mutation<CaptureEventInput, CapturedEvent> {
  /** Bind the provider client. */ constructor(private readonly client: PostHogClient) {}
  /** Return the generated Mutation contract. */ definition() { return CaptureEventDefinition; }
  /** Derive Go's UUID-shaped SHA-256 identity from the Dex call ID. */
  idempotencyKey(callId: string): string {
    const hex = createHash('sha256').update(callId, 'utf8').digest('hex').slice(0, 32);
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
  }
  /** Validate, submit, and classify one event with a stable timestamp. */
  async invoke(call: Call, input: CaptureEventInput): Promise<MutationAttempt<CapturedEvent>> {
    if (!validEvent(input) || call.idempotencyKey?.length !== 36)
      return eventFailure('defect', 'VALIDATION', 'event, distinct ID, or stable call identity is invalid');
    const timestamp = canonicalTimestamp(input.timestamp);
    if (!timestamp) return eventFailure('defect', 'VALIDATION', 'timestamp must be an RFC3339 instant');
    const token = await this.client.projectToken(call);
    if (!token) return eventFailure('defect', 'AUTHENTICATION', 'PostHog project token is unavailable or invalid');
    let body: Uint8Array;
    try {
      body = Buffer.from(JSON.stringify({ token, event: input.event, distinct_id: input.distinctId,
        timestamp, uuid: call.idempotencyKey, ...(input.properties && Object.keys(input.properties).length ? { properties: input.properties } : {}) }));
    } catch { return eventFailure('defect', 'VALIDATION', 'event payload exceeds the limit'); }
    if (body.byteLength > 1 << 20) return eventFailure('defect', 'VALIDATION', 'event payload exceeds the limit');
    const exchange = await this.client.exchange(call, 'POST', `${this.client.captureUrl}/i/v0/e/`,
      { Accept: 'application/json', 'Content-Type': 'application/json' }, body);
    if ('transportFailure' in exchange) return eventRetry('TRANSPORT', 'PostHog capture response is unavailable');
    const { response, tooLarge } = exchange;
    const retryAfter = this.client.retryAfter(response);
    if (response.status === 429) return eventRetry('RATE_LIMIT', 'PostHog rate limited capture', retryAfter);
    if (response.status === 408 || response.status >= 500) return eventRetry('AVAILABILITY', 'PostHog capture is temporarily unavailable', retryAfter);
    if (response.status !== 200) return eventFailure('providerRejected', rejectionKind(response.status), `PostHog rejected capture with HTTP ${response.status}`);
    if (tooLarge || response.body.byteLength > this.client.maxResponseBytes)
      return eventRetry('RESPONSE_TOO_LARGE', 'PostHog capture confirmation exceeded the limit');
    let answer: unknown;
    try { answer = JSON.parse(Buffer.from(response.body).toString('utf8')); }
    catch { return eventRetry('PROTOCOL', 'PostHog capture confirmation is unusable'); }
    if (!isRecord(answer) || answer.status !== 'Ok') return eventRetry('PROTOCOL', 'PostHog capture confirmation is unusable');
    if (answer.quota_limited !== undefined && answer.quota_limited !== null &&
      JSON.stringify(answer.quota_limited) !== '[]' && JSON.stringify(answer.quota_limited) !== '{}')
      return eventFailure('quotaLimited', 'PROVIDER_REJECTION', 'PostHog reported an event quota limit');
    return newMutationBranch('accepted', { eventId: call.idempotencyKey, accepted: true });
  }
}

/** List saved insight IDs and names for one PostHog project. */
export class ListInsightsOperation implements Query<ListInsightsInput, InsightsPage> {
  /** Bind the provider client. */ constructor(private readonly client: PostHogClient) {}
  /** Return the generated Query contract. */ definition() { return ListInsightsDefinition; }
  /** Validate, request, and classify one bounded insight page. */
  async invoke(call: Call, input: ListInsightsInput): Promise<QueryAttempt<InsightsPage>> {
    const offset = input?.offset ?? 0;
    if (!input || !Number.isSafeInteger(input.projectId) || input.projectId <= 0 ||
      !Number.isSafeInteger(offset) || offset < 0 ||
      !Number.isSafeInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 100)
      return pageFailure('defect', 'VALIDATION', 'project ID, offset, or limit is invalid');
    const limit = input.limit || 20;
    const key = await this.client.personalApiKey(call);
    if (!key) return pageFailure('defect', 'AUTHENTICATION', 'PostHog personal API key is unavailable or invalid');
    const url = `${this.client.appUrl}/api/projects/${input.projectId}/insights/?limit=${limit}&offset=${offset}`;
    const exchange = await this.client.exchange(call, 'GET', url,
      { Accept: 'application/json', Authorization: `Bearer ${key}` });
    if ('transportFailure' in exchange) return pageRetry('TRANSPORT', 'PostHog insight response is unavailable');
    const { response, tooLarge } = exchange;
    const retryAfter = this.client.retryAfter(response);
    if (response.status === 429) return pageRetry('RATE_LIMIT', 'PostHog rate limited insight listing', retryAfter);
    if (response.status === 408 || response.status >= 500)
      return pageRetry('AVAILABILITY', 'PostHog insight listing is temporarily unavailable', retryAfter);
    if (response.status !== 200) return pageFailure('providerRejected', rejectionKind(response.status),
      `PostHog rejected insight listing with HTTP ${response.status}`);
    if (tooLarge || response.body.byteLength > this.client.maxResponseBytes)
      return pageFailure('invalidResponse', 'RESPONSE_TOO_LARGE', 'PostHog insight page exceeded the limit');
    let answer: unknown;
    try { answer = JSON.parse(Buffer.from(response.body).toString('utf8')); }
    catch { return pageFailure('invalidResponse', 'PROTOCOL', 'PostHog returned an invalid insight page'); }
    const page = decodePage(answer, offset, limit);
    if (!page) return pageFailure('invalidResponse', 'PROTOCOL', 'PostHog returned an invalid insight page');
    return newQueryBranch('listed', page);
  }
}

function validEvent(input: CaptureEventInput): boolean {
  if (!input || typeof input.event !== 'string' || !input.event.trim() ||
    typeof input.distinctId !== 'string' || !input.distinctId.trim() ||
    Buffer.byteLength(input.event) > 256 || Buffer.byteLength(input.distinctId) > 512) return false;
  return Object.entries(input.properties ?? {}).every(([name, value]) =>
    !!name.trim() && !name.startsWith('$') && !['token', 'api_key', 'password', 'secret'].includes(name.toLowerCase()) &&
    Buffer.byteLength(name) <= 128 && typeof value === 'string' && Buffer.byteLength(value) <= 4096);
}

function canonicalTimestamp(value: string): string | undefined {
  if (typeof value !== 'string' || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(value)) return undefined;
  const milliseconds = Date.parse(value);
  if (!Number.isFinite(milliseconds) || milliseconds === 0) return undefined;
  return new Date(milliseconds).toISOString().replace(/\.000Z$/, 'Z');
}

function decodePage(value: unknown, offset: number, limit: number): InsightsPage | undefined {
  if (!isRecord(value) || !Array.isArray(value.results) || value.results.length > limit ||
    (value.next !== undefined && value.next !== null && typeof value.next !== 'string')) return undefined;
  const insights: InsightMetadata[] = [];
  for (const raw of value.results) {
    if (!isRecord(raw) || !Number.isSafeInteger(raw.id) || (raw.id as number) <= 0 ||
      (raw.name !== undefined && raw.name !== null && typeof raw.name !== 'string')) return undefined;
    insights.push({ id: raw.id as number, name: typeof raw.name === 'string' ? raw.name : '' });
  }
  return { insights, hasMore: !!value.next, offset, limit };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function rejectionKind(status: number): Failure['kind'] {
  return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'posthog', operation, message };
}
function eventFailure(branch: string, kind: Failure['kind'], message: string): MutationAttempt<CapturedEvent> {
  return newMutationBranch(branch, emptyEvent, {}, failure('captureEvent', kind, message));
}
function eventRetry(kind: Failure['kind'], message: string, after = 0): MutationAttempt<CapturedEvent> {
  return newMutationRetry(failure('captureEvent', kind, message), after);
}
function pageFailure(branch: string, kind: Failure['kind'], message: string): QueryAttempt<InsightsPage> {
  return newQueryBranch(branch, emptyPage, {}, failure('listInsights', kind, message));
}
function pageRetry(kind: Failure['kind'], message: string, after = 0): QueryAttempt<InsightsPage> {
  return newQueryRetry(failure('listInsights', kind, message), after);
}
