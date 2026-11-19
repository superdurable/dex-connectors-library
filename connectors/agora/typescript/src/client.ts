// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListRunningAgentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListRunningAgentsInput, RunningAgent, RunningAgents } from './generated/models.js';

const emptyOutput: RunningAgents = { count: 0, agents: [], total: 0, cursor: '' };

/** Send one Agora request through a caller-owned transport. */
export type AgoraRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for deterministic provider tests. */
export interface AgoraClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: AgoraRequestSender;
}

/** Resolve Agora credentials for each read-only agent listing. */
export class AgoraClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: AgoraRequestSender;

  /** Validate settings without contacting Agora. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: AgoraClientOptions = {}) {
    if (!credentials) throw new TypeError('Agora credential source is required');
    const baseUrl = validateBaseURL(config.baseUrl || 'https://api.agora.io');
    const parsed = new URL(baseUrl);
    if (parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('Agora API URL must be a host root');
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Agora timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the one-page running agent Query. */
  listRunningAgents(): ListRunningAgentsOperation { return new ListRunningAgentsOperation(this); }
}

/** Read one page of running Agora Conversational AI agents. */
export class ListRunningAgentsOperation implements Query<ListRunningAgentsInput, RunningAgents> {
  /** Bind an authenticated Agora client. */
  constructor(private readonly client: AgoraClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListRunningAgentsDefinition; }

  /** Send one provider request and classify its result. */
  async invoke(call: Call, input: ListRunningAgentsInput): Promise<QueryAttempt<RunningAgents>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'Agora query input is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Agora credentials are unavailable or invalid'); }
    const appId = credentials.appId;
    const customerId = credentials.customerId;
    const secret = credentials.customerSecret instanceof SecretString ? credentials.customerSecret.reveal() : '';
    if (!/^[0-9a-fA-F]{32}$/.test(appId) || !/^[!-~]{1,128}$/.test(customerId) || customerId.includes(':') || !secret) {
      return branch('defect', 'AUTHENTICATION', 'Agora credentials are unavailable or invalid');
    }
    const authorization = `Basic ${Buffer.from(`${customerId}:${secret}`, 'utf8').toString('base64')}`;
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: `${this.client.baseUrl}/api/conversational-ai-agent/v2/projects/${appId}/agents`,
        method: 'GET', headers: { Accept: 'application/json', Authorization: authorization },
        maxResponseBytes: this.client.maxResponseBytes, timeoutMs: this.client.timeoutMs,
        addressPolicy: this.client.baseUrl === 'https://api.agora.io' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status !== 429 && response.status !== 408 && response.status < 500) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Agora response exceeded the configured limit');
      } else return retry('TRANSPORT', 'Agora request failed');
    }
    const retryAfterHeader = response.headers['retry-after'];
    const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', 'Agora rate limited the request', retryAfterMs);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Agora is temporarily unavailable', retryAfterMs);
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Agora rejected HTTP ${response.status}`);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Agora returned invalid agents JSON'); }
    if (!isRecord(document) || document.status !== 'ok' || !isRecord(document.data) || !isRecord(document.meta)) return branch('invalidResponse', 'PROTOCOL', 'Agora returned inconsistent agent page');
    const page = document.data;
    const metadata = document.meta;
    const count = page.count;
    const rows = page.list;
    const total = metadata.total;
    const cursor = metadata.cursor ?? '';
    if (!Number.isSafeInteger(count) || !Array.isArray(rows) || (count as number) < 0 || (count as number) > 20 || count !== rows.length || !Number.isSafeInteger(total) || (total as number) < (count as number) || typeof cursor !== 'string') {
      return branch('invalidResponse', 'PROTOCOL', 'Agora returned inconsistent agent page');
    }
    const agents: RunningAgent[] = [];
    for (const row of rows) {
      if (!isRecord(row) || typeof row.agent_id !== 'string' || !row.agent_id || row.status !== 'RUNNING' || !Number.isSafeInteger(row.start_ts) || (row.start_ts as number) <= 0) {
        return branch('invalidResponse', 'PROTOCOL', 'Agora returned invalid running agent');
      }
      agents.push({ agentId: row.agent_id, status: 'RUNNING', startTimestamp: row.start_ts as number });
    }
    return newQueryBranch('listed', { count: count as number, agents, total: total as number, cursor });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'agora', operation: 'listRunningAgents', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<RunningAgents> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<RunningAgents> { return newQueryRetry(failure(kind, message), delayMs); }
