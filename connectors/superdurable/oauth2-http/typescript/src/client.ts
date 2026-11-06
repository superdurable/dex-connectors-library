// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials, SendRequestInput } from './generated/models.js';
import { SendRequestOperation } from './operations.js';

/** One HTTP answer, including a bounded partial body. */
export interface APIResponse {
  /** HTTP status. */ status: number;
  /** Provider response headers. */ headers: ProviderResponse['headers'];
  /** Body bytes retained within the connection limit. */ body: Uint8Array;
  /** Whether the original body exceeded the limit. */ truncated: boolean;
  /** Whether reading the response body failed after HTTP status was received. */ readError?: boolean;
}
/** Injectable dependencies for provider tests. */
export interface OAuth2HTTPClientOptions {
  /** Replace provider dispatch in tests. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Set a deterministic receipt clock. */ now?: () => Date;
}

/** One OAuth-protected API connection. */
export class OAuth2HTTPClient {
  /** Validated API base URL without trailing slash. */
  readonly apiBaseUrl: string;
  /** Largest returned or sent body in bytes. */
  readonly maxBodyBytes: number;
  /** Maximum duration of one provider request. */
  readonly requestTimeoutMs: number;
  /** Receipt and retry clock. */
  readonly now: () => Date;
  /** Provider HTTP sender. */
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Generated manifest backed credential refresh driver. */
  readonly refreshDriver: ManifestCredentialRefreshDriver<Credentials>;

  /** Validate connection settings and bind renewable credentials. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: OAuth2HTTPClientOptions = {}) {
    if (!credentials) throw new TypeError('OAuth 2.0 HTTP credential provider is required');
    parseHTTPURL(config.authorizationEndpoint);
    parseHTTPURL(config.tokenEndpoint);
    if (!Array.isArray(config.scopes) || !config.scopes.length) throw new TypeError('OAuth 2.0 HTTP scopes are required');
    const apiBaseUrl = parseHTTPURL(config.apiBaseUrl);
    if (apiBaseUrl.search || config.apiBaseUrl.includes('?')) throw new TypeError('OAuth 2.0 HTTP apiBaseUrl cannot contain a query');
    this.apiBaseUrl = config.apiBaseUrl.replace(/\|/g, '%7C').replace(/\/+$/, '');
    this.maxBodyBytes = config.maxBodyBytes ?? 1_048_576;
    this.requestTimeoutMs = config.requestTimeoutMs ?? 12_000;
    if (!Number.isSafeInteger(this.maxBodyBytes) || this.maxBodyBytes < 1 || !Number.isSafeInteger(this.requestTimeoutMs) || this.requestTimeoutMs < 1_000) {
      throw new TypeError('OAuth 2.0 HTTP maxBodyBytes must be positive and requestTimeout at least one second');
    }
    this.now = options.now ?? (() => new Date());
    this.send = options.send ?? sendProviderRequest;
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'OAuth 2.0 HTTP', auth: Authorization, credentialFields: CredentialFields, settings: {
      authorizationEndpoint: config.authorizationEndpoint, tokenEndpoint: config.tokenEndpoint, scopes: config.scopes.join(' '),
      tokenEndpointAuthMethod: config.tokenEndpointAuthMethod || 'client_secret_post', apiBaseUrl: config.apiBaseUrl,
    } });
  }

  /** Bind the sendRequest Mutation. */
  sendRequest(): SendRequestOperation { return new SendRequestOperation(this); }

  /** Resolve current OAuth material through manifest generated renewal. */
  resolveCredentials(call: Call, signal: AbortSignal = call.context.cancellationSignal): Promise<Credentials> {
    return resolveCredential(this.credentials, call, this.refreshDriver, signal);
  }

  /** Renew once after HTTP 401 through the generated credential source. */
  resolveAfterRejection(call: Call, signal: AbortSignal = call.context.cancellationSignal): Promise<Credentials> {
    return resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, signal);
  }

  /** Send one bearer request without following redirects. */
  async exchange(call: Call, plan: RequestPlan, token: string, signal: AbortSignal = call.context.cancellationSignal, deadline = Date.now() + 25_000): Promise<APIResponse> {
    const headers: Record<string, string> = { ...plan.headers, Authorization: `Bearer ${token}` };
    if (!Object.keys(headers).some(key => key.toLowerCase() === 'user-agent')) headers['User-Agent'] = 'superdurable-dex-oauth2-http-connector/0.1';
    if (plan.idempotencyKeyHeader) headers[plan.idempotencyKeyHeader] = call.idempotencyKey ?? call.id;
    const chunks: Uint8Array[] = [];
    let responseStatus: number | undefined;
    let responseHeaders: ProviderResponse['headers'] | undefined;
    const request: ProviderRequest = {
      url: plan.url, method: plan.method, headers, ...(plan.body ? { body: Buffer.from(plan.body) } : {}),
      maxResponseBytes: this.maxBodyBytes, addressPolicy: 'privateNetworkHTTP', timeoutMs: Math.max(1, Math.min(this.requestTimeoutMs, deadline - Date.now())),
      signal,
      onResponseChunk: (chunk, status, headers) => {
        responseStatus = status;
        responseHeaders = headers;
        chunks.push(chunk);
      },
    };
    try {
      const answer = await this.send(request);
      return { ...answer, truncated: false };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
        return { status: error.status, headers: error.headers, body: Buffer.concat(chunks).subarray(0, this.maxBodyBytes), truncated: true };
      }
      if (responseStatus !== undefined && responseHeaders !== undefined) {
        return { status: responseStatus, headers: responseHeaders, body: Buffer.concat(chunks).subarray(0, this.maxBodyBytes), truncated: false, readError: true };
      }
      throw error;
    }
  }
}

/** Validated request fields, excluding the bearer credential. */
export interface RequestPlan {
  /** HTTP verb. */ method: string;
  /** Same-origin target URL. */ url: string;
  /** Validated caller headers. */ headers: Record<string, string>;
  /** Text request body. */ body: string;
  /** Optional deduplication header name. */ idempotencyKeyHeader: string;
  /** Whether replay is safe after dispatch. */ isSafe: boolean;
}

/** Validate the request and keep the token on the configured origin. */
export function planRequest(client: OAuth2HTTPClient, input: SendRequestInput): RequestPlan {
  const method = (input.method ?? '').trim().toUpperCase() || 'GET';
  if (!['GET', 'HEAD', 'OPTIONS', 'POST', 'PUT', 'PATCH', 'DELETE'].includes(method)) throw new TypeError('sendRequest method must be GET, HEAD, OPTIONS, POST, PUT, PATCH, or DELETE');
  if (Boolean(input.path) === Boolean(input.url)) throw new TypeError('sendRequest needs exactly one of path and url');
  let url: URL;
  let requestURL: string;
  if (input.path) {
    const path = input.path;
    if (!/^\/[\x21-\x7e]*$/.test(path) || path.startsWith('//')) throw new TypeError('sendRequest path must start with a single slash and be printable ASCII without spaces');
    if (/[?#\\]/.test(path)) throw new TypeError('sendRequest path cannot contain ?, #, or a backslash; use query for parameters');
    if (/%(?![\da-fA-F]{2})/.test(path)) throw new TypeError('sendRequest path has invalid percent-encoding');
    const decoded = path.replace(/%2e/gi, '.').replace(/%2f/gi, '/');
    if (decoded.split('/').includes('..')) throw new TypeError('sendRequest path cannot contain a .. segment');
    requestURL = client.apiBaseUrl + path.replace(/\|/g, '%7C');
    url = parseHTTPURL(requestURL);
  } else {
    requestURL = (input.url ?? '').replace(/\|/g, '%7C');
    url = parseHTTPURL(requestURL);
  }
  if (url.origin !== new URL(client.apiBaseUrl).origin) throw new TypeError("sendRequest url must have the scheme, host, and port of the connection's apiBaseUrl");
  if (input.query && Object.keys(input.query).length) {
    const query = parseGoQuery(url.search);
    for (const key of Object.keys(input.query).sort()) {
      if (!key) throw new TypeError('sendRequest query parameter names cannot be empty');
      for (const value of input.query[key] ?? []) query.append(key, value);
    }
    requestURL = requestURL.split('?')[0] + '?' + encodeGoQuery(query);
  }
  const headers: Record<string, string> = {};
  for (const [name, value] of Object.entries(input.headers ?? {})) {
    validateHeaderName(name);
    if (Object.keys(headers).some(existing => existing.toLowerCase() === name.toLowerCase())) throw new TypeError('sendRequest header names must be unique ignoring case');
    // eslint-disable-next-line no-control-regex -- HTTP header values reject control bytes.
    if (Buffer.byteLength(value) > 8192 || /[\x00-\x08\x0a-\x1f\x7f]/.test(value)) throw new TypeError('sendRequest header values cannot contain control characters');
    headers[name] = value;
  }
  if (input.idempotencyKeyHeader) validateHeaderName(input.idempotencyKeyHeader);
  const body = input.body ?? '';
  if (body && ['GET', 'HEAD'].includes(method)) throw new TypeError('sendRequest body is not allowed with GET or HEAD');
  if (Buffer.byteLength(body) > client.maxBodyBytes) throw new TypeError("sendRequest body exceeds the connection's maxBodyBytes");
  if (body && !Object.keys(headers).some(key => key.toLowerCase() === 'content-type' && headers[key])) headers['Content-Type'] = 'application/json';
  return { method, url: requestURL, headers, body, idempotencyKeyHeader: input.idempotencyKeyHeader ?? '', isSafe: ['GET', 'HEAD', 'OPTIONS'].includes(method) };
}

function parseHTTPURL(value: string): URL {
  if (/[^\x21-\x7e]/.test(value) || value.includes('#')) throw new TypeError('URL must be printable ASCII without spaces and cannot contain a fragment');
  if (!value.startsWith('http://') && !value.startsWith('https://')) throw new TypeError('URL must be an absolute http or https URL with a host');
  let url: URL;
  try { url = new URL(value); } catch { throw new TypeError('URL must be an absolute http or https URL with a host'); }
  if (!['http:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password) throw new TypeError('URL must be an absolute http or https URL with a host and no user information');
  return url;
}

function validateHeaderName(name: string): void {
  if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name)) throw new TypeError('sendRequest header names must be RFC 7230 tokens');
  if (['authorization', 'proxy-authorization', 'host', 'content-length', 'transfer-encoding', 'connection', 'keep-alive', 'te', 'trailer', 'upgrade', 'cookie'].includes(name.toLowerCase()) || name.toLowerCase().startsWith('proxy-')) {
    throw new TypeError('sendRequest headers cannot name Authorization or a header the transport owns');
  }
}

function parseGoQuery(rawQuery: string): URLSearchParams {
  const query = new URLSearchParams();
  for (const pair of rawQuery.replace(/^\?/, '').split('&')) {
    if (!pair || pair.includes(';')) continue;
    const separator = pair.indexOf('=');
    const rawName = separator < 0 ? pair : pair.slice(0, separator);
    const rawValue = separator < 0 ? '' : pair.slice(separator + 1);
    try {
      query.append(decodeURIComponent(rawName.replace(/\+/g, '%20')), decodeURIComponent(rawValue.replace(/\+/g, '%20')));
    } catch { /* Go ParseQuery discards a pair with malformed percent-encoding. */ }
  }
  return query;
}

function encodeGoQuery(query: URLSearchParams): string {
  const entries = [...query.entries()].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
  const escape = (value: string): string => encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
  return entries.map(([key, value]) => `${escape(key)}=${escape(value)}`).join('&');
}
