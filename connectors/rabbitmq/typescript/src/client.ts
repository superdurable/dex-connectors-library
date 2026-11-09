// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetQueueMetricsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetQueueMetricsInput, QueueMetrics } from './generated/models.js';

const emptyMetrics = (): QueueMetrics => ({ virtualHost: '', name: '', messages: 0, consumers: 0, state: '' });

/** Injectable provider sender for tests and local applications. */
export interface RabbitMQClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated RabbitMQ Management API client for queue inspection. */
export class RabbitMQClient {
  private readonly managementURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate Management API settings without contacting RabbitMQ. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: RabbitMQClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('RabbitMQ configuration and credentials are required');
    this.managementURL = validateBaseURL(config.managementUrl).replace(/\/$/, '');
    if (new URL(this.managementURL).pathname !== '/') throw new TypeError('RabbitMQ Management API URL must be an origin without a path');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('RabbitMQ request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getQueueMetrics Query. */ getQueueMetrics(): GetQueueMetricsOperation { return new GetQueueMetricsOperation(this); }

  /** Resolve Basic Auth credentials and send one bounded queue lookup. */
  async get(call: Call, virtualHost: string, name: string): Promise<Exchange> {
    let username: string;
    let password: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      username = credentials.username;
      password = credentials.password instanceof SecretString ? credentials.password.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(username) || username.includes(':') || !isHeaderSafeCredential(password)) return emptyExchange('credential');
    const authorization = `Basic ${Buffer.from(`${username}:${password}`, 'ascii').toString('base64')}`;
    const request: ProviderRequest = {
      url: `${this.managementURL}/api/queues/${encodeURIComponent(virtualHost)}/${encodeURIComponent(name)}`,
      method: 'GET', headers: { Accept: 'application/json', Authorization: authorization },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.managementURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read current counts for one RabbitMQ queue. */
export class GetQueueMetricsOperation implements Query<GetQueueMetricsInput, QueueMetrics> {
  /** Bind one client. */ constructor(private readonly client: RabbitMQClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetQueueMetricsDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: GetQueueMetricsInput): Promise<QueryAttempt<QueueMetrics>> {
    if (!isValidQueueIdentity(input?.virtualHost) || !isValidQueueIdentity(input?.name))
      return terminal('defect', 'VALIDATION', 'virtual host or queue name is invalid');
    const exchange = await this.client.get(call, input.virtualHost, input.name);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'RabbitMQ credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'RabbitMQ request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'RabbitMQ rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'RabbitMQ is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'PROVIDER_REJECTION', 'RabbitMQ queue or virtual host was not found');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `RabbitMQ rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'RabbitMQ response exceeded the configured limit');
    const metrics = decodeQueue(exchange.body, input);
    if (metrics === 'statisticsPending') return retry('AVAILABILITY', 'RabbitMQ has not collected queue statistics yet');
    if (!metrics) return terminal('invalidResponse', 'PROTOCOL', 'RabbitMQ returned invalid queue metrics');
    return newQueryBranch('found', metrics);
  }
}

function isValidQueueIdentity(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 &&
    new TextEncoder().encode(value).length <= 255 && !/\p{Cc}/u.test(value) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'rabbitmq', operation: 'getQueueMetrics', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<QueueMetrics> { return newQueryBranch(branch, emptyMetrics(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<QueueMetrics> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
/** Decode queue metrics; a freshly declared queue may omit its counters until RabbitMQ emits them. */
function decodeQueue(body: Uint8Array, input: GetQueueMetricsInput): QueueMetrics | 'statisticsPending' | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.vhost !== input.virtualHost || raw.name !== input.name) return undefined;
  if (raw.messages === undefined || raw.messages === null || raw.consumers === undefined || raw.consumers === null ||
    raw.state === undefined || raw.state === null) return 'statisticsPending';
  if (!Number.isSafeInteger(raw.messages) || !Number.isSafeInteger(raw.consumers) ||
    (raw.messages as number) < 0 || (raw.consumers as number) < 0 || typeof raw.state !== 'string' || !raw.state) return undefined;
  return { virtualHost: input.virtualHost, name: input.name, messages: raw.messages as number, consumers: raw.consumers as number, state: raw.state };
}
