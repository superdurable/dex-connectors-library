// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Buffer } from 'node:buffer';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTransactionDefinition } from './generated/definitions.js';
import type { Config, Credentials, TransactionProfile, GetTransactionInput } from './generated/models.js';

const emptyTransaction = (): TransactionProfile => ({ id: '', status: '' });
const getTransactionGraphQL = 'query GetTransaction($id: ID!) { node(id: $id) { __typename ... on Transaction { id status } } }';
/** Injectable provider sender for tests and local applications. */
export interface BraintreeClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Braintree GraphQL client for transaction lookups. */
export class BraintreeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly apiVersion: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Braintree. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: BraintreeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Braintree configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://payments.sandbox.braintree-api.com').replace(/\/$/, '');
    this.apiVersion = config.apiVersion ?? '2019-01-01';
    if (!/^\d{4}-\d{2}-\d{2}$/.test(this.apiVersion) || !Number.isFinite(Date.parse(this.apiVersion)) || new Date(this.apiVersion).toISOString().slice(0, 10) !== this.apiVersion) throw new TypeError('Braintree API version must be YYYY-MM-DD');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Braintree request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getTransaction Query. */ getTransaction(): GetTransactionOperation { return new GetTransactionOperation(this); }

  /** Resolve credentials and send one bounded transaction lookup. */
  async get(call: Call, input: GetTransactionInput): Promise<Exchange> {
    let publicKey: string;
    let privateKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      publicKey = credentials.publicKey;
      privateKey = credentials.privateKey instanceof SecretString ? credentials.privateKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(publicKey) || publicKey.includes(':') || !isHeaderSafeCredential(privateKey)) return emptyExchange('credential');
    const authorization = `Basic ${Buffer.from(`${publicKey}:${privateKey}`, 'utf8').toString('base64')}`;
    const url = new URL(`${this.baseURL}/graphql`);
    const request: ProviderRequest = {
      url: url.toString(), method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Braintree-Version': this.apiVersion, Authorization: authorization },
      body: new TextEncoder().encode(JSON.stringify({ query: getTransactionGraphQL, variables: { id: input.transactionId } })),
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
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

/** Read one transaction by its Braintree GraphQL ID. */
export class GetTransactionOperation implements Query<GetTransactionInput, TransactionProfile> {
  /** Bind the provider client. */ constructor(private readonly client: BraintreeClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetTransactionDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: GetTransactionInput): Promise<QueryAttempt<TransactionProfile>> {
    if (!isValidGraphQLID(input?.transactionId))
      return terminal('defect', 'VALIDATION', 'transactionId must contain 1–128 printable characters');
    const exchange = await this.client.get(call, input);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Braintree credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Braintree request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Braintree rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Braintree is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Braintree rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Braintree response exceeded the configured limit');
    const transaction = decodeTransaction(exchange.body, input.transactionId);
    if (transaction === 'rejected') return terminal('providerRejected', 'PROVIDER_REJECTION', 'Braintree rejected the GraphQL query');
    if (transaction === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Braintree returned invalid transaction JSON');
    if (transaction === null) return terminal('notFound', 'NOT_FOUND', 'Braintree did not expose the transaction');
    return newQueryBranch('found', transaction);
  }
}

function isValidGraphQLID(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.trim() === value &&
    new TextEncoder().encode(value).length <= 128 && ![...value].some(character => character.codePointAt(0)! < 0x20 || character.codePointAt(0)! === 0x7f) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'braintree', operation: 'getTransaction', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<TransactionProfile> { return newQueryBranch(branch, emptyTransaction(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TransactionProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeTransaction(body: Uint8Array, transactionID: string): TransactionProfile | null | 'rejected' | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw)) return undefined;
  if (raw.errors !== undefined && raw.errors !== null && !Array.isArray(raw.errors)) return undefined;
  if (Array.isArray(raw.errors) && raw.errors.length > 0) return 'rejected';
  if (!isObject(raw.data)) return undefined;
  const node = raw.data.node;
  if (node === null) return null;
  if (!isObject(node)) return undefined;
  if (typeof node.__typename !== 'string' || !node.__typename) return undefined;
  if (node.__typename !== 'Transaction') return null;
  if (node.id !== transactionID || typeof node.status !== 'string' || !node.status) return undefined;
  return { id: node.id, status: node.status };
}
