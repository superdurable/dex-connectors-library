// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetResponseDefinition, ListSurveysDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetResponseInput, ListSurveysInput, ResponseMetadata, SurveyMetadata, SurveysPage } from './generated/models.js';

const defaultBaseUrl = 'https://api.surveymonkey.com/v3';
const numericId = /^[0-9]{1,24}$/;
const emptySurveys = (): SurveysPage => ({ surveys: [], page: 0, perPage: 0, total: 0 });
const emptyResponse = (): ResponseMetadata => ({ id: '', surveyId: '', status: '', createdAt: '', modifiedAt: '' });

/** Optional transport dependency for provider tests and local Dex examples. */
export interface SurveyMonkeyClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated SurveyMonkey API v3 client with call-time Access Token resolution. */
export class SurveyMonkeyClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting SurveyMonkey. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SurveyMonkeyClientOptions = {}) {
    if (!credentials) throw new TypeError('SurveyMonkey credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('SurveyMonkey timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('SurveyMonkey response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the survey page Query. */ listSurveys(): ListSurveysOperation { return new ListSurveysOperation(this); }
  /** Bind the response metadata Query. */ getResponse(): GetResponseOperation { return new GetResponseOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!token || !isHeaderSafeCredential(token)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'SurveyMonkey response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'SurveyMonkey request failed') };
    }
    const status = response.status;
    const retryAfter = headerValue(response.headers['retry-after']);
    if (status === 429) return { retry: true, retryAfterMs: parseRetryAfter(retryAfter), failure: failure(operation, 'RATE_LIMIT', 'SurveyMonkey rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: parseRetryAfter(retryAfter), failure: failure(operation, 'AVAILABILITY', 'SurveyMonkey is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'SurveyMonkey did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `SurveyMonkey rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'SurveyMonkey response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List one page of surveys accessible to the connected account. */
export class ListSurveysOperation implements Query<ListSurveysInput, SurveysPage> {
  /** Bind the provider client. */ constructor(private readonly client: SurveyMonkeyClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListSurveysDefinition; }
  /** Validate pagination and read survey identities. */
  async invoke(call: Call, input: ListSurveysInput): Promise<QueryAttempt<SurveysPage>> {
    const page = positivePage(input.page);
    const perPage = pageSize(input.perPage);
    if (page === undefined || perPage === undefined) return terminal('listSurveys', 'defect', 'VALIDATION', 'page or perPage is invalid', emptySurveys());
    const result = await this.client.exchange(call, 'listSurveys', `/surveys?page=${page}&per_page=${perPage}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptySurveys(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.data) || !Number.isInteger(document.page) || document.page !== page || !isPositiveInteger(document.per_page) || !isNonnegativeInteger(document.total)) return invalid('listSurveys', emptySurveys());
    const surveys: SurveyMetadata[] = [];
    for (const item of document.data) {
      if (!isObject(item) || !isNumericId(item.id) || (item.title !== undefined && typeof item.title !== 'string')) return invalid('listSurveys', emptySurveys());
      surveys.push({ id: item.id, title: typeof item.title === 'string' ? item.title : '' });
    }
    return newQueryBranch('listed', { surveys, page, perPage: document.per_page as number, total: document.total as number });
  }
}

/** Read one response's identity and status without answers or respondent data. */
export class GetResponseOperation implements Query<GetResponseInput, ResponseMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: SurveyMonkeyClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetResponseDefinition; }
  /** Validate both numeric IDs and read response metadata. */
  async invoke(call: Call, input: GetResponseInput): Promise<QueryAttempt<ResponseMetadata>> {
    if (!isNumericId(input.surveyId) || !isNumericId(input.responseId)) return terminal('getResponse', 'defect', 'VALIDATION', 'survey and response IDs must be numeric', emptyResponse());
    const result = await this.client.exchange(call, 'getResponse', `/surveys/${input.surveyId}/responses/${input.responseId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyResponse(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !isNumericId(document.id) || !isNumericId(document.survey_id) || typeof document.response_status !== 'string' || document.response_status === '' || (document.date_created !== undefined && typeof document.date_created !== 'string') || (document.date_modified !== undefined && typeof document.date_modified !== 'string')) return invalid('getResponse', emptyResponse());
    return newQueryBranch('found', { id: document.id, surveyId: document.survey_id, status: document.response_status, createdAt: typeof document.date_created === 'string' ? document.date_created : '', modifiedAt: typeof document.date_modified === 'string' ? document.date_modified : '' });
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'surveymonkey', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'SurveyMonkey returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isNumericId(value: unknown): value is string { return typeof value === 'string' && numericId.test(value); }
function isPositiveInteger(value: unknown): value is number { return typeof value === 'number' && Number.isInteger(value) && value > 0; }
function isNonnegativeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isInteger(value) && value >= 0; }
function positivePage(value: unknown): number | undefined { return value === undefined || value === 0 ? 1 : isPositiveInteger(value) ? value : undefined; }
function pageSize(value: unknown): number | undefined { return value === undefined || value === 0 ? 50 : isPositiveInteger(value) && value <= 100 ? value : undefined; }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
