// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPropertyDefinition, ListAudiencesDefinition, RunReportDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Audience, AudiencesPage, Config, Credentials, GetPropertyInput, ListAudiencesInput, Property, ReportPage, ReportRow, RunReportInput } from './generated/models.js';

/** Injectable provider transport and OAuth refresh dependencies. */
export interface Analytics4ClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}

/** One bounded Google Analytics 4 response. */
export interface Analytics4Exchange {
  /** Provider status, or zero before response headers. */ status: number;
  /** Bounded response bytes retained only in process memory. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local error class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Google Analytics 4 client that resolves credentials for each call. */
export class Analytics4Client {
  private readonly dataBaseURL: string;
  private readonly adminBaseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting Google. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: Analytics4ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Google Analytics 4 configuration and credentials are required');
    this.dataBaseURL = validateBaseURL(config.dataBaseUrl || 'https://analyticsdata.googleapis.com');
    this.adminBaseURL = validateBaseURL(config.adminBaseUrl || 'https://analyticsadmin.googleapis.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Google Analytics 4 request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Google Analytics 4', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the report Query. */ runReport(): RunReportOperation { return new RunReportOperation(this); }
  /** Bind the property Query. */ getProperty(): GetPropertyOperation { return new GetPropertyOperation(this); }
  /** Bind the audience Query. */ listAudiences(): ListAudiencesOperation { return new ListAudiencesOperation(this); }

  /** Send one bounded provider request for a Query. */
  async request(call: Call, api: 'data' | 'admin', method: 'GET' | 'POST', path: string, payload?: object): Promise<Analytics4Exchange> {
    let token: string;
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      if (!token || !isHeaderSafeCredential(token)) return failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    const baseURL = api === 'data' ? this.dataBaseURL : this.adminBaseURL;
    const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' };
    if (payload !== undefined) headers['Content-Type'] = 'application/json';
    const request: ProviderRequest = {
      url: baseURL + path, method, headers, body: payload === undefined ? undefined : Buffer.from(JSON.stringify(payload)),
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()) };
      if (error instanceof ProviderTransportError) return failedExchange('request');
      return failedExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Run one bounded GA4 Data API report. */
export class RunReportOperation implements Query<RunReportInput, ReportPage> {
  /** Bind the provider client. */ constructor(private readonly client: Analytics4Client) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return RunReportDefinition; }
  /** Project one bounded report page. */
  async invoke(call: Call, input: RunReportInput): Promise<QueryAttempt<ReportPage>> {
    const operation = 'runReport'; const empty: ReportPage = { rows: [], rowCount: 0 };
    const dimensions = input?.dimensions ?? []; const metrics = input?.metrics ?? []; const requestedLimit = input?.limit ?? 0;
    if (!input || !validPropertyID(input.propertyId) || !validDateRange(input.startDate, input.endDate)
      || !validNames(dimensions, maxReportDimensions, false) || !validNames(metrics, maxReportMetrics, true)
      || !Number.isSafeInteger(requestedLimit) || requestedLimit < 0 || requestedLimit > 10_000
      || !Number.isSafeInteger(input.offset) || input.offset < 0 || input.offset > 1_000_000_000)
      return queryBranch(operation, 'defect', empty, 'VALIDATION', 'report input is invalid');
    const limit = requestedLimit || 100;
    const payload = { dateRanges: [{ startDate: input.startDate, endDate: input.endDate }], dimensions: dimensions.map(name => ({ name })), metrics: metrics.map(name => ({ name })), limit: String(limit), offset: String(input.offset) };
    const exchange = await this.client.request(call, 'data', 'POST', `/v1beta/properties/${input.propertyId}:runReport`, payload);
    const decision = classifyRead(operation, exchange, empty);
    if (decision) return decision;
    const document = parseJSON(exchange.body);
    if (!isObject(document) || (document.rows !== undefined && !Array.isArray(document.rows)) || (Array.isArray(document.rows) && document.rows.length > limit))
      return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid report JSON');
    const rowCount = document.rowCount ?? 0;
    if ((typeof rowCount !== 'number' && typeof rowCount !== 'string') || !/^\d+$/.test(String(rowCount)) || !Number.isSafeInteger(Number(rowCount)))
      return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid report row count');
    const rows: ReportRow[] = [];
    for (const row of (document.rows ?? []) as unknown[]) {
      if (!isObject(row) || (row.dimensionValues !== undefined && !Array.isArray(row.dimensionValues)) || (row.metricValues !== undefined && !Array.isArray(row.metricValues)))
        return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid report JSON');
      const dimensionValues = (row.dimensionValues ?? []) as unknown[]; const metricValues = (row.metricValues ?? []) as unknown[];
      if (dimensionValues.length !== dimensions.length || metricValues.length !== metrics.length || ![...dimensionValues, ...metricValues].every(value => isObject(value) && typeof value.value === 'string'))
        return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned mismatched report columns');
      rows.push({ dimensions: dimensionValues.map(value => (value as { value: string }).value), metrics: metricValues.map(value => (value as { value: string }).value) });
    }
    return newQueryBranch('reported', { rows, rowCount: Number(rowCount) });
  }
}

/** Read one GA4 property's public display metadata. */
export class GetPropertyOperation implements Query<GetPropertyInput, Property> {
  /** Bind the provider client. */ constructor(private readonly client: Analytics4Client) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetPropertyDefinition; }
  /** Project one property. */
  async invoke(call: Call, input: GetPropertyInput): Promise<QueryAttempt<Property>> {
    const operation = 'getProperty'; const empty: Property = { name: '', displayName: '', timeZone: '', currencyCode: '' };
    if (!input || !validPropertyID(input.propertyId)) return queryBranch(operation, 'defect', empty, 'VALIDATION', 'property ID must be numeric');
    const exchange = await this.client.request(call, 'admin', 'GET', `/v1beta/properties/${input.propertyId}`);
    const decision = classifyRead(operation, exchange, empty, true);
    if (decision) return decision;
    const document = parseJSON(exchange.body);
    if (!isObject(document) || document.name !== `properties/${input.propertyId}` || typeof document.displayName !== 'string' || !document.displayName || typeof document.timeZone !== 'string' || !document.timeZone || (document.currencyCode !== undefined && typeof document.currencyCode !== 'string'))
      return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid property JSON');
    return newQueryBranch('found', { name: document.name, displayName: document.displayName, timeZone: document.timeZone, currencyCode: (document.currencyCode as string | undefined) ?? '' });
  }
}

/** Read one page of GA4 audience identities. */
export class ListAudiencesOperation implements Query<ListAudiencesInput, AudiencesPage> {
  /** Bind the provider client. */ constructor(private readonly client: Analytics4Client) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListAudiencesDefinition; }
  /** Project bounded audience names and descriptions. */
  async invoke(call: Call, input: ListAudiencesInput): Promise<QueryAttempt<AudiencesPage>> {
    const operation = 'listAudiences'; const empty: AudiencesPage = { audiences: [], nextPageToken: '' };
    const requestedPageSize = input?.pageSize ?? 0;
    if (!input || !validPropertyID(input.propertyId) || !Number.isSafeInteger(requestedPageSize) || requestedPageSize < 0 || requestedPageSize > 200 || typeof input.pageToken !== 'string' || Buffer.byteLength(input.pageToken) > 4096 || /[\r\n]/.test(input.pageToken))
      return queryBranch(operation, 'defect', empty, 'VALIDATION', 'audience page input is invalid');
    const pageSize = requestedPageSize || 50;
    const values: Record<string, string[]> = { pageSize: [String(pageSize)] };
    if (input.pageToken) values.pageToken = [input.pageToken];
    const exchange = await this.client.request(call, 'admin', 'GET', `/v1alpha/properties/${input.propertyId}/audiences?${new URLSearchParams(Object.entries(values).map(([key, value]) => [key, value[0] ?? ""])).toString()}`);
    const decision = classifyRead(operation, exchange, empty);
    if (decision) return decision;
    const document = parseJSON(exchange.body);
    if (!isObject(document) || (document.audiences !== undefined && !Array.isArray(document.audiences)))
      return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid audience JSON');
    const entries = (document.audiences ?? []) as unknown[];
    const nextPageToken = document.nextPageToken ?? '';
    if (entries.length > pageSize || typeof nextPageToken !== 'string' || Buffer.byteLength(nextPageToken) > 4096)
      return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid audience JSON');
    const audiences: Audience[] = [];
    for (const entry of entries) {
      if (!isObject(entry) || typeof entry.name !== 'string' || !entry.name.startsWith(`properties/${input.propertyId}/audiences/`) || typeof entry.displayName !== 'string' || !entry.displayName || (entry.description !== undefined && typeof entry.description !== 'string'))
        return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned incomplete audience metadata');
      audiences.push({ name: entry.name, displayName: entry.displayName, description: (entry.description as string | undefined) ?? '' });
    }
    return newQueryBranch('listed', { audiences, nextPageToken });
  }
}

function classifyRead<Output>(operation: string, exchange: Analytics4Exchange, empty: Output, notFound = false): QueryAttempt<Output> | undefined {
  if (exchange.error === 'credential') return queryBranch(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'request' || exchange.error === 'refresh') return queryRetry(operation, 'TRANSPORT', 'Google Analytics request failed');
  if (exchange.status === 429) return queryRetry(operation, 'RATE_LIMIT', 'Google rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return queryRetry(operation, 'AVAILABILITY', 'Google Analytics is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 404 && notFound) return queryBranch(operation, 'notFound', empty, 'NOT_FOUND', 'Google did not find the property');
  if (exchange.status !== 200) return queryBranch(operation, 'providerRejected', empty, 'PROVIDER_REJECTION', `Google rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return queryBranch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'Google response exceeded the configured limit');
  return undefined;
}
function queryBranch<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, { kind, provider: 'google-analytics-4', operation, message }); }
function queryRetry<Output>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Output> { return newQueryRetry({ kind, provider: 'google-analytics-4', operation, message }, delay); }
function validPropertyID(value: string): boolean { return typeof value === 'string' && /^[0-9]{1,30}$/.test(value); }
function validDateRange(start: string, end: string): boolean {
  if (typeof start !== 'string' || typeof end !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(start) || !/^\d{4}-\d{2}-\d{2}$/.test(end)) return false;
  const first = new Date(`${start}T00:00:00Z`); const last = new Date(`${end}T00:00:00Z`);
  return !Number.isNaN(first.getTime()) && !Number.isNaN(last.getTime())
    && first.toISOString().slice(0, 10) === start && last.toISOString().slice(0, 10) === end && last >= first;
}
// GA4 runReport accepts at most nine dimensions and ten metrics.
const maxReportDimensions = 9;
const maxReportMetrics = 10;
function validNames(names: string[], maxCount: number, required: boolean): boolean { return Array.isArray(names) && names.length <= maxCount && (!required || names.length > 0) && names.every(name => typeof name === 'string' && /^[A-Za-z][A-Za-z0-9_:]{0,127}$/.test(name)); }
function failedExchange(error: NonNullable<Analytics4Exchange['error']>): Analytics4Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
