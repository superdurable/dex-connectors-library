// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupAppIDDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupAppIDInput, LookupAppIDOutput } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface HonorClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
const emptyApp: LookupAppIDOutput = { packageName: '', appID: 0 };

/** Resolve Honor credentials for each owned-app lookup. */
export class HonorClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Honor. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: HonorClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Honor configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Honor request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only app ID Query. */ lookupAppID(): LookupAppIDOperation { return new LookupAppIDOperation(this); }
  /** Resolve short-lived token credentials for this call. */
  async resolveCredentials(call: Call): Promise<{ clientID: string; clientSecret: string } | undefined> {
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      const clientID = credential.clientID instanceof SecretString ? credential.clientID.reveal() : '';
      const clientSecret = credential.clientSecret instanceof SecretString ? credential.clientSecret.reveal() : '';
      if (!isValidCredential(clientID) || !isValidCredential(clientSecret)) return undefined;
      return { clientID, clientSecret };
    } catch { return undefined; }
  }
  /** Send one bounded token or app request. */
  async request(call: Call, url: string, method: 'GET' | 'POST', headers: Record<string, string>, body?: Uint8Array): Promise<Exchange> {
    const request: ProviderRequest = { url, method, headers, ...(body ? { body } : {}), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Look up an owned app ID by Android package name. */
export class LookupAppIDOperation implements Query<LookupAppIDInput, LookupAppIDOutput> {
  /** Bind the provider client. */ constructor(private readonly client: HonorClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return LookupAppIDDefinition; }
  /** Exchange credentials, then query exactly one app package. */
  async invoke(call: Call, input: LookupAppIDInput): Promise<QueryAttempt<LookupAppIDOutput>> {
    const packageName = input?.packageName;
    if (typeof packageName !== 'string' || packageName.length > 255 || !/^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$/.test(packageName)) return branch('defect', 'VALIDATION');
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const form = new URLSearchParams({ grant_type: 'client_credentials', client_id: credentials.clientID, client_secret: credentials.clientSecret });
    const tokenExchange = await this.client.request(call, 'https://iam.developer.honor.com/auth/token', 'POST', { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, Buffer.from(form.toString()));
    const tokenFailure = classifyExchange(tokenExchange);
    if (tokenFailure) return tokenFailure;
    const tokenWire = parseObject(tokenExchange.response!.body);
    if (!tokenWire || !isValidCredential(tokenWire.access_token) || !Number.isInteger(tokenWire.expires_in) || (tokenWire.expires_in as number) <= 0 || tokenWire.token_type !== 'Bearer') return branch('invalidResponse', 'PROTOCOL');
    const url = new URL('https://appmarket-openapi-drcn.cloud.honor.com/openapi/v1/publish/get-app-id');
    url.searchParams.set('pkgName', packageName);
    const appExchange = await this.client.request(call, url.toString(), 'GET', { Accept: 'application/json', Authorization: 'Bearer ' + tokenWire.access_token });
    const appFailure = classifyExchange(appExchange);
    if (appFailure) return appFailure;
    const appWire = parseObject(appExchange.response!.body);
    if (!appWire || !Number.isInteger(appWire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (appWire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(appWire.data)) return branch('invalidResponse', 'PROTOCOL');
    if (appWire.data.length === 0) return newQueryBranch('notFound', { packageName, appID: 0 });
    if (appWire.data.length !== 1 || !appWire.data[0] || typeof appWire.data[0] !== 'object' || Array.isArray(appWire.data[0])) return branch('invalidResponse', 'PROTOCOL');
    const app = appWire.data[0] as Record<string, unknown>;
    if (app.packageName !== packageName || !Number.isSafeInteger(app.appId) || (app.appId as number) <= 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { packageName, appID: app.appId as number });
  }
}

function isValidCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 1024 && !/[^\x21-\x7e]/.test(value); }
function parseObject(body: Uint8Array): Record<string, unknown> | undefined {
  try { const parsed: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)); return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : undefined; }
  catch { return undefined; }
}
function classifyExchange(exchange: Exchange): QueryAttempt<LookupAppIDOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const status = exchange.response.status;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  return undefined;
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'honor', operation: 'lookupAppID', kind, message: 'Honor app ID lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupAppIDOutput> { return newQueryBranch(name, emptyApp, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupAppIDOutput> { return newQueryRetry(failure(kind), delay); }
