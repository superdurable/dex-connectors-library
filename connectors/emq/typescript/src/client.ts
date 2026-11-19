// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDeploymentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, Deployment, ListDeploymentsInput, ListDeploymentsOutput } from './generated/models.js';

const emptyOutput: ListDeploymentsOutput = { deployments: [] };

/** Optional provider sender for isolated conformance replay. */
export interface EmqClientOptions {
  /** Sender; omitted uses the SDK HTTPS transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read deployments scoped by an EMQX Cloud Platform key. */
export class EmqClient {
  private readonly endpoint: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting EMQX Cloud. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: EmqClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('EMQX Cloud configuration and credentials are required');
    const selectedEndpoint = config.platformEndpoint;
    let endpoint: URL;
    try { endpoint = new URL(selectedEndpoint); } catch { throw new TypeError('EMQX Cloud Platform endpoint is invalid'); }
    if (!['https:', 'http:'].includes(endpoint.protocol) || (endpoint.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(endpoint.hostname)) || !endpoint.hostname || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || endpoint.pathname !== '/public_api/v1' || selectedEndpoint.endsWith('/')) throw new TypeError('EMQX Cloud Platform endpoint is invalid');
    this.endpoint = selectedEndpoint;
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('EMQX Cloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the scoped deployment Query. */ listDeployments(): ListDeploymentsOperation { return new ListDeploymentsOperation(this); }

  /** Send one bounded deployment-list request. */
  async readDeployments(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let key = '', secret = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      key = credentials.platformApiKey instanceof SecretString ? credentials.platformApiKey.reveal() : '';
      secret = credentials.platformApiSecret instanceof SecretString ? credentials.platformApiSecret.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validBasicCredential(key) || !validBasicCredential(secret) || key.includes(':')) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: this.endpoint + '/deployments', method: 'GET', headers: { Authorization: 'Basic ' + Buffer.from(key + ':' + secret).toString('base64'), Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** List deployments accessible to the configured Platform key. */
export class ListDeploymentsOperation implements Query<ListDeploymentsInput, ListDeploymentsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: EmqClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListDeploymentsDefinition; }
  /** Classify one deployment-list request according to the Go contract. */
  async invoke(call: Call, input: ListDeploymentsInput): Promise<QueryAttempt<ListDeploymentsOutput>> {
    void input;
    const exchange = await this.client.readDeployments(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const deployments: Deployment[] = [];
    for (const item of wire) {
      if (!isRecord(item) || !nonempty(item.deploymentID) || !nonempty(item.deploymentName) || !nonempty(item.deploymentType) || !nonempty(item.status) || !Number.isSafeInteger(item.connections) || !Number.isSafeInteger(item.tps) || (item.connections as number) < 0 || (item.tps as number) < 0) return branch('invalidResponse', 'PROTOCOL');
      deployments.push({ deploymentId: item.deploymentID as string, deploymentName: item.deploymentName as string, deploymentType: item.deploymentType as string, projectName: String(item.projectName ?? ''), region: String(item.region ?? ''), status: item.status as string, version: String(item.version ?? ''), platform: String(item.platform ?? ''), connections: item.connections as number, tps: item.tps as number, createdAt: String(item.createAt ?? '') });
    }
    return newQueryBranch('listed', { deployments }, {});
  }
}

function validBasicCredential(value: string): boolean { return !!value && value.length <= 512 && !/[^\x21-\x7e]/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function nonempty(value: unknown): boolean { return typeof value === 'string' && value.length > 0; }
function header(headers: Record<string, string | string[] | undefined>, name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'emq', operation: 'listDeployments', kind, message: 'EMQX Cloud deployment query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListDeploymentsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListDeploymentsOutput> { return newQueryRetry(failure(kind), delay); }
