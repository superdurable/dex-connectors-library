// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Call, CredentialProvider, ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetObjectTextOperation, HeadObjectOperation, ListObjectsOperation, PutObjectOperation } from './operations.js';

const emptyHash = createHash('sha256').update('').digest('hex');
const encoder = new TextEncoder();

/** One S3 exchange, including a bounded-body result. */
export interface S3Response {
  /** HTTP status. */ status: number;
  /** Provider headers. */ headers: ProviderResponse['headers'];
  /** Complete bounded response body. */ body: Uint8Array;
  /** True when a successful body exceeded the configured bound. */ isTooLarge: boolean;
}

/** Injectable sender for deterministic provider tests. */
export type S3RequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Constructor dependencies for request and clock tests. */
export interface S3ClientOptions {
  /** Provider HTTP sender. */ send?: S3RequestSender;
  /** Signing and receipt clock. */ now?: () => Date;
}

/** Credentials kept only in one Invoke, outside Dex state. */
export interface S3Session {
  /** Access key ID for signing. */ accessKeyId: string;
  /** Secret key for signing. */ secretAccessKey: string;
  /** Optional temporary session token. */ sessionToken: string;
}

/** S3 request data before SigV4 signing. */
export interface S3Request {
  /** HTTP method. */ method: 'GET' | 'HEAD' | 'PUT';
  /** Bucket name. */ bucket: string;
  /** Optional object key. */ key?: string;
  /** Query parameters. */ query?: Record<string, string>;
  /** Request headers. */ headers?: Record<string, string>;
  /** Request body. */ body?: Uint8Array;
  /** Maximum complete body size. */ maxBytes?: number;
}

/** Signed, bounded S3 REST client for a single connection. */
export class S3Client {
  readonly region: string;
  readonly defaultBucket: string;
  readonly listPageSize: number;
  readonly maxResponseBytes: number;
  readonly maxTextBytes: number;
  readonly maxUploadBytes: number;
  private readonly endpoint: URL;
  private readonly addressingStyle: string;
  private readonly isAmazonEndpoint: boolean;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: S3RequestSender;
  private readonly now: () => Date;

  /** Validate public configuration and bind a private credential source. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: S3ClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    this.region = config.region || 'us-east-1';
    if (!/^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/.test(this.region)) throw new TypeError('S3 region must be a Region code');
    const suffix = /^(us|eu|ap|sa|ca|me|af|il|mx)-\w+-\d+$|^us-gov-\w+-\d+$/.test(this.region) ? 'amazonaws.com' : /^cn-\w+-\d+$/.test(this.region) ? 'amazonaws.com.cn' : /^eusc-de-\w+-\d+$/.test(this.region) ? 'amazonaws.eu' : '';
    this.isAmazonEndpoint = !config.endpoint;
    if (this.isAmazonEndpoint && !suffix) throw new TypeError('S3 region is not an Amazon S3 Region; set endpoint');
    this.endpoint = new URL(validateBaseURL(config.endpoint || `https://s3.${this.region}.${suffix}`));
    if (!/^(\/[A-Za-z0-9._~-]+)*$/.test(this.endpoint.pathname.replace(/\/$/, '')) || this.endpoint.search || this.endpoint.hash) throw new TypeError('S3 endpoint path is invalid');
    this.addressingStyle = config.addressingStyle || 'auto';
    if (!['auto', 'path', 'virtualHosted'].includes(this.addressingStyle)) throw new TypeError('S3 addressingStyle is invalid');
    this.defaultBucket = config.defaultBucket || '';
    if (this.defaultBucket) validateBucket(this.defaultBucket);
    this.listPageSize = config.listPageSize || 100;
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    this.maxTextBytes = config.maxTextBytes || 1_048_576;
    this.maxUploadBytes = config.maxUploadBytes || 1_048_576;
    if (!Number.isSafeInteger(this.listPageSize) || this.listPageSize < 1 || this.listPageSize > 1000) throw new TypeError('S3 listPageSize must be from 1 to 1000');
    if (![this.maxResponseBytes, this.maxTextBytes].every(value => Number.isSafeInteger(value) && value > 0)) throw new TypeError('S3 response limits must be positive');
    if (!Number.isSafeInteger(this.maxUploadBytes) || this.maxUploadBytes < 1 || this.maxUploadBytes > 5_242_880) throw new TypeError('S3 maxUploadBytes must be from 1 to 5242880');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the bucket listing query. */ listObjects(): ListObjectsOperation { return new ListObjectsOperation(this); }
  /** Bind the metadata query. */ headObject(): HeadObjectOperation { return new HeadObjectOperation(this); }
  /** Bind the text query. */ getObjectText(): GetObjectTextOperation { return new GetObjectTextOperation(this); }
  /** Bind the object write mutation. */ putObject(): PutObjectOperation { return new PutObjectOperation(this); }

  /** Resolve current credentials once per Invoke without exposing secret-bearing errors. */
  async startSession(call: Call): Promise<S3Session | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const accessKeyId = credentials.accessKeyId.reveal();
      const secretAccessKey = credentials.secretAccessKey.reveal();
      const sessionToken = credentials.sessionToken?.reveal() ?? '';
      if (!isHeaderSafeCredential(accessKeyId) || accessKeyId.length > 128 || /[/,=]/.test(accessKeyId)) return undefined;
      if (!secretAccessKey || Buffer.byteLength(secretAccessKey) > 256 || /\p{Cc}/u.test(secretAccessKey)) return undefined;
      if (sessionToken && (!isHeaderSafeCredential(sessionToken) || sessionToken.length > 4096)) return undefined;
      return { accessKeyId, secretAccessKey, sessionToken };
    } catch { return undefined; }
  }

  /** Resolve and validate an input bucket. */
  bucket(inputBucket?: string): string {
    const bucket = inputBucket || this.defaultBucket;
    if (!bucket) throw new TypeError('bucket is required because the connection has no defaultBucket');
    validateBucket(bucket);
    if (this.addressingStyle === 'virtualHosted' && bucket.includes('.') && this.endpoint.protocol === 'https:') throw new TypeError('a bucket name with a dot cannot use virtualHosted addressing over HTTPS');
    return bucket;
  }

  /** Send a SigV4 request once; Dex owns operation retries. */
  async exchange(call: Call, session: S3Session, request: S3Request): Promise<S3Response> {
    const target = this.requestURL(request);
    const now = this.now();
    const amzDate = now.toISOString().replace(/[-:]|\.\d{3}/g, '');
    const body = request.body;
    const payloadHash = body ? createHash('sha256').update(body).digest('hex') : emptyHash;
    const headers: Record<string, string> = { ...request.headers, 'x-amz-content-sha256': payloadHash, 'x-amz-date': amzDate };
    if (body) headers['content-length'] = String(body.length);
    if (session.sessionToken) headers['x-amz-security-token'] = session.sessionToken;
    const signed: Record<string, string> = { host: target.host.toLowerCase() };
    for (const [name, value] of Object.entries(headers)) signed[name.toLowerCase()] = value.trim().replace(/\s+/g, ' ');
    const names = Object.keys(signed).sort();
    const canonicalHeaders = names.map(name => `${name}:${signed[name]}\n`).join('');
    const canonicalRequest = [request.method, target.pathname, target.search.slice(1), canonicalHeaders, names.join(';'), payloadHash].join('\n');
    const date = amzDate.slice(0, 8);
    const scope = `${date}/${this.region}/s3/aws4_request`;
    const stringToSign = `AWS4-HMAC-SHA256\n${amzDate}\n${scope}\n${createHash('sha256').update(canonicalRequest).digest('hex')}`;
    const signingKey = hmac(hmac(hmac(hmac(`AWS4${session.secretAccessKey}`, date), this.region), 's3'), 'aws4_request');
    headers.authorization = `AWS4-HMAC-SHA256 Credential=${session.accessKeyId}/${scope}, SignedHeaders=${names.join(';')}, Signature=${createHmac('sha256', signingKey).update(stringToSign).digest('hex')}`;
    headers['accept-encoding'] = 'identity';
    const remaining = call.context.deadline ? call.context.deadline.getTime() - Date.now() : 12_000;
    const timeoutMs = Math.max(1, Math.min(12_000, remaining));
    try {
      const response = await this.send({ url: target.toString(), method: request.method, headers, ...(body ? { body } : {}), maxResponseBytes: request.maxBytes ?? this.maxResponseBytes, addressPolicy: this.isAmazonEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs, signal: call.context.cancellationSignal });
      return { ...response, isTooLarge: false };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { status: error.status, headers: error.headers ?? {}, body: new Uint8Array(), isTooLarge: true };
      throw error;
    }
  }

  /** Make a safe receipt from provider correlation metadata. */
  receipt(_call: Call, response: S3Response, bucket: string, key?: string): ReceiptDraft {
    const requestId = header(response, 'x-amz-request-id');
    return { providerObjectId: key === undefined ? bucket : `${bucket}/${key}`, ...(requestId && /^[A-Za-z0-9+/=._-]{1,128}$/.test(requestId) ? { providerRequestId: requestId } : {}), observedAt: this.now().toISOString() };
  }

  /** Return the clock used for retry headers. */ currentTime(): Date { return this.now(); }

  private requestURL(request: S3Request): URL {
    const target = new URL(this.endpoint);
    const basePath = target.pathname.replace(/\/$/, '');
    const usePath = this.addressingStyle === 'path' || (this.addressingStyle === 'auto' && (!this.isAmazonEndpoint || request.bucket.includes('.')));
    if (usePath) target.pathname = escapeS3Path(`${basePath}/${request.bucket}${request.key === undefined ? '' : `/${request.key}`}`);
    else { target.hostname = `${request.bucket}.${target.hostname}`; target.pathname = escapeS3Path(`${basePath}/${request.key ?? ''}`); }
    const query = Object.entries(request.query ?? {}).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0);
    target.search = query.map(([name, value]) => `${encodeURIComponent(name)}=${encodeURIComponent(value)}`).join('&');
    return target;
  }
}

/** Read a case-insensitive single response header. */
export function header(response: S3Response, name: string): string {
  const value = response.headers[name.toLowerCase()];
  return Array.isArray(value) ? value.join(',') : value ?? '';
}

/** Parse S3 Retry-After using the same operation clock. */
export function retryAfter(response: S3Response, now: Date): number { return parseRetryAfter(header(response, 'retry-after'), now); }

/** Validate one general-purpose bucket name. */
export function validateBucket(bucket: string): void {
  if (!/^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(bucket) || bucket.includes('..') || /^\d+\.\d+\.\d+\.\d+$/.test(bucket)) throw new TypeError('bucket must be a 3 to 63 character S3 bucket name');
}

/** Validate an object key or list filter. */
export function validateKey(value: string, field = 'key', isPrefix = false): void {
  if (!isPrefix && !value) throw new TypeError('key must be 1 to 1024 bytes');
  if (Buffer.byteLength(value) > 1024) throw new TypeError(`${field} must be at most 1024 bytes`);
  if (/\p{Cc}/u.test(value)) throw new TypeError(`${field} must be UTF-8 text without control characters`);
  if (!isPrefix && value.split('/').some(segment => segment === '.' || segment === '..')) throw new TypeError('key cannot contain . or .. path segments');
}

/** Keep the access key, secret, and session token out of Results. */
export function containsCredential(value: string, session: S3Session, includeAccessKey = false): boolean {
  return [session.secretAccessKey, session.sessionToken, ...(includeAccessKey ? [session.accessKeyId] : [])].some(secret => secret.length > 0 && value.includes(secret));
}

/** Bound a provider header and suppress credential echoes. */
export function safeHeader(response: S3Response, name: string, session: S3Session): string {
  const value = header(response, name);
  return value.length <= 2048 && !containsCredential(value, session) ? value : '';
}

function hmac(key: string | Uint8Array, value: string): Buffer { return createHmac('sha256', key).update(value).digest(); }
function escapeS3Path(path: string): string { return Array.from(encoder.encode(path)).map(byte => (byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122) || (byte >= 48 && byte <= 57) || [45, 46, 95, 126, 47].includes(byte) ? String.fromCharCode(byte) : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`).join(''); }
