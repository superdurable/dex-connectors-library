// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetEmailIdentityDefinition } from './generated/definitions.js';
import type { Config, Credentials, EmailIdentityDetails, GetEmailIdentityInput } from './generated/models.js';

const emptyDetails = (): EmailIdentityDetails => ({ name: '', verificationStatus: '' });
const emailIdentityPattern = /^[A-Za-z0-9._+@-]{1,254}$/;
const regionPattern = /^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$/;
/** Injectable transport and clock for tests and local applications. */
export interface AmazonSESClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; omitted uses the current time. */ now?: () => Date;
}
interface Exchange { status: number; body: Uint8Array; retryAfterMs: number; error?: 'credential' | 'request' | 'tooLarge' }

/** Authenticated AWS SES client for signed GetEmailIdentity requests. */
export class AmazonSESClient {
  private readonly endpoint: string;
  private readonly region: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the Region, endpoint, and limits without contacting AWS. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AmazonSESClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('SES configuration and credentials are required');
    this.region = config.region || 'us-east-1';
    if (!regionPattern.test(this.region)) throw new TypeError('SES Region is invalid');
    this.endpoint = validateBaseURL(config.endpoint || `https://email.${this.region}.amazonaws.com`).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('SES request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the getEmailIdentity Query. */ getEmailIdentity(): GetEmailIdentityOperation { return new GetEmailIdentityOperation(this); }

  /** Sign and send one bounded GetEmailIdentity request. */
  async readIdentity(call: Call, name: string): Promise<Exchange> {
    let accessKey: string; let secretKey: string; let sessionToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKey = credentials.accessKeyId instanceof SecretString ? credentials.accessKeyId.reveal() : '';
      secretKey = credentials.secretAccessKey instanceof SecretString ? credentials.secretAccessKey.reveal() : '';
      sessionToken = credentials.sessionToken instanceof SecretString ? credentials.sessionToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!accessKey || !secretKey || !isHeaderSafeCredential(accessKey) || !isHeaderSafeCredential(secretKey) ||
      (sessionToken && !isHeaderSafeCredential(sessionToken))) return emptyExchange('credential');
    const url = `${this.endpoint}/v2/email/identities/${encodeURIComponent(name)}`;
    const now = this.now();
    const timestamp = now.toISOString().replace(/[-:]|\.\d{3}/g, '');
    const day = timestamp.slice(0, 8);
    const host = new URL(url).host;
    const signedHeaders = sessionToken ? 'host;x-amz-date;x-amz-security-token' : 'host;x-amz-date';
    const canonicalHeaders = `host:${host}\nx-amz-date:${timestamp}\n` + (sessionToken ? `x-amz-security-token:${sessionToken}\n` : '');
    const payloadHash = createHash('sha256').update('').digest('hex');
    const canonicalRequest = `GET\n/v2/email/identities/${encodeURIComponent(name)}\n\n${canonicalHeaders}\n${signedHeaders}\n${payloadHash}`;
    const scope = `${day}/${this.region}/ses/aws4_request`;
    const stringToSign = `AWS4-HMAC-SHA256\n${timestamp}\n${scope}\n${createHash('sha256').update(canonicalRequest).digest('hex')}`;
    const hmac = (key: Buffer | string, value: string) => createHmac('sha256', key).update(value).digest();
    const signingKey = hmac(hmac(hmac(hmac(`AWS4${secretKey}`, day), this.region), 'ses'), 'aws4_request');
    const signature = createHmac('sha256', signingKey).update(stringToSign).digest('hex');
    const headers: Record<string, string> = {
      'X-Amz-Date': timestamp,
      Authorization: `AWS4-HMAC-SHA256 Credential=${accessKey}/${scope}, SignedHeaders=${signedHeaders}, Signature=${signature}`,
    };
    if (sessionToken) headers['X-Amz-Security-Token'] = sessionToken;
    const request: ProviderRequest = {
      url, method: 'GET', headers, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.endpoint.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
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
    if ([accessKey, secretKey, sessionToken].some(secret => secret && Buffer.from(response.body).includes(secret)))
      return { ...emptyExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read the name and verification status of one SES identity. */
export class GetEmailIdentityOperation implements Query<GetEmailIdentityInput, EmailIdentityDetails> {
  /** Bind the provider client. */ constructor(private readonly client: AmazonSESClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetEmailIdentityDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetEmailIdentityInput): Promise<QueryAttempt<EmailIdentityDetails>> {
    if (!input || typeof input.emailIdentity !== 'string' || !emailIdentityPattern.test(input.emailIdentity))
      return terminal('defect', 'VALIDATION', 'SES email identity is invalid');
    const exchange = await this.client.readIdentity(call, input.emailIdentity);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'SES credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'SES request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'SES rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'SES is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return newQueryBranch('missing', emptyDetails());
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `SES rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'SES response exceeded the configured limit');
    let document: unknown;
    try { document = JSON.parse(Buffer.from(exchange.body).toString('utf8')) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'SES returned invalid identity JSON'); }
    if (!isObject(document) || typeof document.VerificationStatus !== 'string' ||
      !['PENDING', 'SUCCESS', 'FAILED', 'TEMPORARY_FAILURE', 'NOT_STARTED'].includes(document.VerificationStatus))
      return terminal('invalidResponse', 'PROTOCOL', 'SES returned invalid identity JSON');
    return newQueryBranch('found', { name: input.emailIdentity, verificationStatus: document.VerificationStatus });
  }
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'amazon-ses', operation: 'getEmailIdentity', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<EmailIdentityDetails> { return newQueryBranch(branch, emptyDetails(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<EmailIdentityDetails> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
