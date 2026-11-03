// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { type Call, type CredentialProvider, type Failure, type Receipt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { type Config, type Credentials } from './generated/models.js';
import { ChangeMessageVisibilityOperation, DeleteMessageOperation, ReceiveMessagesOperation } from './operations.js';

const provider = 'amazon-sqs';
const regionPattern = /^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/;
const queueNamePattern = /^[A-Za-z0-9_-]{1,80}$/;
const ownerPattern = /^[A-Za-z0-9_-]{1,64}$/;
const requestIdPattern = /^[A-Za-z0-9+/=._-]{1,128}$/;
const errorCodePattern = /^[A-Za-z0-9.]{1,96}$/;
const systemAttributes = ['ApproximateReceiveCount', 'ApproximateFirstReceiveTimestamp', 'SentTimestamp', 'MessageGroupId', 'MessageDeduplicationId', 'SequenceNumber'];

/** Validated queue identity used for SQS requests and receipts. */
export interface QueueAddress {
  /** Full queue URL sent in the SQS JSON request. */ url: string;
  /** Whether the queue name ends in .fifo. */ isFIFO: boolean;
}
/** One bounded provider reply. */
export interface SQSResponse {
  /** HTTP response status. */ status: number;
  /** Provider response headers. */ headers: ProviderResponse['headers'];
  /** Complete retained response body, or empty when oversized. */ body: Uint8Array;
  /** Whether the response exceeded the configured receive limit. */ isTooLarge: boolean;
}
/** Injectable request sender for deterministic provider tests. */
export type SQSRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Constructor dependencies for deterministic tests. */
export interface SQSClientOptions {
  /** Substitute an HTTP sender for provider tests. */ send?: SQSRequestSender;
  /** Supply a deterministic signing and receipt clock. */ now?: () => Date;
}

/** Immutable SQS JSON-protocol client that resolves private credentials per call. */
export class SQSClient {
  /** Validated SQS JSON-protocol endpoint. */ readonly endpoint: string;
  /** Region used in every SigV4 credential scope. */ readonly region: string;
  /** Queue selected when an operation leaves queueUrl blank. */ readonly defaultQueueUrl: string;
  /** Default receive batch limit, from one through ten. */ readonly maxMessagesPerReceive: number;
  /** Default receive long-poll duration in seconds. */ readonly receiveWaitSeconds: number;
  /** Complete response bound in bytes. */ readonly maxResponseBytes: number;
  /** Whether the endpoint was derived from the Region. */ readonly isAmazonEndpoint: boolean;
  /** Clock used for signing and provider receipts. */ readonly now: () => Date;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: SQSRequestSender;

  /** Validate local settings and retain host-owned credentials. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: SQSClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    this.region = config.region || 'us-east-1';
    if (!regionPattern.test(this.region)) throw new TypeError('SQS region must be a Region code such as eu-west-1');
    this.isAmazonEndpoint = !config.endpoint;
    const suffix = this.region.startsWith('cn-') ? 'amazonaws.com.cn' : this.region.startsWith('eusc-de-') ? 'amazonaws.eu' : 'amazonaws.com';
    if (this.isAmazonEndpoint && !/^(?:(?:us|eu|ap|sa|ca|me|af|il|mx)-[a-z0-9-]+-[0-9]+|us-gov-[a-z0-9-]+-[0-9]+|cn-[a-z0-9-]+-[0-9]+|eusc-de-[a-z0-9-]+-[0-9]+)$/.test(this.region)) throw new TypeError('SQS region is not an Amazon SQS Region; set endpoint for an SQS-compatible server');
    const endpoint = new URL(config.endpoint || `https://sqs.${this.region}.${suffix}`);
    if (!endpoint.hostname || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || !/^\/(?:[A-Za-z0-9._~-]+\/?)*$/.test(endpoint.pathname)) throw new TypeError('SQS endpoint is invalid');
    if (endpoint.protocol !== 'https:' && !(endpoint.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(endpoint.hostname))) throw new TypeError('SQS endpoint must use HTTPS or loopback HTTP');
    this.endpoint = endpoint.href;
    this.defaultQueueUrl = config.defaultQueueUrl || '';
    if (this.defaultQueueUrl) this.resolveQueue(this.defaultQueueUrl);
    this.maxMessagesPerReceive = config.maxMessagesPerReceive || 10;
    this.receiveWaitSeconds = config.receiveWaitSeconds || 20;
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (!Number.isSafeInteger(this.maxMessagesPerReceive) || this.maxMessagesPerReceive < 1 || this.maxMessagesPerReceive > 10) throw new TypeError('SQS maxMessagesPerReceive must be from 1 to 10');
    if (!Number.isSafeInteger(this.receiveWaitSeconds) || this.receiveWaitSeconds < 1 || this.receiveWaitSeconds > 20) throw new TypeError('SQS receiveWaitSeconds must be from 1 to 20');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('SQS maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the receive mutation. */ receiveMessages(): ReceiveMessagesOperation { return new ReceiveMessagesOperation(this); }
  /** Bind the delete mutation. */ deleteMessage(): DeleteMessageOperation { return new DeleteMessageOperation(this); }
  /** Bind the visibility mutation. */ changeMessageVisibility(): ChangeMessageVisibilityOperation { return new ChangeMessageVisibilityOperation(this); }

  /** Select the input queue or the connection default and validate its identity. */
  resolveQueue(inputQueueUrl: string | undefined): QueueAddress {
    const queueUrl = inputQueueUrl || this.defaultQueueUrl;
    if (!queueUrl) throw new TypeError('queueUrl is required because the connection has no defaultQueueUrl');
    if (Buffer.byteLength(queueUrl) > 2048) throw new TypeError('queueUrl must be a queue URL');
    let parsed: URL;
    try { parsed = new URL(queueUrl); } catch { throw new TypeError('queueUrl must be a queue URL'); }
    if (!['https:', 'http:'].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.href !== queueUrl) throw new TypeError('queueUrl must be a queue URL');
    const match = /^\/([^/]+)\/([^/]+)$/.exec(parsed.pathname);
    if (!match || !ownerPattern.test(match[1] ?? '')) throw new TypeError('queueUrl must be a queue URL');
    const name = match[2] ?? '';
    const isFIFO = name.endsWith('.fifo');
    if (!queueNamePattern.test(isFIFO ? name.slice(0, -5) : name) || name.length > 80) throw new TypeError('queueUrl must end with a queue name of 1 to 80 letters, digits, hyphens, and underscores, including any .fifo suffix');
    const awsRegion = /^sqs\.([a-z0-9-]+)\.amazonaws\.(?:com|com\.cn|eu)$/.exec(parsed.hostname)?.[1];
    if (this.isAmazonEndpoint && awsRegion && awsRegion !== this.region) throw new TypeError(`queueUrl is in Region ${awsRegion} but the connection signs for ${this.region}; set region to ${awsRegion}`);
    return { url: queueUrl, isFIFO };
  }

  /** Resolve one current access key without retaining it in a result. */
  async resolveCredentials(call: Call): Promise<Credentials | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const key = credentials.accessKeyId?.reveal();
      const secret = credentials.secretAccessKey?.reveal();
      const token = credentials.sessionToken?.reveal() ?? '';
      if (!key || !secret || /[^\x21-\x7e]/.test(key + secret + token)) return undefined;
      return credentials;
    } catch { return undefined; }
  }

  /** Sign and send one JSON-protocol action with AWS Signature Version 4. */
  async exchange(call: Call, credentials: Credentials, action: string, requestBody: object): Promise<SQSResponse> {
    const body = Buffer.from(JSON.stringify(requestBody));
    const url = new URL(this.endpoint);
    const now = this.now();
    const timestamp = now.toISOString().replace(/[-:]|\.\d{3}/g, '');
    const day = timestamp.slice(0, 8);
    const payloadHash = createHash('sha256').update(body).digest('hex');
    const host = url.host;
    const canonicalUri = url.pathname;
    const headersForSignature: Record<string, string> = {
      'content-length': String(body.length),
      'content-type': 'application/x-amz-json-1.0',
      host,
      'x-amz-date': timestamp,
      'x-amz-target': `AmazonSQS.${action}`,
    };
    const token = credentials.sessionToken?.reveal();
    if (token) headersForSignature['x-amz-security-token'] = token;
    const signedHeaders = Object.keys(headersForSignature).sort().join(';');
    const canonicalHeaders = Object.entries(headersForSignature).sort(([left], [right]) => left.localeCompare(right)).map(([name, value]) => `${name}:${value.trim()}\n`).join('');
    const canonicalRequest = `POST\n${canonicalUri}\n\n${canonicalHeaders}\n${signedHeaders}\n${payloadHash}`;
    const scope = `${day}/${this.region}/sqs/aws4_request`;
    const stringToSign = `AWS4-HMAC-SHA256\n${timestamp}\n${scope}\n${createHash('sha256').update(canonicalRequest).digest('hex')}`;
    const hmac = (key: Buffer | string, value: string) => createHmac('sha256', key).update(value).digest();
    const signingKey = hmac(hmac(hmac(hmac(`AWS4${credentials.secretAccessKey.reveal()}`, day), this.region), 'sqs'), 'aws4_request');
    const signature = createHmac('sha256', signingKey).update(stringToSign).digest('hex');
    const headers: Record<string, string> = {
      'Content-Type': headersForSignature['content-type']!,
      'X-Amz-Date': timestamp,
      'X-Amz-Target': headersForSignature['x-amz-target']!,
      Authorization: `AWS4-HMAC-SHA256 Credential=${credentials.accessKeyId.reveal()}/${scope}, SignedHeaders=${signedHeaders}, Signature=${signature}`,
    };
    if (token) headers['X-Amz-Security-Token'] = token;
    const remaining = call.context.deadline ? call.context.deadline.getTime() - Date.now() : 25_000;
    if (remaining <= 0) throw new TypeError('Dex Step attempt deadline has expired');
    const request: ProviderRequest = { url: this.endpoint, method: 'POST', headers, body,
      maxResponseBytes: Math.max(this.maxResponseBytes, 65_536), timeoutMs: Math.min(25_000, remaining),
      addressPolicy: this.isAmazonEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    try {
      const response = await this.send(request);
      return { ...response, isTooLarge: response.body.length > this.maxResponseBytes };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
        return { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true };
      }
      throw error;
    }
  }

  /** Build one secret-safe receipt. */
  receipt(call: Call, response: SQSResponse, objectId: string): Receipt {
    const requestId = header(response.headers, 'x-amzn-requestid');
    return { callId: call.id, provider, observedAt: this.now().toISOString().replace(/\.000Z$/, 'Z'),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(requestIdPattern.test(requestId) ? { providerRequestId: requestId } : {}) };
  }
}

/** Stable SQS system attributes requested by every receive. */
export function receiveSystemAttributes(): string[] { return [...systemAttributes]; }
/** Read one case-insensitive response header. */
export function header(headers: ProviderResponse['headers'], name: string): string {
  const value = headers[name.toLowerCase()];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}
/** Classify one SQS error without copying provider message text into Flow state. */
export function classifyResponse(operation: string, subject: string, response: SQSResponse, credentials: Credentials, now: Date): { branch: 'retry' | 'receiptHandleInvalid' | 'providerRejected'; failure: Failure; retryAfterMs: number } {
  let code = header(response.headers, 'x-amzn-query-error').split(';')[0] ?? '';
  let message = '';
  try {
    const parsed = JSON.parse(Buffer.from(response.body.slice(0, 65_536)).toString('utf8')) as { __type?: unknown; message?: unknown };
    if (typeof parsed.__type === 'string') code = parsed.__type.split('#').pop()?.split(':')[0] ?? '';
    if (typeof parsed.message === 'string') message = parsed.message;
  } catch { /* HTTP status remains authoritative. */ }
  if (!errorCodePattern.test(code) || [credentials.secretAccessKey.reveal(), credentials.sessionToken?.reveal()].some(secret => secret && code.includes(secret))) code = '';
  const status = response.status;
  const withStatus = (summary: string) => `${summary} with HTTP ${status}${code ? ` ${code}` : ''}`;
  let branch: 'retry' | 'receiptHandleInvalid' | 'providerRejected' = 'providerRejected';
  let kind: Failure['kind'] = 'PROVIDER_REJECTION';
  let description = withStatus(`SQS rejected the ${subject}`);
  if (status === 429 || ['RequestThrottled', 'ThrottlingException', 'Throttling', 'KmsThrottled', 'AWS.SimpleQueueService.RequestThrottled'].includes(code)) { branch = 'retry'; kind = 'RATE_LIMIT'; description = `SQS throttled the ${subject}`; }
  else if (code === 'RequestTimeout' || status === 408) { branch = 'retry'; kind = 'TRANSPORT'; description = `SQS timed out the ${subject}`; }
  else if (status >= 500 && status !== 501) { branch = 'retry'; kind = 'AVAILABILITY'; description = withStatus(`SQS is temporarily unable to serve the ${subject}`); }
  else if (['ReceiptHandleIsInvalid', 'InvalidIdFormat', 'MessageNotInflight', 'AWS.SimpleQueueService.MessageNotInflight'].includes(code) || (['InvalidParameterValue', 'InvalidParameterValueException'].includes(code) && /receipt ?handle/i.test(message))) { branch = 'receiptHandleInvalid'; kind = 'NOT_FOUND'; description = `${withStatus(`SQS no longer accepts the receipt handle for the ${subject}`)}; the message may be delivered again`; }
  else if (['QueueDoesNotExist', 'AWS.SimpleQueueService.NonExistentQueue'].includes(code)) { kind = 'NOT_FOUND'; description = `${withStatus(`SQS rejected the ${subject}`)} because the queue does not exist; check queueUrl and region`; }
  else if (['OverLimit', 'AWS.SimpleQueueService.OverLimit'].includes(code)) { description = `${withStatus(`SQS rejected the ${subject}`)} because the queue holds too many in-flight messages; delete or release received messages`; }
  else if (['RequestTimeTooSkewed', 'SignatureExpired'].includes(code)) { kind = 'AUTHENTICATION'; description = `SQS rejected the ${subject} because the Worker clock differs from AWS by more than 5 minutes`; }
  else if (['InvalidClientTokenId', 'UnrecognizedClientException', 'SignatureDoesNotMatch', 'IncompleteSignature', 'ExpiredToken', 'InvalidSecurity', 'MissingAuthenticationToken', 'InvalidAccessKeyId', 'AWS.SimpleQueueService.InvalidSecurity'].includes(code) || status === 401) { kind = 'AUTHENTICATION'; description = `${withStatus(`SQS rejected the connection credentials for the ${subject}`)}; replace the access key or session token`; }
  else if (['AccessDenied', 'AccessDeniedException', 'KmsAccessDenied', 'KmsDisabled', 'KmsInvalidState', 'KmsNotFound', 'KmsOptInRequired', 'KmsInvalidKeyUsage'].includes(code) || status === 403) { kind = 'AUTHORIZATION'; description = withStatus(`SQS denied the ${subject}`); }
  const failure: Failure = { provider, operation, kind, message: description };
  return { branch, failure, retryAfterMs: branch === 'retry' ? parseRetryAfter(header(response.headers, 'retry-after'), now) : 0 };
}
