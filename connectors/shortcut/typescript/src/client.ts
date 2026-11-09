// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetStoryDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetStoryInput, StoryMetadata } from './generated/models.js';

const defaultBaseURL = 'https://api.app.shortcut.com/api/v3';
const emptyStory = (): StoryMetadata => ({ storyId: '', name: '', storyType: '', workflowStateId: '', archived: false });
const maxSafeStoryID = 9007199254740991;

/** Injectable provider sender for tests and local applications. */
export interface ShortcutClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Shortcut REST API v3 client for Story metadata. */
export class ShortcutClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Shortcut. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ShortcutClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Shortcut configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Shortcut request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getStory Query. */ getStory(): GetStoryOperation { return new GetStoryOperation(this); }

  /** Resolve the token and send one bounded Story lookup. */
  async get(call: Call, storyID: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/stories/${storyID}`, method: 'GET',
      headers: { Accept: 'application/json', 'Shortcut-Token': token },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one Story's ID, title, type, workflow state, and archived flag. */
export class GetStoryOperation implements Query<GetStoryInput, StoryMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: ShortcutClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetStoryDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetStoryInput): Promise<QueryAttempt<StoryMetadata>> {
    if (!isValidStoryID(input?.storyId))
      return terminal('defect', 'VALIDATION', 'story ID must be a positive decimal ID within the safe integer range');
    const exchange = await this.client.get(call, input.storyId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Shortcut credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Shortcut request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Shortcut rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Shortcut is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Shortcut did not find the story');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Shortcut rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Shortcut response exceeded the configured limit');
    const story = decodeStory(exchange.body, input.storyId);
    if (!story) return terminal('invalidResponse', 'PROTOCOL', 'Shortcut returned invalid story JSON');
    return newQueryBranch('found', story);
  }
}

function isValidStoryID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]*$/.test(value) && value.length <= 16 && Number(value) <= maxSafeStoryID;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'shortcut', operation: 'getStory', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<StoryMetadata> { return newQueryBranch(branch, emptyStory(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<StoryMetadata> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeStory(body: Uint8Array, storyID: string): StoryMetadata | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || typeof raw.id !== 'number' || !Number.isSafeInteger(raw.id) || raw.id <= 0 ||
      String(raw.id) !== storyID || typeof raw.name !== 'string' || !raw.name ||
      typeof raw.story_type !== 'string' || !raw.story_type || typeof raw.workflow_state_id !== 'number' ||
      !Number.isSafeInteger(raw.workflow_state_id) || raw.workflow_state_id <= 0 || typeof raw.archived !== 'boolean') return undefined;
  return { storyId: storyID, name: raw.name, storyType: raw.story_type, workflowStateId: String(raw.workflow_state_id), archived: raw.archived };
}
