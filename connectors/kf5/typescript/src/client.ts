// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTicketDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetTicketInput, GetTicketOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface KF5ClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve KF5 tenant credentials for each ticket query. */
export class KF5Client {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting KF5. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: KF5ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('KF5 configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('KF5 request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the ticket Query. */ getTicket(): GetTicketOperation { return new GetTicketOperation(this); }

  /** Send one bounded ticket request. */
  async readTicket(call: Call, ticketID: number): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credential: Credentials;
    try { credential = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const subdomain = credential.subdomain;
    const email = credential.email instanceof SecretString ? credential.email.reveal() : '';
    const token = credential.apiToken instanceof SecretString ? credential.apiToken.reveal() : '';
    if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(subdomain) || !validASCII(email, 254) || email.split('@').length !== 2 || !validASCII(token, 1024)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = {
      url: `https://${subdomain}.kf5.com/apiv2/tickets/${ticketID}.json`, method: 'GET',
      headers: { Authorization: 'Basic ' + Buffer.from(`${email}/token:${token}`).toString('base64'), Accept: 'application/json' },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
}

/** Read limited ticket status metadata without content or requester data. */
export class GetTicketOperation implements Query<GetTicketInput, GetTicketOutput> {
  /** Bind the provider client. */ constructor(private readonly client: KF5Client) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetTicketDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: GetTicketInput): Promise<QueryAttempt<GetTicketOutput>> {
    if (!input || !Number.isSafeInteger(input.ticketID) || input.ticketID <= 0) return branch('defect', 'VALIDATION');
    const exchange = await this.client.readTicket(call, input.ticketID);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status === 404) return branch('noData', 'NOT_FOUND');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const ticket = (wire as { ticket?: unknown }).ticket;
    if (!ticket || typeof ticket !== 'object' || Array.isArray(ticket)) return branch('invalidResponse', 'PROTOCOL');
    const record = ticket as Record<string, unknown>;
    if (record.id !== input.ticketID || !['new', 'open', 'pending', 'solved', 'closed'].includes(record.status as string)) return branch('invalidResponse', 'PROTOCOL');
    for (const [key, limit] of [['priority', 30], ['type', 30], ['created_at', 40], ['updated_at', 40]] as const) if (record[key] != null && (typeof record[key] !== 'string' || new TextEncoder().encode(record[key]).length > limit)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { ticketID: input.ticketID, status: record.status as string, priority: (record.priority ?? '') as string, type: (record.type ?? '') as string, createdAt: (record.created_at ?? '') as string, updatedAt: (record.updated_at ?? '') as string });
  }
}

function validASCII(value: string, limit: number): boolean { return !!value && value.length <= limit && !/[^\x21-\x7e]/.test(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'kf5', operation: 'getTicket', kind, message: 'KF5 ticket query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetTicketOutput> { return newQueryBranch(name, { ticketID: 0, status: '', priority: '', type: '', createdAt: '', updatedAt: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetTicketOutput> { return newQueryRetry(failure(kind), delay); }
