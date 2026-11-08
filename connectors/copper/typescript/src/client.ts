// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetLeadDefinition, ListLeadStatusesDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetLeadInput, LeadMetadata, LeadStatusMetadata, LeadStatusesList, ListLeadStatusesInput } from './generated/models.js';

const emptyStatuses = (): LeadStatusesList => ({ statuses: [] });
const emptyLead = (): LeadMetadata => ({ id: 0, name: '', status: '', companyName: '' });
const userAgent = 'Dex Connector (https://superdurable.io)';

/** Optional transport dependency for provider tests and local Dex examples. */
export interface CopperClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Copper Developer API v1 client with call-time credential resolution. */
export class CopperClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection configuration without contacting Copper. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CopperClientOptions = {}) {
    if (!credentials) throw new TypeError('Copper credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.copper.com/developer_api/v1');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Copper timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Copper response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the lead status discovery Query. */ listLeadStatuses(): ListLeadStatusesOperation { return new ListLeadStatusesOperation(this); }
  /** Bind the lead metadata Query. */ getLead(): GetLeadOperation { return new GetLeadOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let apiKey: string;
    let userEmail: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : '';
      userEmail = resolved.userEmail instanceof SecretString ? resolved.userEmail.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!isHeaderSafeCredential(apiKey) || !isHeaderSafeCredential(userEmail) || !userEmail.includes('@') || userEmail.trim() !== userEmail) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-PW-AccessToken': apiKey, 'X-PW-Application': 'developer_api', 'X-PW-UserEmail': userEmail, 'User-Agent': userAgent },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Copper response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Copper request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Copper rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Copper is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Copper did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Copper rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Copper response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List Copper lead status IDs and names without private fields. */
export class ListLeadStatusesOperation implements Query<ListLeadStatusesInput, LeadStatusesList> {
  /** Bind the provider client. */ constructor(private readonly client: CopperClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return ListLeadStatusesDefinition; }
  /** Read visible lead status identities. */
  async invoke(call: Call, input: ListLeadStatusesInput): Promise<QueryAttempt<LeadStatusesList>> {
    if (!input || typeof input !== 'object') return terminal('listLeadStatuses', 'defect', 'VALIDATION', 'lead status list input is invalid', emptyStatuses());
    const result = await this.client.exchange(call, 'listLeadStatuses', '/lead_statuses', false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyStatuses(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!Array.isArray(document)) return invalid('listLeadStatuses', emptyStatuses());
    const statuses: LeadStatusMetadata[] = [];
    for (const item of document) {
      if (!isObject(item) || !isPositiveSafeInteger(item.id) || typeof item.name !== 'string' || !item.name) return invalid('listLeadStatuses', emptyStatuses());
      statuses.push({ id: item.id, name: item.name });
    }
    return newQueryBranch('listed', { statuses });
  }
}

/** Read one Copper lead's identity and status without custom fields. */
export class GetLeadOperation implements Query<GetLeadInput, LeadMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: CopperClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return GetLeadDefinition; }
  /** Validate the lead ID and read its metadata. */
  async invoke(call: Call, input: GetLeadInput): Promise<QueryAttempt<LeadMetadata>> {
    if (!isValidLeadID(input)) return terminal('getLead', 'defect', 'VALIDATION', 'lead ID must be a positive decimal identifier', emptyLead());
    const result = await this.client.exchange(call, 'getLead', `/leads/${input.leadId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyLead(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !isPositiveSafeInteger(document.id) || typeof document.name !== 'string' || !document.name || typeof document.status !== 'string' || !document.status || (document.company_name !== undefined && document.company_name !== null && typeof document.company_name !== 'string')) return invalid('getLead', emptyLead());
    return newQueryBranch('found', { id: document.id, name: document.name, status: document.status, companyName: (document.company_name as string | null | undefined) ?? '' });
  }
}

function isValidLeadID(input: GetLeadInput): boolean {
  if (!input || typeof input.leadId !== 'string' || !/^[0-9]+$/.test(input.leadId)) return false;
  const significantDigits = input.leadId.replace(/^0+/, '');
  return significantDigits.length > 0 && significantDigits.length <= 19 && BigInt(significantDigits) <= 9223372036854775807n;
}
function isPositiveSafeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'copper', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Copper returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
