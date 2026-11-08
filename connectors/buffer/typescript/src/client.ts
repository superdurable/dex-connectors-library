// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetChannelDefinition } from './generated/definitions.js';
import type { Config, Credentials, ChannelProfile, GetChannelInput } from './generated/models.js';

const emptyChannel = (): ChannelProfile => ({ id: '', name: '', service: '' });
const getChannelGraphQL = 'query GetChannel($id: ChannelId!) { channel(input: {id: $id}) { id name service } }';
/** Injectable provider sender for tests and local applications. */
export interface BufferClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Buffer GraphQL client for channel lookups. */
export class BufferClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Buffer. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: BufferClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Buffer configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://api.buffer.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Buffer request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getChannel Query. */ getChannel(): GetChannelOperation { return new GetChannelOperation(this); }

  /** Resolve credentials and send one bounded channel lookup. */
  async get(call: Call, input: GetChannelInput): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/`);
    const request: ProviderRequest = {
      url: url.toString(), method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${apiKey}` },
      body: new TextEncoder().encode(JSON.stringify({ query: getChannelGraphQL, variables: { id: input.channelId } })),
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

/** Read one channel by its Buffer GraphQL ID. */
export class GetChannelOperation implements Query<GetChannelInput, ChannelProfile> {
  /** Bind the provider client. */ constructor(private readonly client: BufferClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetChannelDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: GetChannelInput): Promise<QueryAttempt<ChannelProfile>> {
    if (!isValidGraphQLID(input?.channelId))
      return terminal('defect', 'VALIDATION', 'channelId must contain 1–128 printable characters');
    const exchange = await this.client.get(call, input);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Buffer credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Buffer request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Buffer rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Buffer is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Buffer rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Buffer response exceeded the configured limit');
    const channel = decodeChannel(exchange.body, input.channelId);
    if (channel === 'rejected') return terminal('providerRejected', 'PROVIDER_REJECTION', 'Buffer rejected the GraphQL query');
    if (channel === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Buffer returned invalid channel JSON');
    if (channel === null) return terminal('notFound', 'NOT_FOUND', 'Buffer did not expose the channel');
    return newQueryBranch('found', channel);
  }
}

function isValidGraphQLID(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.trim() === value &&
    new TextEncoder().encode(value).length <= 128 && ![...value].some(character => character.codePointAt(0)! < 0x20 || character.codePointAt(0)! === 0x7f) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'buffer', operation: 'getChannel', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ChannelProfile> { return newQueryBranch(branch, emptyChannel(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ChannelProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeChannel(body: Uint8Array, channelID: string): ChannelProfile | null | 'rejected' | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw)) return undefined;
  if (raw.errors !== undefined && raw.errors !== null && !Array.isArray(raw.errors)) return undefined;
  if (Array.isArray(raw.errors) && raw.errors.length > 0) return 'rejected';
  if (!isObject(raw.data)) return undefined;
  const channel = raw.data.channel;
  if (channel === null) return null;
  if (!isObject(channel)) return undefined;
  if (channel.id !== channelID || typeof channel.name !== 'string' || !channel.name || typeof channel.service !== 'string' || !channel.service) return undefined;
  return { id: channel.id, name: channel.name, service: channel.service };
}
