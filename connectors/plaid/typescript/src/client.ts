// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountsDefinition } from './generated/definitions.js';
import type { AccountSummary, Config, Credentials, AccountsSnapshot, GetAccountsInput } from './generated/models.js';

const defaultBaseURL = 'https://sandbox.plaid.com';
const emptyAccounts = (): AccountsSnapshot => ({ itemId: '', accounts: [] });

/** Injectable provider sender for tests and local applications. */
export interface PlaidClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Plaid REST API client for accounts metadata. */
export class PlaidClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Plaid. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PlaidClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Plaid configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Plaid request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getAccounts Query. */ getAccounts(): GetAccountsOperation { return new GetAccountsOperation(this); }

  /** Resolve Item credentials and send one bounded account lookup. */
  async sendAccountsRequest(call: Call): Promise<Exchange> {
    let clientID: string;
    let secret: string;
    let accessToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      clientID = credentials.clientId instanceof SecretString ? credentials.clientId.reveal() : '';
      secret = credentials.secret instanceof SecretString ? credentials.secret.reveal() : '';
      accessToken = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(clientID) || !isHeaderSafeCredential(secret) || !accessToken) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/accounts/get`, method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'PLAID-CLIENT-ID': clientID, 'PLAID-SECRET': secret },
      body: Buffer.from(JSON.stringify({ access_token: accessToken })),
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

/** Read account IDs, types, and subtypes for one linked Item. */
export class GetAccountsOperation implements Query<GetAccountsInput, AccountsSnapshot> {
  /** Bind the provider client. */ constructor(private readonly client: PlaidClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetAccountsDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetAccountsInput): Promise<QueryAttempt<AccountsSnapshot>> {
    if (!isValidItemID(input?.itemId))
      return terminal('defect', 'VALIDATION', 'Item ID must be 1–128 URL-safe characters');
    const exchange = await this.client.sendAccountsRequest(call);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Plaid credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Plaid request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Plaid rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Plaid is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404 || (!exchange.error && isItemNotFound(exchange.body))) return terminal('notFound', 'NOT_FOUND', 'Plaid did not expose the Item');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Plaid rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Plaid response exceeded the configured limit');
    const accountsStatus = decodeAccounts(exchange.body, input.itemId);
    if (!accountsStatus) return terminal('invalidResponse', 'PROTOCOL', 'Plaid returned invalid accounts JSON');
    return newQueryBranch('found', accountsStatus);
  }
}

function isValidItemID(value: unknown): value is string {
  return typeof value === 'string' && /^[a-zA-Z0-9_-]{1,128}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'plaid', operation: 'getAccounts', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<AccountsSnapshot> { return newQueryBranch(branch, emptyAccounts(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<AccountsSnapshot> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
/** Report Plaid's ITEM_NOT_FOUND error, sent with HTTP 400 for a removed or unknown Item. */
function isItemNotFound(body: Uint8Array): boolean {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return false; }
  return isObject(raw) && raw.error_type === 'ITEM_ERROR' && raw.error_code === 'ITEM_NOT_FOUND';
}
function decodeAccounts(body: Uint8Array, itemID: string): AccountsSnapshot | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.item) || raw.item.item_id !== itemID || !Array.isArray(raw.accounts)) return undefined;
  const accounts: AccountSummary[] = [];
  for (const account of raw.accounts) {
    if (!isObject(account) || typeof account.account_id !== 'string' || !account.account_id.trim() ||
      typeof account.type !== 'string' || !account.type.trim() ||
      (account.subtype != null && typeof account.subtype !== 'string')) return undefined;
    accounts.push({ id: account.account_id, type: account.type, subtype: account.subtype ?? '' });
  }
  return { itemId: itemID, accounts };
}
