// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SetUserPropertiesDefinition } from './generated/definitions.js';
import type { Config, SetUserPropertiesInput, UserPropertiesResult } from './generated/models.js';

const defaultBaseURL = 'https://heapanalytics.com';
const emptyResult: UserPropertiesResult = { accepted: false };

/** Injectable provider sender for tests and local applications. */
export interface HeapClientOptions {
  /** Provider sender; omitted uses the SDK transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Heap server-side API client for absolute user-property updates. */
export class HeapClient {
  private readonly baseURL: string;
  private readonly appID: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Heap. */
  constructor(config: Config, options: HeapClientOptions = {}) {
    if (!config || !isValidAppID(config.appId)) throw new TypeError('Heap appId must contain decimal digits');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.appID = config.appId;
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Heap request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the property update Mutation. */
  setUserProperties(): SetUserPropertiesOperation { return new SetUserPropertiesOperation(this); }

  /** Send one bounded provider request. */
  async post(call: Call, body: Uint8Array): Promise<{ response: ProviderResponse; tooLarge: boolean } | undefined> {
    const request: ProviderRequest = {
      url: `${this.baseURL}/api/add_user_properties`, method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body,
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    try { return { response: await this.send(request), tooLarge: false }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return undefined;
    }
  }

  /** Return the environment ID without credentials. */
  get appId(): string { return this.appID; }
  /** Return the configured response bound. */
  get responseLimit(): number { return this.maxResponseBytes; }
}

/** Overwrite selected string properties for one stable Heap identity. */
export class SetUserPropertiesOperation implements Mutation<SetUserPropertiesInput, UserPropertiesResult> {
  /** Bind one Heap client. */ constructor(private readonly client: HeapClient) {}
  /** Return the generated Mutation contract. */ definition(): MutationDefinition { return SetUserPropertiesDefinition; }
  /** Derive Go's stable SHA-256 call key; the provider write is an absolute replacement. */
  idempotencyKey(callId: string): string { return createHash('sha256').update(callId).digest('hex'); }
  /** Validate and send one property update, then classify the provider outcome. */
  async invoke(call: Call, input: SetUserPropertiesInput): Promise<MutationAttempt<UserPropertiesResult>> {
    if (!isValidInput(input)) return terminal('defect', 'VALIDATION', 'identity or properties are invalid');
    let body: Uint8Array;
    try { body = Buffer.from(JSON.stringify({ app_id: this.client.appId, identity: input.identity, properties: input.properties })); }
    catch { return terminal('defect', 'VALIDATION', 'property payload exceeds the limit'); }
    if (body.byteLength > 65_536) return terminal('defect', 'VALIDATION', 'property payload exceeds the limit');
    const exchange = await this.client.post(call, body);
    if (!exchange) return retry('TRANSPORT', 'Heap response is unavailable');
    const { response, tooLarge } = exchange;
    const retryAfter = parseRetryAfter(header(response.headers, 'retry-after'), new Date());
    if (response.status === 429) return retry('RATE_LIMIT', 'Heap rate limited the request', retryAfter);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Heap is temporarily unavailable', retryAfter);
    if (response.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Heap rejected HTTP ${response.status}`);
    if (tooLarge || response.body.byteLength > this.client.responseLimit)
      return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Heap confirmation exceeded the limit');
    return newMutationBranch('accepted', { accepted: true });
  }
}

function isValidAppID(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= 32 && /^[0-9]+$/.test(value);
}
function utf8Length(value: unknown): number | undefined {
  if (typeof value !== 'string') return undefined;
  for (let index = 0; index < value.length; index++) {
    const point = value.codePointAt(index);
    if (point === undefined || (point >= 0xd800 && point <= 0xdfff)) return undefined;
    if (point > 0xffff) index++;
  }
  return Buffer.byteLength(value, 'utf8');
}
function isValidInput(input: SetUserPropertiesInput): boolean {
  if (!input) return false;
  const identityLength = utf8Length(input.identity);
  if (identityLength === undefined || identityLength < 1 || identityLength > 255 || !input.identity.trim()) return false;
  const properties = input.properties;
  if (!properties || typeof properties !== 'object' || Array.isArray(properties)) return false;
  const entries = Object.entries(properties);
  if (entries.length < 1 || entries.length > 50) return false;
  return entries.every(([key, value]) => {
    const keyLength = utf8Length(key);
    const valueLength = utf8Length(value);
    return keyLength !== undefined && valueLength !== undefined && !!key.trim() && key.toLowerCase() !== 'id' && keyLength < 1024 && valueLength < 1024;
  });
}
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'heap', operation: 'setUserProperties', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): MutationAttempt<UserPropertiesResult> { return newMutationBranch(branch, emptyResult, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): MutationAttempt<UserPropertiesResult> { return newMutationRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
