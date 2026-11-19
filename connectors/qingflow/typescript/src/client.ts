// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListApplicationsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListApplicationsInput, ListApplicationsOutput } from './generated/models.js';

const applicationsEndpoint = 'https://api.qingflow.com/app';
const emptyOutput: ListApplicationsOutput = { applications: [] };

/** Optional request sender for provider tests. */
export interface QingflowClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve a Qingflow super-admin token for each application-list attempt. */
export class QingflowClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate settings without contacting Qingflow. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: QingflowClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Qingflow configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Qingflow request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the application-list Query. */
  listApplications(): ListApplicationsOperation { return new ListApplicationsOperation(this); }

  /** Request bounded application metadata with a freshly resolved token. */
  async fetchApplications(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (!token || token.length > 1024 || !/^[!-~]+$/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    let response: ProviderResponse;
    try { response = await this.send({ url: applicationsEndpoint, method: 'GET', headers: { accessToken: token, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** List application keys and names without reading records. */
export class ListApplicationsOperation implements Query<ListApplicationsInput, ListApplicationsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: QingflowClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListApplicationsDefinition; }
  /** Classify one Qingflow response. */
  async invoke(call: Call, input: ListApplicationsInput): Promise<QueryAttempt<ListApplicationsOutput>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchApplications(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.errCode)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.errCode !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(wire.result) || !Array.isArray(wire.result.appList)) return branch('invalidResponse', 'PROTOCOL');
    const applications: ListApplicationsOutput['applications'] = [];
    for (const application of wire.result.appList) {
      if (!isRecord(application) || typeof application.appKey !== 'string' || !application.appKey || typeof application.appName !== 'string' || !application.appName) return branch('invalidResponse', 'PROTOCOL');
      applications.push({ appKey: application.appKey, appName: application.appName });
    }
    return newQueryBranch('listed', { applications });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'qingflow', operation: 'listApplications', kind, message: 'Qingflow application-list query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListApplicationsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<ListApplicationsOutput> { return newQueryRetry(failure(kind), delayMs); }
