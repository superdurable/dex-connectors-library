// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetProjectAPISummaryDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetProjectAPISummaryInput, GetProjectAPISummaryOutput } from './generated/models.js';

const endpointRoot = 'https://api.apifox.com/v1/projects/';
const exportBody = Buffer.from('{"scope":{"type":"ALL"},"oasVersion":"3.1","exportFormat":"JSON"}', 'utf8');
const httpMethods = new Set(['get', 'post', 'put', 'patch', 'delete', 'head', 'options', 'trace']);
const emptyOutput: GetProjectAPISummaryOutput = { title: '', version: '', endpointCount: 0 };

/** Send one Apifox provider request through a caller-owned transport. */
export type ApifoxRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests and approved proxies. */
export interface ApifoxClientOptions {
  /** Request sender; production uses the Connector SDK's bounded HTTP transport. */
  send?: ApifoxRequestSender;
}

/** Resolve Apifox credentials for each read-only project export. */
export class ApifoxClient {
  readonly maxResponseBytes: number;
  readonly timeoutMs: number;
  readonly send: ApifoxRequestSender;

  /** Validate settings without contacting Apifox. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: ApifoxClientOptions = {}) {
    if (!credentials) throw new TypeError('Apifox credential source is required');
    this.timeoutMs = config.timeoutMs ?? 20_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) {
      throw new TypeError('Apifox configuration is invalid');
    }
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the project-summary Query to this client. */
  getProjectAPISummary(): GetProjectAPISummaryOperation { return new GetProjectAPISummaryOperation(this); }
}

/** Read an accessible OpenAPI export while retaining only a bounded summary. */
export class GetProjectAPISummaryOperation implements Query<GetProjectAPISummaryInput, GetProjectAPISummaryOutput> {
  constructor(readonly client: ApifoxClient) { if (!client) throw new TypeError('Apifox client is required'); }

  /** Return the generated Query identity and retry policy. */
  definition(): QueryDefinition { return GetProjectAPISummaryDefinition; }

  /** Make one provider request and classify its observed result. */
  async invoke(call: Call, input: GetProjectAPISummaryInput): Promise<QueryAttempt<GetProjectAPISummaryOutput>> {
    if (!isValidProjectID(input?.projectID)) return branch('defect', 'VALIDATION', 'Apifox project ID must be a positive decimal string');
    let token: string;
    try {
      const credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken.reveal();
      if (!isValidAccessToken(token)) throw new TypeError('invalid Apifox token');
    } catch {
      return branch('defect', 'AUTHENTICATION', 'Apifox access token is unavailable or invalid');
    }
    const request: ProviderRequest = {
      url: `${endpointRoot}${input.projectID}/export-openapi`, method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'X-Apifox-Api-Version': '2024-03-28',
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: exportBody,
      maxResponseBytes: this.client.maxResponseBytes,
      maxErrorResponseBytes: 4096,
      timeoutMs: this.client.timeoutMs,
      addressPolicy: 'publicHTTPS',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try {
      response = await this.client.send(request);
    } catch (error) {
      if (error instanceof BodyTooLargeError || error instanceof ProviderTransportError || error instanceof ProviderStalledError) {
        const status = error.status;
        const retryAfterMs = headerRetryAfter(error.headers);
        if (status === 429) return retry('RATE_LIMIT', 'Apifox rate limited the project request', retryAfterMs);
        if (status === 408 || (status !== undefined && status >= 500)) return retry('AVAILABILITY', 'Apifox project export is temporarily unavailable', retryAfterMs);
        if (status !== undefined && status !== 200) return classifyHTTPStatus(status);
        if (error instanceof BodyTooLargeError) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Apifox export exceeded the configured limit');
        return retry('AVAILABILITY', 'Apifox export was interrupted');
      }
      return retry('AVAILABILITY', 'Apifox project request failed');
    }
    const retryAfterMs = headerRetryAfter(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', 'Apifox rate limited the project request', retryAfterMs);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Apifox project export is temporarily unavailable', retryAfterMs);
    if (response.status !== 200) return classifyHTTPStatus(response.status);
    let document: unknown;
    try { document = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Apifox returned invalid OpenAPI data'); }
    const summary = decodeProjectSummary(document, token);
    if (!summary) return branch('invalidResponse', 'PROTOCOL', 'Apifox returned invalid OpenAPI data');
    return newQueryBranch('found', summary);
  }
}

function headerRetryAfter(headers: ProviderResponse['headers'] | undefined): number {
  const header = headers?.['retry-after'];
  return parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isValidProjectID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]{0,19}$/.test(value);
}

function isValidAccessToken(value: unknown): value is string {
  return typeof value === 'string' && value.length <= 1024 && isHeaderSafeCredential(value);
}

function classifyHTTPStatus(status: number): QueryAttempt<GetProjectAPISummaryOutput> {
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `Apifox rejected project export with HTTP ${status}`);
}

function decodeProjectSummary(value: unknown, token: string): GetProjectAPISummaryOutput | undefined {
  if (!isRecord(value) || typeof value.openapi !== 'string' || !value.openapi.startsWith('3.1') || !isRecord(value.info) || !isRecord(value.paths)) return undefined;
  const title = value.info.title;
  const version = value.info.version;
  if (typeof title !== 'string' || typeof version !== 'string' || !title || !version || Buffer.byteLength(title, 'utf8') > 256 || Buffer.byteLength(version, 'utf8') > 64 || /[\r\n\0]/.test(title + version) || (title + version).includes(token)) return undefined;
  let endpointCount = 0;
  for (const methods of Object.values(value.paths)) {
    if (methods !== null && !isRecord(methods)) return undefined;
    if (methods === null) continue;
    for (const method of Object.keys(methods)) if (httpMethods.has(method.toLowerCase())) endpointCount++;
  }
  return { title, version, endpointCount };
}

function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetProjectAPISummaryOutput> {
  return newQueryBranch(id, emptyOutput, {}, { provider: 'apifox', operation: 'getProjectAPISummary', kind, message });
}

function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetProjectAPISummaryOutput> {
  return newQueryRetry({ provider: 'apifox', operation: 'getProjectAPISummary', kind, message }, delayMs);
}
