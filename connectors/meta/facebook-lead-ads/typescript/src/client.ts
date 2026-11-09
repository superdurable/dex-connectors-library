// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetLeadDefinition, ListLeadFormsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetLeadInput, LeadFormMetadata, LeadFormsPage, LeadMetadata, ListLeadFormsInput } from './generated/models.js';

const emptyPage = (): LeadFormsPage => ({ forms: [], nextCursor: '' });
const emptyLead = (): LeadMetadata => ({ id: '', formId: '', adId: '', createdTime: '' });
const numericID = /^[0-9]{1,40}$/;

/** Optional provider transport for local tests and examples. */
export interface FacebookLeadAdsClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Meta client with call-time Page token resolution. */
export class FacebookLeadAdsClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting Meta. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FacebookLeadAdsClientOptions = {}) {
    if (!credentials) throw new TypeError('Facebook Lead Ads credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://graph.facebook.com/v25.0');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Meta timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Meta response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the Page form Query. */ listLeadForms(): ListLeadFormsOperation { return new ListLeadFormsOperation(this); }
  /** Bind the lead identity Query. */ getLead(): GetLeadOperation { return new GetLeadOperation(this); }

  /** Fetch one provider response and classify transport and HTTP failures. */
  async exchange(call: Call, operation: string, path: string): Promise<ProviderOutcome> {
    let accessToken: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessToken = resolved.pageAccessToken instanceof SecretString ? resolved.pageAccessToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!accessToken || !isHeaderSafeCredential(accessToken)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${accessToken}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Meta request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (Buffer.from(response.body).includes(accessToken)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta is temporarily unavailable') };
    const graphError = status >= 400 ? decodeGraphError(response.body) : undefined;
    if (graphError?.isThrottled) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (graphError?.isTransient) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta reported a transient error') };
    if (status === 404) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Meta did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Meta rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Read one Page of form IDs, names, and statuses without questions. */
export class ListLeadFormsOperation implements Query<ListLeadFormsInput, LeadFormsPage> {
  /** Bind the provider client. */ constructor(private readonly client: FacebookLeadAdsClient) {}
  /** Return the manifest-generated contract. */ definition(): QueryDefinition { return ListLeadFormsDefinition; }
  /** Read one form page or select a Go-compatible optional branch. */
  async invoke(call: Call, input: ListLeadFormsInput): Promise<QueryAttempt<LeadFormsPage>> {
    if (!input || typeof input.pageId !== 'string' || !numericID.test(input.pageId) || (input.after !== undefined && (typeof input.after !== 'string' || input.after.length > 4096 || /[\r\n\0]/.test(input.after)))) {
      return terminal('listLeadForms', 'defect', 'VALIDATION', 'Page ID or cursor is invalid', emptyPage());
    }
    const query = new URLSearchParams({ fields: 'id,name,status', limit: '100' });
    if (input.after) query.set('after', input.after);
    const result = await this.client.exchange(call, 'listLeadForms', `/${input.pageId}/leadgen_forms?${query}`);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPage(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.data) || document.data.length > 100) return invalid('listLeadForms', emptyPage());
    const paging = document.paging ?? {};
    if (!isObject(paging)) return invalid('listLeadForms', emptyPage());
    const cursors = paging.cursors ?? {};
    if (!isObject(cursors)) return invalid('listLeadForms', emptyPage());
    const after = cursors.after ?? '';
    if (typeof after !== 'string' || after.length > 4096) return invalid('listLeadForms', emptyPage());
    const forms: LeadFormMetadata[] = [];
    for (const item of document.data) {
      if (!isObject(item) || typeof item.id !== 'string' || !numericID.test(item.id) || typeof item.name !== 'string' || !item.name || (item.status != null && typeof item.status !== 'string')) return invalid('listLeadForms', emptyPage());
      forms.push({ id: item.id, name: item.name, status: item.status as string | undefined ?? '' });
    }
    return newQueryBranch('listed', { forms, nextCursor: after });
  }
}

/** Read one lead's identity without answer fields. */
export class GetLeadOperation implements Query<GetLeadInput, LeadMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: FacebookLeadAdsClient) {}
  /** Return the manifest-generated contract. */ definition(): QueryDefinition { return GetLeadDefinition; }
  /** Read lead metadata or select a Go-compatible optional branch. */
  async invoke(call: Call, input: GetLeadInput): Promise<QueryAttempt<LeadMetadata>> {
    if (!input || typeof input.leadId !== 'string' || !numericID.test(input.leadId)) return terminal('getLead', 'defect', 'VALIDATION', 'lead ID must be numeric', emptyLead());
    const query = new URLSearchParams({ fields: 'id,created_time,form_id,ad_id' });
    const result = await this.client.exchange(call, 'getLead', `/${input.leadId}?${query}`);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyLead(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || typeof document.id !== 'string' || !numericID.test(document.id) || typeof document.form_id !== 'string' || !numericID.test(document.form_id) || typeof document.created_time !== 'string' || !document.created_time || (document.ad_id != null && (typeof document.ad_id !== 'string' || (document.ad_id && !numericID.test(document.ad_id))))) return invalid('getLead', emptyLead());
    return newQueryBranch('found', { id: document.id, formId: document.form_id, adId: document.ad_id as string | undefined ?? '', createdTime: document.created_time });
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'facebook-lead-ads', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Meta returned invalid JSON', empty); }
/** Decode Meta's Graph API error envelope; undefined means the body is not one. */
function decodeGraphError(body: Uint8Array): { isThrottled: boolean; isTransient: boolean } | undefined {
  const document = parseJSON(body);
  if (!isObject(document) || !isObject(document.error)) return undefined;
  const code = document.error.code ?? 0;
  const isTransient = document.error.is_transient ?? false;
  if (!Number.isSafeInteger(code) || typeof isTransient !== 'boolean') return undefined;
  const numericCode = code as number;
  // Meta documents these rate-limit codes, which Graph returns with HTTP 4xx.
  const isThrottled = [4, 17, 32, 341, 613].includes(numericCode) || (numericCode >= 80_000 && numericCode <= 80_014);
  return { isThrottled, isTransient };
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
