// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDocumentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, DocumentMetadata, DocumentsPage, ListDocumentsInput } from './generated/models.js';

const defaultBaseURL = 'https://api.lucid.co/v1';
const emptyPage = (): DocumentsPage => ({ documents: [], nextPageToken: '' });

/** Injectable provider sender for tests and local applications. */
export interface LucidClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  link: string;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Lucid REST API v1 client for document pages. */
export class LucidClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Lucid. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: LucidClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Lucid configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Lucid request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listDocuments Query. */ listDocuments(): ListDocumentsOperation { return new ListDocumentsOperation(this); }

  /** Resolve the API key and search one bounded document page. */
  async get(call: Call, pageToken: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/documents/search`);
    url.searchParams.set('pageSize', '200');
    if (pageToken) url.searchParams.set('pageToken', pageToken);
    const request: ProviderRequest = {
      url: url.toString(), method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, 'Lucid-Api-Version': '1' },
      body: new TextEncoder().encode('{"excludeTrashed":true}'),
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
      return { status: response.status, body: new Uint8Array(), retryAfterMs, link: header(response.headers, 'link'), error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs, link: header(response.headers, 'link') };
  }
}

/** Read one document page with IDs, names, and the next opaque cursor. */
export class ListDocumentsOperation implements Query<ListDocumentsInput, DocumentsPage> {
  /** Bind the provider client. */ constructor(private readonly client: LucidClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListDocumentsDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: ListDocumentsInput): Promise<QueryAttempt<DocumentsPage>> {
    const pageToken = input?.pageToken ?? '';
    if (!input || !isValidCursor(pageToken))
      return terminal('defect', 'VALIDATION', 'page token contains invalid characters or exceeds 2048 bytes');
    const exchange = await this.client.get(call, pageToken);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Lucid credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Lucid request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Lucid rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Lucid is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Lucid rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Lucid response exceeded the configured limit');
    const page = decodePage(exchange.body, exchange.link);
    if (!page) return terminal('invalidResponse', 'PROTOCOL', 'Lucid returned invalid document JSON or pagination');
    return newQueryBranch('listed', page);
  }
}

function isValidCursor(value: unknown): value is string {
  if (typeof value !== 'string' || Buffer.byteLength(value, 'utf8') > 2048) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, link: '', error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'lucid', operation: 'listDocuments', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<DocumentsPage> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<DocumentsPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function nextPageToken(link: string): string | undefined {
  if (!link) return '';
  for (const segment of link.split(',')) {
    const parts = segment.split(';').map(part => part.trim());
    if (parts.length < 2) return undefined;
    if (!parts.slice(1).some(part => part === 'rel="next"' || part === 'rel=next')) continue;
    const rawURL = parts[0] ?? '';
    if (!rawURL.startsWith('<') || !rawURL.endsWith('>')) return undefined;
    let nextURL: URL;
    try { nextURL = new URL(rawURL.slice(1, -1)); } catch { return undefined; }
    if (nextURL.protocol !== 'https:' || nextURL.host !== 'api.lucid.co' ||
      !['/v1/documents/search', '/documents/search'].includes(nextURL.pathname)) return undefined;
    const tokens = nextURL.searchParams.getAll('pageToken');
    if (tokens.length !== 1 || !tokens[0] || !isValidCursor(tokens[0])) return undefined;
    return tokens[0];
  }
  return '';
}
function decodePage(body: Uint8Array, link: string): DocumentsPage | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!Array.isArray(raw)) return undefined;
  const documents: DocumentMetadata[] = [];
  for (const document of raw) {
    if (!isObject(document) || typeof document.documentId !== 'string' || !document.documentId ||
      typeof document.title !== 'string' || !document.title.trim()) return undefined;
    documents.push({ id: document.documentId, title: document.title });
  }
  const token = nextPageToken(link);
  if (token === undefined) return undefined;
  return { documents, nextPageToken: token };
}
