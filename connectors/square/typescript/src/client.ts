// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, GetPaymentInput, ListPaymentsInput, Payment, PaymentPage } from './generated/models.js';
import { GetPaymentDefinition, ListPaymentsDefinition } from './generated/definitions.js';

const defaultBaseUrl = 'https://connect.squareup.com/v2';
const zeroPayment: Payment = { id: '', status: '', amount_money: { amount: 0, currency: '' } };
const zeroPage: PaymentPage = { payments: [], cursor: '' };

/** Sender used by Square; supply one in provider tests. */
export type SquareRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional sender and clock for deterministic tests. */
export interface SquareClientOptions {
  /** Provider transport; defaults to the SDK sender. */ send?: SquareRequestSender;
  /** Clock used for Retry-After dates. */ now?: () => Date;
}

/** Read-only Square HTTP client with immutable request configuration. */
export class Client {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: SquareRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without contacting Square. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SquareClientOptions = {}) {
    if (!credentials) throw new TypeError('Square credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('timeout and maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Query one payment page. */ listPayments(): ListPaymentsOperation { return new ListPaymentsOperation(this); }
  /** Query one payment by ID. */ getPayment(): GetPaymentOperation { return new GetPaymentOperation(this); }

  /** Send one provider read and classify its outcome. */
  async read<T>(call: Call, operation: string, path: string, successBranch: string, zero: T, decode: (body: Uint8Array) => T): Promise<QueryAttempt<T>> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.accessToken instanceof SecretString) || !isHeaderSafeCredential(credentials.accessToken.reveal())) throw new TypeError('invalid Square token');
      token = credentials.accessToken.reveal();
    } catch { return branch(operation, 'defect', zero, 'AUTHENTICATION', 'Square credentials are unavailable'); }
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseUrl + path, method: 'GET',
        headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'Square-Version': '2026-09-16' },
        maxResponseBytes: Math.max(this.maxResponseBytes, 65_536), timeoutMs: this.timeoutMs,
        addressPolicy: this.baseUrl === defaultBaseUrl ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.status !== 200) {
        return classifyStatus(operation, error.status, error.headers?.['retry-after'], zero, this.now());
      }
      if (error instanceof BodyTooLargeError) return branch(operation, 'invalidResponse', zero, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
      return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider read failed'));
    }
    if (response.status !== 200) return classifyStatus(operation, response.status, response.headers['retry-after'], zero, this.now());
    if (response.body.length > this.maxResponseBytes) return branch(operation, 'invalidResponse', zero, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
    try { return newQueryBranch(successBranch, decode(response.body)); }
    catch { return branch(operation, 'invalidResponse', zero, 'PROTOCOL', 'provider returned invalid JSON'); }
  }
}

/** Square listPayments Query. */
export class ListPaymentsOperation implements Query<ListPaymentsInput, PaymentPage> {
  /** Bind a client. */ constructor(private readonly client: Client) {}
  /** Manifest operation definition. */ definition() { return ListPaymentsDefinition; }
  /** Read one payment page. */
  invoke(call: Call, input: ListPaymentsInput): Promise<QueryAttempt<PaymentPage>> {
    const query = input.cursor ? `?cursor=${new URLSearchParams({ cursor: input.cursor }).toString().slice(7)}` : '';
    return this.client.read(call, 'listPayments', `/payments${query}`, 'listed', zeroPage, decodePage);
  }
}

/** Square getPayment Query. */
export class GetPaymentOperation implements Query<GetPaymentInput, Payment> {
  /** Bind a client. */ constructor(private readonly client: Client) {}
  /** Manifest operation definition. */ definition() { return GetPaymentDefinition; }
  /** Read one payment by valid path segment. */
  invoke(call: Call, input: GetPaymentInput): Promise<QueryAttempt<Payment>> {
    if (!input.paymentId || input.paymentId === '.' || input.paymentId === '..' || /[/?#]/.test(input.paymentId) || [...input.paymentId].some(character => character.codePointAt(0)! < 32)) {
      return Promise.resolve(branch('getPayment', 'defect', zeroPayment, 'VALIDATION', 'paymentId must be a path segment'));
    }
    const escaped = encodeURIComponent(input.paymentId).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%24|%26|%2B|%3D|%3A|%40/g, encoded => String.fromCharCode(parseInt(encoded.slice(1), 16)));
    return this.client.read(call, 'getPayment', `/payments/${escaped}`, 'found', zeroPayment, decodePaymentResponse);
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'square', operation, kind, message }; }
function branch<T>(operation: string, name: string, zero: T, kind: Failure['kind'], message: string): QueryAttempt<T> {
  return newQueryBranch(name, zero, {}, failure(operation, kind, message));
}
function classifyStatus<T>(operation: string, status: number, retryAfter: string | string[] | undefined, zero: T, now: Date): QueryAttempt<T> {
  if (status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'provider rate limited the read'), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', now));
  if (status === 408 || status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider read is temporarily unavailable'), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', now));
  if (status === 404) return branch(operation, 'notFound', zero, 'PROVIDER_REJECTION', 'resource was not found');
  if (status === 401) return branch(operation, 'providerRejected', zero, 'AUTHENTICATION', 'provider rejected credentials');
  if (status === 403) return branch(operation, 'providerRejected', zero, 'AUTHORIZATION', 'provider denied access');
  return branch(operation, 'providerRejected', zero, 'PROVIDER_REJECTION', `provider rejected read with HTTP ${status}`);
}
function object(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected object');
  return value as Record<string, unknown>;
}
function string(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError('expected string'); return value; }
function integer(value: unknown): number { if (value === undefined || value === null) return 0; if (!Number.isSafeInteger(value)) throw new TypeError('expected integer'); return value as number; }
function decodePayment(value: unknown): Payment {
  const fields = object(value); const money = fields.amount_money === undefined || fields.amount_money === null ? {} : object(fields.amount_money);
  return { id: string(fields.id), status: string(fields.status), amount_money: { amount: integer(money.amount), currency: string(money.currency) } };
}
function decodePage(body: Uint8Array): PaymentPage {
  const fields = object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
  if (fields.payments !== undefined && fields.payments !== null && !Array.isArray(fields.payments)) throw new TypeError('payments must be an array');
  return { payments: fields.payments === undefined || fields.payments === null ? [] : (fields.payments as unknown[]).map(decodePayment), cursor: string(fields.cursor) };
}
function decodePaymentResponse(body: Uint8Array): Payment {
  const fields = object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
  const payment = fields.payment === undefined || fields.payment === null ? zeroPayment : decodePayment(fields.payment);
  if (!payment.id) throw new TypeError('missing payment');
  return payment;
}
