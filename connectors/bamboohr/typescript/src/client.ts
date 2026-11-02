// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { AddEmployeeOperation, FindEmployeeByEmailOperation, GetEmployeeOperation, ListEmployeeChangesOperation, ListTimeOffRequestsOperation, UpdateEmployeeOperation } from './operations.js';

/** Sender override for provider replay and local tests. */
export type BambooHRRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional local transport and clock dependencies. */
export interface BambooHRClientOptions {
  /** Local fake base URL; omit for the company domain. */ apiBaseURL?: string;
  /** Provider HTTP sender. */ send?: BambooHRRequestSender;
  /** Receipt and retry clock. */ now?: () => Date;
}

/** One classified provider exchange. */
export type BambooHRExchange =
  | { kind: 'success'; response: ProviderResponse }
  | { kind: 'notFound' | 'rateLimited' | 'notSent' | 'unavailable' | 'rejected' | 'invalid' | 'defect'; failure: Failure; retryAfterMs: number };

/** Authenticated BambooHR API v1 client. */
export class BambooHRClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: BambooHRRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without contacting BambooHR. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: BambooHRClientOptions = {}) {
    if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(config.companyDomain)) throw new TypeError('invalid BambooHR companyDomain');
    if (!credentials) throw new TypeError('BambooHR credential provider is required');
    this.endpoint = validateBaseURL(options.apiBaseURL ?? `https://${config.companyDomain}.bamboohr.com/api/v1`);
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('invalid BambooHR maxResponseBytes');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the getEmployee Query. */ getEmployee(): GetEmployeeOperation { return new GetEmployeeOperation(this); }
  /** Bind the findEmployeeByEmail Query. */ findEmployeeByEmail(): FindEmployeeByEmailOperation { return new FindEmployeeByEmailOperation(this); }
  /** Bind the listEmployeeChanges Query. */ listEmployeeChanges(): ListEmployeeChangesOperation { return new ListEmployeeChangesOperation(this); }
  /** Bind the listTimeOffRequests Query. */ listTimeOffRequests(): ListTimeOffRequestsOperation { return new ListTimeOffRequestsOperation(this); }
  /** Bind the updateEmployee Mutation. */ updateEmployee(): UpdateEmployeeOperation { return new UpdateEmployeeOperation(this); }
  /** Bind the addEmployee Mutation. */ addEmployee(): AddEmployeeOperation { return new AddEmployeeOperation(this); }

  /** Make a secret-free provider receipt. */
  receipt(call: Call, employeeId = ''): ReceiptDraft {
    return { callId: call.id, provider: 'bamboohr', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(employeeId ? { providerObjectId: employeeId } : {}) };
  }

  /** Resolve credentials before every request. */
  async resolveKey(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) return undefined;
      const key = credentials.apiKey.reveal();
      return key && isHeaderSafeCredential(key) && !key.includes(':') ? key : undefined;
    } catch { return undefined; }
  }

  /** Send one bounded request and classify the provider result. */
  async exchange(call: Call, operation: string, key: string, method: string, path: string, query?: URLSearchParams, payload?: unknown, signal?: AbortSignal): Promise<BambooHRExchange> {
    const url = this.endpoint + path + (query && [...query].length ? `?${query.toString()}` : '');
    const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${key}:x`).toString('base64')}` };
    let body: Uint8Array | undefined;
    if (payload !== undefined) {
      try { body = Buffer.from(JSON.stringify(payload)); headers['Content-Type'] = 'application/json'; }
      catch { return this.failure('defect', operation, 'LOCAL_DEFECT', 'BambooHR request could not be built'); }
    }
    let response: ProviderResponse;
    try {
      response = await this.send({ url, method, headers, ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.endpoint.startsWith('https://') && !this.endpoint.includes('localhost') && !this.endpoint.includes('127.0.0.1') ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: 9_000, signal: signal ?? call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status >= 200 && error.status < 300) return this.failure('invalid', operation, 'RESPONSE_TOO_LARGE', 'BambooHR response exceeds the configured maxResponseBytes limit');
        if (error.status !== undefined && error.headers) response = { status: error.status, headers: error.headers, body: Buffer.alloc(0) };
        else return this.failure('unavailable', operation, 'TRANSPORT', 'BambooHR response could not be read');
      } else {
        if (error instanceof ProviderTransportError && error.isProvablyNotSent) return this.failure('notSent', operation, 'TRANSPORT', 'BambooHR could not be reached; no request was sent');
        return this.failure('unavailable', operation, 'TRANSPORT', 'BambooHR request failed before a response arrived');
      }
    }
    if (response.status >= 200 && response.status < 300) {
      if (Buffer.from(response.body).includes(Buffer.from(key))) return this.failure('invalid', operation, 'PROTOCOL', 'BambooHR response reflected the connection credential');
      return { kind: 'success', response };
    }
    const retryAfterMs = parseRetryAfter(header(response, 'retry-after'), this.now());
    const code = errorCode(response.body, key);
    const suffix = code ? ` [${code}]` : '';
    const status = response.status;
    if (status === 429) return this.failure('rateLimited', operation, 'RATE_LIMIT', `BambooHR rate limited the request (HTTP 429)${suffix}`, retryAfterMs);
    if (status === 408 || status >= 500) return this.failure('unavailable', operation, 'AVAILABILITY', `BambooHR could not complete the request (HTTP ${status})${suffix}`, retryAfterMs);
    if (status === 404) return this.failure('notFound', operation, 'NOT_FOUND', `BambooHR found no such resource (HTTP 404)${suffix}`);
    if (status >= 300 && status < 400) return this.failure('rejected', operation, 'PROTOCOL', `BambooHR redirected the request (HTTP ${status}); check companyDomain`);
    const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 409 ? 'CONFLICT' : [400, 406, 422].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION';
    return this.failure('rejected', operation, kind, `BambooHR rejected the request (HTTP ${status})${suffix}`);
  }

  private failure(kind: Exclude<BambooHRExchange['kind'], 'success'>, operation: string, failureKind: Failure['kind'], message: string, retryAfterMs = 0): BambooHRExchange {
    return { kind, failure: { kind: failureKind, provider: 'bamboohr', operation, message }, retryAfterMs };
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

function errorCode(body: Uint8Array, key: string): string {
  try {
    const value: unknown = JSON.parse(Buffer.from(body).toString());
    if (!value || typeof value !== 'object') return '';
    const document = value as Record<string, unknown>;
    const nested = document.error && typeof document.error === 'object' ? (document.error as Record<string, unknown>).code : undefined;
    const code = nested ?? document.code;
    return typeof code === 'string' && /^[A-Za-z][A-Za-z0-9_.-]{0,63}$/.test(code) && !code.includes(key) ? code : '';
  } catch { return ''; }
}
