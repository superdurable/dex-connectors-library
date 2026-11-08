// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetLeadDefinition, ListLeadsDefinition } from './generated/definitions.js';
import type { Config, LeadMetadata, LeadsPage, Credentials, GetLeadInput, ListLeadsInput } from './generated/models.js';

const emptyPage = (): LeadsPage => ({ leads: [], page: 0, perPage: 0 });
const emptyLead = (): LeadMetadata => ({ id: 0, firstName: '', lastName: '', emailAddress: '', organizationName: '' });
const userAgent = 'Dex Connector (https://superdurable.io)';

/** Optional transport dependency for provider tests and local Dex examples. */
export interface ZendeskSellClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Zendesk Sell CRM API v2 client with call-time credential resolution. */
export class ZendeskSellClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection configuration without contacting Zendesk Sell. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ZendeskSellClientOptions = {}) {
    if (!credentials) throw new TypeError('Zendesk Sell credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.getbase.com/v2');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Zendesk Sell timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Zendesk Sell response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the lead page Query. */ listLeads(): ListLeadsOperation { return new ListLeadsOperation(this); }
  /** Bind the lead metadata Query. */ getLead(): GetLeadOperation { return new GetLeadOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let accessToken: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessToken = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!isHeaderSafeCredential(accessToken)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${accessToken}`, 'User-Agent': userAgent },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Zendesk Sell response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Zendesk Sell request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Zendesk Sell rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Zendesk Sell is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Zendesk Sell did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Zendesk Sell rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Zendesk Sell response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Read one bounded page of Zendesk Sell lead identities. */
export class ListLeadsOperation implements Query<ListLeadsInput, LeadsPage> {
  /** Bind the provider client. */ constructor(private readonly client: ZendeskSellClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return ListLeadsDefinition; }
  /** Read one lead page. */
  async invoke(call: Call, input: ListLeadsInput): Promise<QueryAttempt<LeadsPage>> {
    if (!input || !Number.isSafeInteger(input.page) || input.page < 1 || !Number.isSafeInteger(input.perPage) || input.perPage < 1 || input.perPage > 100) return terminal('listLeads', 'defect', 'VALIDATION', 'lead page number or size is invalid', emptyPage());
    const result = await this.client.exchange(call, 'listLeads', `/leads?page=${input.page}&per_page=${input.perPage}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPage(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.items)) return invalid('listLeads', emptyPage());
    const leads: LeadMetadata[] = [];
    for (const item of document.items) {
      const lead = decodeLead(isObject(item) ? item.data : undefined);
      if (!lead) return invalid('listLeads', emptyPage());
      leads.push(lead);
    }
    return newQueryBranch('listed', { leads, page: input.page, perPage: input.perPage });
  }
}

/** Read one Zendesk Sell lead's identity without custom fields. */
export class GetLeadOperation implements Query<GetLeadInput, LeadMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: ZendeskSellClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return GetLeadDefinition; }
  /** Validate the lead ID and read its metadata. */
  async invoke(call: Call, input: GetLeadInput): Promise<QueryAttempt<LeadMetadata>> {
    if (!isValidLeadID(input)) return terminal('getLead', 'defect', 'VALIDATION', 'lead ID must be a positive decimal identifier', emptyLead());
    const result = await this.client.exchange(call, 'getLead', `/leads/${input.leadId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyLead(), {}, result.failure);
    const document = parseJSON(result.body!);
    const lead = decodeLead(isObject(document) ? document.data : undefined);
    if (!lead) return invalid('getLead', emptyLead());
    return newQueryBranch('found', lead);
  }
}

function isValidLeadID(input: GetLeadInput): boolean {
  if (!input || typeof input.leadId !== 'string' || !/^[0-9]+$/.test(input.leadId)) return false;
  const significantDigits = input.leadId.replace(/^0+/, '');
  return significantDigits.length > 0 && significantDigits.length <= 19 && BigInt(significantDigits) <= 9223372036854775807n;
}
function decodeLead(value: unknown): LeadMetadata | undefined {
  if (!isObject(value) || !isPositiveSafeInteger(value.id)) return undefined;
  const firstName = optionalString(value.first_name);
  const lastName = optionalString(value.last_name);
  const emailAddress = optionalString(value.email);
  const organizationName = optionalString(value.organization_name);
  if (firstName === undefined || lastName === undefined || emailAddress === undefined || organizationName === undefined) return undefined;
  return { id: value.id, firstName, lastName, emailAddress, organizationName };
}
function optionalString(value: unknown): string | undefined { return value === undefined || value === null ? '' : typeof value === 'string' ? value : undefined; }
function isPositiveSafeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'zendesk-sell', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Zendesk Sell returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
