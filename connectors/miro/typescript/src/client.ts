// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetBoardDefinition } from './generated/definitions.js';
import type { Config, Credentials, BoardMetadata, GetBoardInput } from './generated/models.js';

const defaultBaseURL = 'https://api.miro.com/v2';
const emptyBoard = (): BoardMetadata => ({ id: '', name: '' });

/** Injectable provider sender for tests and local applications. */
export interface MiroClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Miro REST API v2 client for board metadata. */
export class MiroClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Miro. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MiroClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Miro configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Miro request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getBoard Query. */ getBoard(): GetBoardOperation { return new GetBoardOperation(this); }

  /** Resolve the token and send one bounded board lookup. */
  async get(call: Call, boardID: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/boards/${encodeURIComponent(boardID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
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

/** Read one board's ID and name. */
export class GetBoardOperation implements Query<GetBoardInput, BoardMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: MiroClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetBoardDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetBoardInput): Promise<QueryAttempt<BoardMetadata>> {
    if (!isValidBoardID(input?.boardId))
      return terminal('defect', 'VALIDATION', 'board ID must be 1–256 printable characters');
    const exchange = await this.client.get(call, input.boardId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Miro credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Miro request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Miro rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Miro is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Miro did not expose the board');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Miro rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Miro response exceeded the configured limit');
    const board = decodeBoard(exchange.body, input.boardId);
    if (!board) return terminal('invalidResponse', 'PROTOCOL', 'Miro returned invalid board JSON');
    return newQueryBranch('found', board);
  }
}

function isValidBoardID(value: unknown): value is string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value, 'utf8') > 256) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159) || '/?#'.includes(character)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'miro', operation: 'getBoard', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<BoardMetadata> { return newQueryBranch(branch, emptyBoard(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<BoardMetadata> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeBoard(body: Uint8Array, boardID: string): BoardMetadata | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.id !== boardID || typeof raw.name !== 'string' || !raw.name.trim() || raw.type !== 'board') return undefined;
  return { id: boardID, name: raw.name };
}
