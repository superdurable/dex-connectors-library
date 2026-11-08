// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListClientsDefinition } from './generated/definitions.js';
import type { ClientMetadata, ClientsList, Config, Credentials, ListClientsInput } from './generated/models.js';

const emptyClients = (): ClientsList => ({ clients: [] });

/** Optional transport dependency for provider tests and local Dex examples. */
export interface CampaignMonitorClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Campaign Monitor API v3.3 client with call-time API key resolution. */
export class CampaignMonitorClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate account configuration without contacting Campaign Monitor. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CampaignMonitorClientOptions = {}) {
    if (!credentials) throw new TypeError('Campaign Monitor credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.createsend.com/api/v3.3');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Campaign Monitor timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Campaign Monitor response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the client discovery Query. */ listClients(): ListClientsOperation { return new ListClientsOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call): Promise<ProviderOutcome> {
    let apiKey: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : '';
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}/clients.json`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:`, 'utf8').toString('base64')}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Campaign Monitor response exceeded the configured limit') };
      } else return { retry: true, failure: failure('TRANSPORT', 'Campaign Monitor request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure('RATE_LIMIT', 'Campaign Monitor rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure('AVAILABILITY', 'Campaign Monitor is temporarily unavailable') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure('PROVIDER_REJECTION', `Campaign Monitor rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Campaign Monitor response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Read client IDs and names without provider credentials or billing fields. */
export class ListClientsOperation implements Query<ListClientsInput, ClientsList> {
  /** Bind the provider client. */ constructor(private readonly client: CampaignMonitorClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return ListClientsDefinition; }
  /** Read the account's accessible clients. */
  async invoke(call: Call, input: ListClientsInput): Promise<QueryAttempt<ClientsList>> {
    if (!input || typeof input !== 'object') return newQueryBranch('defect', emptyClients(), {}, failure('VALIDATION', 'client list input is invalid'));
    const result = await this.client.exchange(call);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyClients(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!Array.isArray(document)) return invalid();
    const clients: ClientMetadata[] = [];
    for (const item of document) {
      if (!isObject(item) || typeof item.ClientID !== 'string' || !item.ClientID || typeof item.Name !== 'string' || !item.Name) return invalid();
      clients.push({ id: item.ClientID, name: item.Name });
    }
    return newQueryBranch('listed', { clients });
  }
}

function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'campaignmonitor', operation: 'listClients', kind, message }; }
function invalid(): QueryAttempt<ClientsList> { return newQueryBranch('invalidResponse', emptyClients(), {}, failure('PROTOCOL', 'Campaign Monitor returned invalid JSON')); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
