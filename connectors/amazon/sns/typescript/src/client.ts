// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { XMLParser, XMLValidator } from 'fast-xml-parser';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTopicsDefinition } from './generated/definitions.js';
import type { Config, Credentials, TopicPage, ListTopicsInput } from './generated/models.js';

const emptyPage = (): TopicPage => ({ topicARNs: [], nextToken: '' });
const nextTokenPattern = /^[A-Za-z0-9+/=._-]*$/;
const regionPattern = /^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$/;
/** Injectable transport and clock for tests and local applications. */
export interface AmazonSNSClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; omitted uses the current time. */ now?: () => Date;
}
interface Exchange { status: number; body: Uint8Array; retryAfterMs: number; error?: 'credential' | 'request' | 'tooLarge' }

/** Authenticated AWS SNS client for signed ListTopics requests. */
export class AmazonSNSClient {
  private readonly endpoint: string;
  private readonly region: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the Region, endpoint, and limits without contacting AWS. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AmazonSNSClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('SNS configuration and credentials are required');
    this.region = config.region || 'us-east-1';
    if (!regionPattern.test(this.region)) throw new TypeError('SNS Region is invalid');
    this.endpoint = validateBaseURL(config.endpoint || `https://sns.${this.region}.amazonaws.com`).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('SNS request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the listTopics Query. */ listTopics(): ListTopicsOperation { return new ListTopicsOperation(this); }

  /** Sign and send one bounded ListTopics request. */
  async readTopicPage(call: Call, nextToken: string): Promise<Exchange> {
    let accessKey: string; let secretKey: string; let sessionToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKey = credentials.accessKeyId instanceof SecretString ? credentials.accessKeyId.reveal() : '';
      secretKey = credentials.secretAccessKey instanceof SecretString ? credentials.secretAccessKey.reveal() : '';
      sessionToken = credentials.sessionToken instanceof SecretString ? credentials.sessionToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!accessKey || !secretKey || !isHeaderSafeCredential(accessKey) || !isHeaderSafeCredential(secretKey) ||
      (sessionToken && !isHeaderSafeCredential(sessionToken))) return emptyExchange('credential');
    const query = new URLSearchParams({ Action: 'ListTopics', Version: '2010-03-31' });
    if (nextToken) query.set('NextToken', nextToken);
    query.sort();
    const url = `${this.endpoint}/?${query}`;
    const now = this.now();
    const timestamp = now.toISOString().replace(/[-:]|\.\d{3}/g, '');
    const day = timestamp.slice(0, 8);
    const host = new URL(url).host;
    const signedHeaders = sessionToken ? 'host;x-amz-date;x-amz-security-token' : 'host;x-amz-date';
    const canonicalHeaders = `host:${host}\nx-amz-date:${timestamp}\n` + (sessionToken ? `x-amz-security-token:${sessionToken}\n` : '');
    const payloadHash = createHash('sha256').update('').digest('hex');
    const canonicalRequest = `GET\n/\n${query}\n${canonicalHeaders}\n${signedHeaders}\n${payloadHash}`;
    const scope = `${day}/${this.region}/sns/aws4_request`;
    const stringToSign = `AWS4-HMAC-SHA256\n${timestamp}\n${scope}\n${createHash('sha256').update(canonicalRequest).digest('hex')}`;
    const hmac = (key: Buffer | string, value: string) => createHmac('sha256', key).update(value).digest();
    const signingKey = hmac(hmac(hmac(hmac(`AWS4${secretKey}`, day), this.region), 'sns'), 'aws4_request');
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

/** Read one page of SNS topic ARNs. */
export class ListTopicsOperation implements Query<ListTopicsInput, TopicPage> {
  /** Bind the provider client. */ constructor(private readonly client: AmazonSNSClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListTopicsDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: ListTopicsInput): Promise<QueryAttempt<TopicPage>> {
    if (!input || typeof input.nextToken !== 'string' || input.nextToken.length > 4096 || !nextTokenPattern.test(input.nextToken))
      return terminal('defect', 'VALIDATION', 'SNS pagination token is invalid');
    const exchange = await this.client.readTopicPage(call, input.nextToken);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'SNS credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'SNS request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'SNS rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'SNS is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `SNS rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'SNS response exceeded the configured limit');
    const xml = Buffer.from(exchange.body).toString('utf8');
    if (XMLValidator.validate(xml) !== true) return terminal('invalidResponse', 'PROTOCOL', 'SNS returned invalid topic XML');
    let document: unknown;
    try { document = new XMLParser({ ignoreAttributes: false, parseTagValue: false, processEntities: false }).parse(xml) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'SNS returned invalid topic XML'); }
    if (!isObject(document) || !isObject(document.ListTopicsResponse) ||
      !['https://sns.amazonaws.com/doc/2010-03-31/', 'http://sns.amazonaws.com/doc/2010-03-31/'].includes(String(document.ListTopicsResponse['@_xmlns'])) ||
      !isObject(document.ListTopicsResponse.ListTopicsResult))
      return terminal('invalidResponse', 'PROTOCOL', 'SNS returned invalid topic XML');
    const result = document.ListTopicsResponse.ListTopicsResult;
    const topics = isObject(result.Topics) ? result.Topics.member : undefined;
    const members = topics === undefined ? [] : Array.isArray(topics) ? topics : [topics];
    const arns = members.map(member => isObject(member) ? member.TopicArn : undefined);
    const next = result.NextToken === undefined ? '' : result.NextToken;
    if (arns.length > 100 || arns.some(arn => typeof arn !== 'string' || !arn) || typeof next !== 'string' || next.length > 4096 || !nextTokenPattern.test(next))
      return terminal('invalidResponse', 'PROTOCOL', 'SNS returned invalid topic XML');
    return newQueryBranch('page', { topicARNs: arns as string[], nextToken: next });
  }
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'amazon-sns', operation: 'listTopics', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<TopicPage> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TopicPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
