// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Buffer } from 'node:buffer';
import {
  ReauthorizationRequiredError, SecretString, type Call, type Failure,
  type ReceiptDraft,
} from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import {
  BodyTooLargeError, MaxErrorBodyBytes, isHeaderSafeCredential, parseRetryAfter,
  sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import type { IncomingHttpHeaders } from 'node:http';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials, CredentialSource, ProviderError } from './models.js';
import {
  GetRecordOperation, QueryRecordsOperation, UpdateRecordOperation,
  UpsertRecordByExternalIDOperation,
} from './operations.js';

const apiVersionPattern = /^v([1-9][0-9]{1,2})\.0$/;
const apiNamePattern = /^[A-Za-z][A-Za-z0-9_]*$/;
const recordIDPattern = /^[A-Za-z0-9]{15}(?:[A-Za-z0-9]{3})?$/;
const errorCodePattern = /^[A-Z][A-Z0-9_]{0,79}$/;
const apiUsagePattern = /(?:^|,)\s*api-usage=([0-9]{1,12}\/[0-9]{1,12})\s*(?:,|$)/;
const accessCodes = new Set([
  'API_CURRENTLY_DISABLED', 'API_DISABLED_FOR_ORG', 'FUNCTIONALITY_NOT_ENABLED',
  'INSUFFICIENT_ACCESS', 'INSUFFICIENT_ACCESS_OR_READONLY', 'INVALID_AUTH_HEADER',
  'INVALID_SESSION_ID', 'NOT_API_ENABLED', 'ORG_LOCKED',
]);
const pathLiteral = new Set('ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~$&+=:@');

/** Exact Go url.PathEscape encoding for one path segment. */
export function goPathEscape(value: string): string {
  let encoded = '';
  for (const octet of Buffer.from(value, 'utf8')) {
    const character = String.fromCharCode(octet);
    encoded += pathLiteral.has(character) ? character : `%${octet.toString(16).toUpperCase().padStart(2, '0')}`;
  }
  return encoded;
}

function goQueryComponent(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character =>
    `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
}

/** Exact Go url.Values.Encode ordering and form escaping, including literal tilde. */
export function goQuery(values: Record<string, string>): string {
  return Object.keys(values).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
    .map(key => `${goQueryComponent(key)}=${goQueryComponent(values[key]!)}`).join('&');
}

/** Check one Salesforce object or field API name. */
export function isAPIName(value: string): boolean {
  return Buffer.byteLength(value) <= 255 && apiNamePattern.test(value) && !value.endsWith('_');
}

/** Check either Salesforce record ID form. */
export function isRecordID(value: string): boolean { return recordIDPattern.test(value); }

/** Safe bounded provider response. */
export interface SalesforceResponse extends ProviderResponse { apiUsage: string; }

/** Failed exchange classified before a specific operation selects a branch. */
export interface SalesforceOutcome {
  route: 'retry' | 'notFound' | 'requestRejected' | 'providerRejected' | 'invalidResponse' | 'defect';
  failure: Failure;
  retryAfterMs: number;
  providerErrors: ProviderError[];
  apiUsage: string;
}

/** Provider transport and clock seams; production uses the SDK HTTP sender. */
export interface SalesforceClientOptions {
  /** Send one request, preserving the SDK request and response contract. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and Retry-After clock. */
  now?: () => Date;
  /** Per-request timeout; defaults to Go's 25 seconds. */
  timeoutMs?: number;
}

/** Keep provider diagnostics out of one Dex Failure. */
export function salesforceFailure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'salesforce', operation, message };
}

function firstHeader(value: string | string[] | number | undefined): string {
  return typeof value === 'string' ? value : Array.isArray(value) ? value[0] ?? '' : '';
}

/** Extract only a numeric Salesforce API usage ratio. */
export function parseAPIUsage(value: string): string { return apiUsagePattern.exec(value)?.[1] ?? ''; }

/** Keep only validated error codes and API field names. */
export function parseProviderErrors(body: Uint8Array): ProviderError[] {
  let decoded: unknown;
  try { decoded = JSON.parse(Buffer.from(body).toString('utf8')); } catch { return []; }
  if (!Array.isArray(decoded)) return [];
  const errors: ProviderError[] = [];
  for (const value of decoded) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) continue;
    const item = value as Record<string, unknown>;
    if (typeof item.errorCode !== 'string' || !errorCodePattern.test(item.errorCode)) continue;
    const fields = Array.isArray(item.fields) ? item.fields.filter(
      (field): field is string => typeof field === 'string' && isAPIName(field),
    ) : [];
    errors.push({ errorCode: item.errorCode, ...(fields.length ? { fields } : {}) });
  }
  return errors;
}

/** Authenticated Salesforce REST client shared by all four operation implementations. */
export class SalesforceClient {
  readonly apiVersion: string;
  readonly maxResponseBytes: number;
  readonly queryBatchSize: number;
  readonly now: () => Date;
  private readonly credentials: CredentialSource;
  private readonly refreshDriver: ManifestCredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly timeoutMs: number;

  /** Validate the configuration and bind generated OAuth and JWT bearer grants. */
  constructor(config: Config, credentials: CredentialSource, options: SalesforceClientOptions = {}) {
    if (!credentials) throw new TypeError('Salesforce credential provider is required');
    const apiVersion = config.apiVersion || 'v62.0';
    const match = apiVersionPattern.exec(apiVersion);
    if (!match || Number(match[1]) < 46) throw new TypeError('Salesforce apiVersion must be a vNN.0 version no older than v46.0');
    const maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1) throw new TypeError('Salesforce maxResponseBytes must be positive');
    const queryBatchSize = config.queryBatchSize ?? 200;
    if (!Number.isSafeInteger(queryBatchSize) || queryBatchSize < 200 || queryBatchSize > 2000) throw new TypeError('Salesforce queryBatchSize must be from 200 to 2000');
    const timeoutMs = options.timeoutMs ?? 25_000;
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0) throw new TypeError('Salesforce request timeout must be positive');
    this.apiVersion = apiVersion;
    this.maxResponseBytes = maxResponseBytes;
    this.queryBatchSize = queryBatchSize;
    this.credentials = credentials;
    this.now = options.now ?? (() => new Date());
    this.timeoutMs = timeoutMs;
    this.sendRequest = options.send ?? sendProviderRequest;
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Salesforce', auth: Authorization, credentialFields: CredentialFields,
      now: this.now,
    });
  }

  /** Bind the queryRecords Query. */ queryRecords(): QueryRecordsOperation { return new QueryRecordsOperation(this); }
  /** Bind the getRecord Query. */ getRecord(): GetRecordOperation { return new GetRecordOperation(this); }
  /** Bind the external ID upsert Mutation. */ upsertRecordByExternalId(): UpsertRecordByExternalIDOperation { return new UpsertRecordByExternalIDOperation(this); }
  /** Bind the record update Mutation. */ updateRecord(): UpdateRecordOperation { return new UpdateRecordOperation(this); }

  /** Build a versioned REST path from escaped segments. */
  dataPath(...segments: string[]): string {
    return '/services/data/' + this.apiVersion + (segments.length ? '/' + segments.map(goPathEscape).join('/') : '');
  }

  /** Build a safe correlation receipt with validated API usage only. */
  receipt(call: Call, objectID = '', apiUsage = ''): ReceiptDraft {
    return {
      callId: call.id, provider: 'salesforce', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectID ? { providerObjectId: objectID } : {}),
      ...(apiUsage ? { metadata: { apiUsage } } : {}),
    };
  }

  /** Resolve current session material and distinguish terminal from transient failures. */
  async resolve(call: Call, operation: string): Promise<Credentials | SalesforceOutcome> {
    let credentials: Credentials;
    try {
      credentials = await this.credentials.resolveWithRefresh(call, this.refreshDriver, call.context.cancellationSignal);
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return this.outcome(
        'providerRejected', operation, 'AUTHENTICATION',
        'Salesforce authorization is revoked or expired; reauthorize the connection',
      );
      return this.outcome('retry', operation, 'AUTHENTICATION', 'Salesforce credentials are temporarily unavailable');
    }
    if (!this.validCredentials(credentials)) return this.outcome(
      'defect', operation, 'AUTHENTICATION', 'Salesforce connection credentials are missing or invalid',
    );
    return credentials;
  }

  /** Send one request, then optionally refresh and resend after a 401. */
  async exchange(
    call: Call, credentials: Credentials, operation: string,
    request: { method: string; path: string; query?: Record<string, string>; body?: Uint8Array; batchSize?: number; duplicateRetry?: boolean; oversizedRoute?: 'retry' | 'invalidResponse' },
  ): Promise<SalesforceResponse | SalesforceOutcome> {
    for (let attempt = 0; attempt < 2; attempt++) {
      let instanceURL: string;
      try {
        instanceURL = validateBaseURL(credentials.instanceUrl ?? '');
        const parsed = new URL(instanceURL);
        if (parsed.pathname !== '/') throw new TypeError('instance path');
      } catch {
        return this.outcome('defect', operation, 'LOCAL_DEFECT', 'Salesforce request URL could not be built');
      }
      const accessToken = credentials.accessToken;
      if (!(accessToken instanceof SecretString)) return this.outcome(
        'defect', operation, 'AUTHENTICATION', 'Salesforce connection credentials are missing or invalid',
      );
      const headers: Record<string, string> = { Authorization: 'Bearer ' + accessToken.reveal(), Accept: 'application/json' };
      if (request.body) headers['Content-Type'] = 'application/json';
      if (request.batchSize !== undefined) headers['Sforce-Query-Options'] = 'batchSize=' + request.batchSize;
      const url = instanceURL + request.path + (request.query ? '?' + goQuery(request.query) : '');
      let response: ProviderResponse;
      let observedStatus = 0;
      let observedHeaders: IncomingHttpHeaders = {};
      try {
        response = await this.sendRequest({
          url, method: request.method, headers,
          ...(request.body ? { body: request.body } : {}),
          maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes),
          addressPolicy: instanceURL.startsWith('http://') ? 'privateNetworkHTTP' : 'publicHTTPS',
          timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
          onResponseChunk: (_chunk, status, responseHeaders) => {
            observedStatus = status;
            observedHeaders = responseHeaders;
          },
        });
      } catch (error) {
        if (error instanceof BodyTooLargeError) {
          const status = error.status ?? observedStatus;
          const responseHeaders = error.headers ?? observedHeaders;
          const apiUsage = parseAPIUsage(firstHeader(responseHeaders['sforce-limit-info']));
          if (status >= 200 && status < 300) return this.outcome(
            request.oversizedRoute ?? 'invalidResponse', operation, 'RESPONSE_TOO_LARGE',
            'Salesforce response exceeds the configured maxResponseBytes limit', 0, [], apiUsage,
          );
          return this.classify(operation, { status, headers: responseHeaders, body: new Uint8Array(), apiUsage }, request.duplicateRetry ?? false);
        }
        return this.outcome('retry', operation, 'TRANSPORT', 'Salesforce request outcome is unknown');
      }
      const apiUsage = parseAPIUsage(firstHeader(response.headers['sforce-limit-info']));
      if (response.status >= 200 && response.status < 300 && response.body.length > this.maxResponseBytes) return this.outcome(
        request.oversizedRoute ?? 'invalidResponse', operation, 'RESPONSE_TOO_LARGE',
        'Salesforce response exceeds the configured maxResponseBytes limit', 0, [], apiUsage,
      );
      const bounded: SalesforceResponse = { ...response, apiUsage };
      if (response.status === 401 && attempt === 0 && 'resolveAfterRejection' in this.credentials && typeof this.credentials.resolveAfterRejection === 'function') {
        try {
          const replacement = await this.credentials.resolveAfterRejection(call, this.refreshDriver, call.context.cancellationSignal);
          if (this.validCredentials(replacement)) { credentials = replacement; continue; }
        } catch { /* The original 401 remains the definite provider response. */ }
      }
      if ((response.status >= 200 && response.status < 300) || response.status === 300) return bounded;
      return this.classify(operation, bounded, request.duplicateRetry ?? false);
    }
    return this.outcome('defect', operation, 'LOCAL_DEFECT', 'authenticated Salesforce request retry was exhausted');
  }

  private validCredentials(credentials: Credentials): boolean {
    if (!(credentials.accessToken instanceof SecretString) || !isHeaderSafeCredential(credentials.accessToken.reveal())) return false;
    try {
      const parsed = new URL(validateBaseURL(credentials.instanceUrl ?? ''));
      if (parsed.pathname !== '/') return false;
    } catch { return false; }
    return ['', 'salesforce-oauth', 'salesforce-sandbox-oauth', 'salesforce-jwt-bearer'].includes(credentials.authMethodId ?? '');
  }

  private outcome(
    route: SalesforceOutcome['route'], operation: string, kind: Failure['kind'], message: string,
    retryAfterMs = 0, providerErrors: ProviderError[] = [], apiUsage = '',
  ): SalesforceOutcome {
    return { route, failure: salesforceFailure(operation, kind, message), retryAfterMs, providerErrors, apiUsage };
  }

  private classify(operation: string, response: SalesforceResponse, duplicateRetry: boolean): SalesforceOutcome {
    const providerErrors = parseProviderErrors(response.body);
    const code = providerErrors[0]?.errorCode ?? '';
    const message = (prefix: string) => prefix + (code ? ` (${code})` : '');
    const retryAfter = parseRetryAfter(firstHeader(response.headers['retry-after']), this.now());
    const result = (
      route: SalesforceOutcome['route'], kind: Failure['kind'], description: string, delay = 0,
    ) => this.outcome(route, operation, kind, message(description), delay, providerErrors, response.apiUsage);
    if (code === 'REQUEST_LIMIT_EXCEEDED' || response.status === 429) return result('retry', 'RATE_LIMIT', 'Salesforce API request limit was reached', retryAfter);
    if (code === 'UNABLE_TO_LOCK_ROW') return result('retry', 'CONFLICT', 'Salesforce could not lock the record', retryAfter);
    if (code === 'DUPLICATE_VALUE' && duplicateRetry) return result('retry', 'CONFLICT', 'a concurrent write holds the unique value');
    if (code === 'SERVER_UNAVAILABLE' || response.status >= 500) return result('retry', 'AVAILABILITY', 'Salesforce is temporarily unavailable', retryAfter);
    if (response.status === 401 || code === 'INVALID_SESSION_ID') return result('providerRejected', 'AUTHENTICATION', 'Salesforce rejected the session');
    if (accessCodes.has(code) || response.status === 403) return result('providerRejected', 'AUTHORIZATION', 'Salesforce denied access');
    if (code === 'NOT_FOUND' || code === 'ENTITY_IS_DELETED' || response.status === 404) return result('notFound', 'NOT_FOUND', 'Salesforce did not find the resource');
    if ([400, 409, 412, 422].includes(response.status)) return result('requestRejected', 'VALIDATION', 'Salesforce rejected the request');
    return result('providerRejected', 'PROVIDER_REJECTION', 'Salesforce rejected the request');
  }
}
