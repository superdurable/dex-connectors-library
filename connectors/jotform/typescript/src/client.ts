// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSubmissionDefinition, ListFormsDefinition } from './generated/definitions.js';
import type { Config, Credentials, FormMetadata, FormsPage, GetSubmissionInput, ListFormsInput, SubmissionMetadata } from './generated/models.js';

const emptyPage = (): FormsPage => ({ forms: [], offset: 0, limit: 0 });
const emptySubmission = (): SubmissionMetadata => ({ id: '', formId: '', status: '', createdAt: '' });
const numericID = /^[0-9]{1,24}$/;

/** Optional provider transport for local tests and examples. */
export interface JotformClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Jotform client with call-time API key resolution. */
export class JotformClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting Jotform. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JotformClientOptions = {}) {
    if (!credentials) throw new TypeError('Jotform credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.jotform.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Jotform timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Jotform response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the form-list Query. */ listForms(): ListFormsOperation { return new ListFormsOperation(this); }
  /** Bind the submission metadata Query. */ getSubmission(): GetSubmissionOperation { return new GetSubmissionOperation(this); }

  /** Fetch one provider response and classify transport and HTTP failures. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let apiKey: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET', headers: { Accept: 'application/json', APIKEY: apiKey },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Jotform response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Jotform request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Jotform rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Jotform is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Jotform did not find the submission') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Jotform rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Jotform response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List form IDs, titles, and statuses without questions or answers. */
export class ListFormsOperation implements Query<ListFormsInput, FormsPage> {
  /** Bind the provider client. */ constructor(private readonly client: JotformClient) {}
  /** Return the manifest-generated contract. */ definition(): QueryDefinition { return ListFormsDefinition; }
  /** Read one page or select a Go-compatible optional branch. */
  async invoke(call: Call, input: ListFormsInput): Promise<QueryAttempt<FormsPage>> {
    const offset = input?.offset ?? 0;
    if (!input || !Number.isSafeInteger(offset) || offset < 0 || (input.limit !== undefined && (!Number.isSafeInteger(input.limit) || input.limit < 0 || input.limit > 100))) {
      return terminal('listForms', 'defect', 'VALIDATION', 'offset or limit is invalid', emptyPage());
    }
    const limit = input.limit || 20;
    const path = `/user/forms?${new URLSearchParams({ offset: String(offset), limit: String(limit) })}`;
    const result = await this.client.exchange(call, 'listForms', path, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPage(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document)) return invalid('listForms', emptyPage());
    const providerDecision = responseCodeDecision('listForms', document, emptyPage(), false);
    if (providerDecision) return providerDecision;
    if (!Array.isArray(document.content)) return invalid('listForms', emptyPage());
    const forms: FormMetadata[] = [];
    for (const item of document.content) {
      if (!isObject(item) || typeof item.id !== 'string' || !numericID.test(item.id) || typeof item.title !== 'string' || !item.title || (item.status != null && typeof item.status !== 'string')) return invalid('listForms', emptyPage());
      forms.push({ id: item.id, title: item.title, status: item.status as string | undefined ?? '' });
    }
    return newQueryBranch('listed', { forms, offset, limit });
  }
}

/** Read one submission's ID, form ID, status, and timestamp without answers. */
export class GetSubmissionOperation implements Query<GetSubmissionInput, SubmissionMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: JotformClient) {}
  /** Return the manifest-generated contract. */ definition(): QueryDefinition { return GetSubmissionDefinition; }
  /** Read a submission or select a Go-compatible optional branch. */
  async invoke(call: Call, input: GetSubmissionInput): Promise<QueryAttempt<SubmissionMetadata>> {
    if (!input || typeof input.submissionId !== 'string' || !numericID.test(input.submissionId)) return terminal('getSubmission', 'defect', 'VALIDATION', 'submission ID must be numeric', emptySubmission());
    const result = await this.client.exchange(call, 'getSubmission', `/submission/${input.submissionId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptySubmission(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document)) return invalid('getSubmission', emptySubmission());
    const providerDecision = responseCodeDecision('getSubmission', document, emptySubmission(), true);
    if (providerDecision) return providerDecision;
    const content = document.content;
    if (!isObject(content) || typeof content.id !== 'string' || !numericID.test(content.id) || typeof content.form_id !== 'string' || !numericID.test(content.form_id) || typeof content.created_at !== 'string' || !content.created_at || (content.status != null && typeof content.status !== 'string')) return invalid('getSubmission', emptySubmission());
    return newQueryBranch('found', { id: content.id, formId: content.form_id, status: content.status as string | undefined ?? '', createdAt: content.created_at });
  }
}

function responseCodeDecision<Output>(operation: string, document: Record<string, unknown>, empty: Output, hasNotFound: boolean): QueryAttempt<Output> | undefined {
  const code = document.responseCode;
  if (code != null && (!Number.isInteger(code) || typeof code !== 'number')) return invalid(operation, empty);
  if (code === 200) return undefined;
  if (code === 429 || (typeof code === 'number' && code >= 500)) return newQueryRetry(failure(operation, 'AVAILABILITY', 'Jotform reported temporary unavailability'));
  if (code === 404 && hasNotFound) return terminal(operation, 'notFound', 'NOT_FOUND', 'Jotform did not find the submission', empty);
  return terminal(operation, 'providerRejected', 'PROVIDER_REJECTION', 'Jotform rejected the request', empty);
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'jotform', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Jotform returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
