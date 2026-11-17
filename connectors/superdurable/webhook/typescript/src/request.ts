// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac, timingSafeEqual } from 'node:crypto';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { WebhookVerificationUnavailableError, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import type { Config, Credentials } from './generated/models.js';

const headerNamePattern = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;
const deniedHeaders = new Set(['Authorization', 'Proxy-Authorization', 'Cookie', 'Set-Cookie', 'Webhook-Signature']);
/** Validated inbound webhook settings shared by verification and event decoding. */
export interface InboundSettings {
  /** Authentication scheme. */ scheme: 'hmacSha256' | 'standardWebhooks' | 'sharedToken';
  /** Case-insensitive HMAC header name. */ signatureHeader: string;
  /** Literal prefix before an HMAC signature. */ signaturePrefix: string;
  /** HMAC signature wire encoding. */ signatureEncoding: 'hex' | 'base64';
  /** Signed Standard Webhooks timestamp tolerance. */ timestampToleranceSeconds: number;
  /** Case-insensitive shared token header. */ tokenHeader: string;
  /** Optional sender delivery ID header. */ eventIdHeader: string;
  /** Parsed RFC 6901 pointer to an event ID. */ eventIdPointer?: string[];
  /** Canonical safe headers to include in events. */ forwardedHeaders: string[];
}
/** Verified JSON or form request without authentication headers. */
export interface WebhookRequestEvent {
  /** Media type without parameters. */ contentType: 'application/json' | 'application/x-www-form-urlencoded';
  /** Explicitly allowed nonsecret request headers. */ headers?: Record<string, string>;
  /** Decoded JSON value when contentType is application/json. */ jsonBody?: unknown;
  /** Decoded form values when contentType is application/x-www-form-urlencoded. */ formBody?: Record<string, string[]>;
  /** UTC time the endpoint read the request. */ receivedAt: string;
}
/** Validate immutable inbound settings before serving requests. */
export function inboundSettings(config: Config): InboundSettings {
  const scheme = config.verification || 'hmacSha256';
  if (scheme !== 'hmacSha256' && scheme !== 'standardWebhooks' && scheme !== 'sharedToken') throw new TypeError('webhook verification is invalid');
  const signatureHeader = canonicalHeader('signatureHeader', config.signatureHeader || 'X-Signature-256');
  const tokenHeader = canonicalHeader('tokenHeader', config.tokenHeader || 'X-Webhook-Token');
  const signaturePrefix = config.signaturePrefix ?? '';
  if ([...signaturePrefix].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127))
    throw new TypeError('webhook signaturePrefix cannot contain control characters');
  const signatureEncoding = config.signatureEncoding || 'hex';
  if (signatureEncoding !== 'hex' && signatureEncoding !== 'base64') throw new TypeError('webhook signatureEncoding is invalid');
  const timestampToleranceSeconds = config.timestampToleranceSeconds || 300;
  if (!Number.isSafeInteger(timestampToleranceSeconds) || timestampToleranceSeconds <= 0) throw new TypeError('webhook timestampToleranceSeconds must be positive');
  const eventIdHeader = config.eventIdHeader ? canonicalHeader('eventIdHeader', config.eventIdHeader) : '';
  const eventIdPointer = config.eventIdPointer ? parsePointer(config.eventIdPointer) : undefined;
  const forwardedHeaders: string[] = [];
  for (const name of config.forwardedHeaders ?? []) {
    const header = canonicalHeader('forwardedHeaders entry', name);
    if (deniedHeaders.has(header) || header === signatureHeader || header === tokenHeader) throw new TypeError(`webhook forwardedHeaders cannot include ${header}`);
    if (!forwardedHeaders.includes(header)) forwardedHeaders.push(header);
  }
  return { scheme, signatureHeader, signaturePrefix, signatureEncoding, timestampToleranceSeconds, tokenHeader, eventIdHeader,
    ...(eventIdPointer ? { eventIdPointer } : {}), forwardedHeaders };
}
/** Verify one request with the configured scheme and connection secret. */
export function verifyInboundRequest(request: WebhookRequest, credentials: Credentials, settings: InboundSettings): void {
  const secret = credentials.signingSecret.reveal();
  if (!secret) throw new WebhookVerificationUnavailableError();
  if (settings.scheme === 'sharedToken') {
    const token = header(request, settings.tokenHeader);
    if (!token) throw new TypeError('webhook token header is missing');
    const received = createHash('sha256').update(token).digest();
    const expected = createHash('sha256').update(secret).digest();
    if (!timingSafeEqual(received, expected)) throw new TypeError('webhook token does not match');
    return;
  }
  if (settings.scheme === 'hmacSha256') {
    const value = header(request, settings.signatureHeader).trim();
    if (!value || !value.startsWith(settings.signaturePrefix)) throw new TypeError('webhook signature header or prefix is missing');
    const encoded = value.slice(settings.signaturePrefix.length);
    const signature = decodeDigest(encoded, settings.signatureEncoding);
    const expected = createHmac('sha256', secret).update(request.body).digest();
    if (!signature || !timingSafeEqual(signature, expected)) throw new TypeError('webhook signature does not match');
    return;
  }
  const key = standardKey(secret);
  const id = header(request, 'webhook-id'), timestamp = header(request, 'webhook-timestamp'), signatures = header(request, 'webhook-signature');
  if (!id || !timestamp || !signatures) throw new TypeError('Standard Webhooks headers are incomplete');
  if (!/^-?\d+$/.test(timestamp)) throw new TypeError('Standard Webhooks timestamp is not an integer');
  const seconds = Number(timestamp);
  if (!Number.isSafeInteger(seconds) || Math.abs(request.receivedAt.getTime() - seconds * 1000) > settings.timestampToleranceSeconds * 1000)
    throw new TypeError('Standard Webhooks timestamp is outside the tolerance');
  const expected = createHmac('sha256', key).update(Buffer.from(`${id}.${timestamp}.`)).update(request.body).digest();
  if (!signatures.split(/\s+/).some(entry => {
    const separator = entry.indexOf(',');
    return entry.slice(0, separator) === 'v1' && separator > 0 && (() => {
      const signature = decodeDigest(entry.slice(separator + 1), 'base64');
      return !!signature && timingSafeEqual(signature, expected);
    })();
  })) throw new TypeError('no Standard Webhooks signature matches');
}
/** Decode a verified request and derive its stable event ID. */
export function decodeInboundRequest(request: WebhookRequest, settings: InboundSettings): TriggerEvent<WebhookRequestEvent> {
  const contentType = header(request, 'content-type').split(';', 1)[0]?.trim().toLowerCase();
  if (contentType !== 'application/json' && contentType !== 'application/x-www-form-urlencoded') throw new TypeError('webhook Content-Type is not supported');
  const body = Buffer.from(request.body).toString('utf8');
  let jsonBody: unknown;
  let formBody: Record<string, string[]> | undefined;
  if (contentType === 'application/json') jsonBody = JSON.parse(body);
  else {
    if (/%(?![0-9A-Fa-f]{2})|;/.test(body)) throw new TypeError('webhook form body is invalid');
    formBody = {};
    for (const [name, value] of new URLSearchParams(body)) (formBody[name] ??= []).push(value);
  }
  const headers: Record<string, string> = {};
  for (const name of settings.forwardedHeaders) {
    const values = request.headers[name.toLowerCase()];
    if (values !== undefined) headers[name] = Array.isArray(values) ? values.join(', ') : values;
  }
  const payload: WebhookRequestEvent = { contentType, ...(Object.keys(headers).length ? { headers } : {}),
    ...(contentType === 'application/json' ? { jsonBody } : { formBody: formBody! }), receivedAt: request.receivedAt.toISOString().replace('.000Z', 'Z') };
  let id: string;
  if (settings.scheme === 'standardWebhooks') id = header(request, 'webhook-id');
  else if (settings.eventIdHeader && header(request, settings.eventIdHeader)) id = header(request, settings.eventIdHeader);
  else if (settings.eventIdPointer) {
    const value = pointerValue(settings.eventIdPointer, contentType === 'application/json' ? jsonBody : formBody);
    if (typeof value !== 'string' && (typeof value !== 'number' || !Number.isFinite(value))) throw new TypeError('webhook eventIdPointer does not name a string or number');
    id = String(value);
  } else id = createHash('sha256').update(request.body).digest('hex');
  if (!id || Buffer.byteLength(id) > 256 || !/^[\x21-\x7e]+$/.test(id)) throw new TypeError('webhook event ID must be printable ASCII without spaces');
  return { id, occurredAt: request.receivedAt, payload };
}
/** Resolve a validated pointer in a JSON or form document. */
export function pointerValue(tokens: string[], document: unknown): unknown {
  let value = document;
  for (const token of tokens) {
    if (Array.isArray(value)) {
      if (!/^(0|[1-9]\d*)$/.test(token)) return undefined;
      value = value[Number(token)];
    } else if (value && typeof value === 'object') value = (value as Record<string, unknown>)[token];
    else return undefined;
  }
  return value;
}
/** Parse one non-root RFC 6901 JSON Pointer. */
export function parsePointer(pointer: string): string[] {
  if (!pointer.startsWith('/')) throw new TypeError('JSON Pointer must start with /');
  return pointer.slice(1).split('/').map(token => {
    if (/~(?![01])|~$/.test(token)) throw new TypeError('JSON Pointer contains invalid escape');
    return token.replaceAll('~1', '/').replaceAll('~0', '~');
  });
}
function canonicalHeader(field: string, name: string): string {
  const trimmed = name.trim();
  if (!headerNamePattern.test(trimmed)) throw new TypeError(`webhook ${field} is not a valid HTTP header name`);
  return trimmed.toLowerCase().split('-').map(part => part.slice(0, 1).toUpperCase() + part.slice(1)).join('-');
}
function header(request: WebhookRequest, name: string): string {
  const value = request.headers[name.toLowerCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
function decodeDigest(encoded: string, encoding: 'hex' | 'base64'): Buffer | undefined {
  if (encoding === 'hex') return /^[0-9a-fA-F]{64}$/.test(encoded) ? Buffer.from(encoded, 'hex') : undefined;
  if (!/^[A-Za-z0-9+/]{43}=$/.test(encoded)) return undefined;
  const value = Buffer.from(encoded, 'base64');
  return value.length === 32 ? value : undefined;
}
function standardKey(secret: string): Buffer {
  if (!secret.startsWith('whsec_')) return Buffer.from(secret);
  const encoded = secret.slice(6);
  if (!encoded || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)) throw new WebhookVerificationUnavailableError();
  const key = Buffer.from(encoded, 'base64');
  if (!key.length) throw new WebhookVerificationUnavailableError();
  return key;
}
