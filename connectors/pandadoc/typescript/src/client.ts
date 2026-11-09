// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetDocumentDefinition } from './generated/definitions.js';
import type { Config, Credentials, DocumentStatus, GetDocumentInput } from './generated/models.js';

const defaultBaseURL = 'https://api.pandadoc.com/public/v1';
const emptyDocument = (): DocumentStatus => ({ id: '', name: '', status: '' });

/** Injectable provider sender for tests and local applications. */
export interface PandaDocClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated PandaDoc Public API v1 client for document metadata. */
export class PandaDocClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting PandaDoc. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PandaDocClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('PandaDoc configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('PandaDoc request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getDocument Query. */ getDocument(): GetDocumentOperation { return new GetDocumentOperation(this); }

  /** Resolve the API key and send one bounded document lookup. */
  async get(call: Call, documentID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/documents/${encodeURIComponent(documentID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `API-Key ${apiKey}` },
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

/** Read one document's ID, name, and status. */
export class GetDocumentOperation implements Query<GetDocumentInput, DocumentStatus> {
  /** Bind the provider client. */ constructor(private readonly client: PandaDocClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetDocumentDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetDocumentInput): Promise<QueryAttempt<DocumentStatus>> {
    if (!isValidDocumentID(input?.documentId))
      return terminal('defect', 'VALIDATION', 'document ID must be 1–256 printable characters');
    const exchange = await this.client.get(call, input.documentId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'PandaDoc credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'PandaDoc request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'PandaDoc rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'PandaDoc is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'PandaDoc did not expose the document');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `PandaDoc rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'PandaDoc response exceeded the configured limit');
    const document = decodeDocument(exchange.body, input.documentId);
    if (!document) return terminal('invalidResponse', 'PROTOCOL', 'PandaDoc returned invalid document JSON');
    return newQueryBranch('found', document);
  }
}

function isValidDocumentID(value: unknown): value is string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value, 'utf8') > 256) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159) || '/?#'.includes(character)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'pandadoc', operation: 'getDocument', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<DocumentStatus> { return newQueryBranch(branch, emptyDocument(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<DocumentStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeDocument(body: Uint8Array, documentID: string): DocumentStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.id !== documentID || typeof raw.name !== 'string' || !raw.name.trim() || typeof raw.status !== 'string' || !raw.status.startsWith('document.') || raw.status.length <= 'document.'.length) return undefined;
  return { id: documentID, name: raw.name, status: raw.status };
}
