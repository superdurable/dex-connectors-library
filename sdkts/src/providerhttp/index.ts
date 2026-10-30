// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { lookup as lookupAddresses } from 'node:dns/promises';
import { isIP } from 'node:net';
import http, { type ClientRequest, type IncomingMessage, type RequestOptions } from 'node:http';
import https from 'node:https';
import { isAllowedProviderIPAddress, isLoopbackIPAddressLiteral } from '../netpolicy.js';

/** Maximum body retained while extracting safe provider error tokens. */
export const MaxErrorBodyBytes = 64 << 10;
/** Maximum Retry-After delay, in milliseconds. */
export const MaxRetryAfterDelayMs = 60 * 60 * 1000;

/** Response exceeded its caller-owned safety limit. */
export class BodyTooLargeError extends Error {
  constructor() { super('provider response body exceeds its size limit'); this.name = 'BodyTooLargeError'; }
}

/** Address policy refused a request before transmission. */
export class AddressRefusal extends Error {
  /** Name a nonsecret network policy reason. */
  constructor(readonly reason: string) { super(`address not allowed: ${reason}`); this.name = 'AddressRefusal'; }
}

/** Sanitized transport failure with positive evidence about dispatch. */
export class ProviderTransportError extends Error {
  /** True only when no socket was assigned to the request. */
  constructor(readonly isProvablyNotSent: boolean) {
    super('provider request failed in transport');
    this.name = 'ProviderTransportError';
  }
}

/** Public-only HTTPS or explicitly owned private-network HTTP. */
export type AddressPolicy = 'publicHTTPS' | 'privateNetworkHTTP';

/** One bounded provider request; response bodies and URL values never enter errors. */
export interface ProviderRequest {
  /** Absolute provider URL. */ url: string;
  /** HTTP method. */ method: string;
  /** Request headers, including provider credentials when needed. */ headers?: Record<string, string>;
  /** Caller-encoded request body. */ body?: Uint8Array;
  /** Maximum response body size in bytes. */ maxResponseBytes: number;
  /** Network policy checked against all resolved addresses. */ addressPolicy: AddressPolicy;
  /** Request deadline in milliseconds. */ timeoutMs?: number;
  /** Caller cancellation. */ signal?: AbortSignal;
}

/** Bounded response; redirects remain visible as 3xx responses. */
export interface ProviderResponse {
  /** Numeric HTTP status. */ status: number;
  /** Response headers. */ headers: http.IncomingHttpHeaders;
  /** Bounded response bytes. */ body: Uint8Array;
}

/** Parse RFC 9110 delay-seconds or an HTTP-date, capped at one hour. */
export function parseRetryAfter(value: string, now = new Date()): number {
  const trimmed = value.trim();
  if (/^[0-9]+$/.test(trimmed)) {
    const seconds = Number(trimmed);
    return Number.isSafeInteger(seconds) ? Math.min(seconds * 1000, MaxRetryAfterDelayMs) : MaxRetryAfterDelayMs;
  }
  const weekday = '(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)';
  const month = '(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)';
  const time = '[0-9]{2}:[0-9]{2}:[0-9]{2}';
  const isHTTPDate = new RegExp(`^${weekday}, [0-9]{2} ${month} [0-9]{4} ${time} GMT$`).test(trimmed) ||
    new RegExp(`^(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), [0-9]{2}-${month}-[0-9]{2} ${time} GMT$`).test(trimmed) ||
    new RegExp(`^${weekday} ${month} {1,2}[0-9]{1,2} ${time} [0-9]{4}$`).test(trimmed);
  if (!isHTTPDate) return 0;
  const timestamp = Date.parse(trimmed);
  return Number.isNaN(timestamp) ? 0 : Math.min(Math.max(0, timestamp - now.getTime()), MaxRetryAfterDelayMs);
}

/** Validate a provider base URL without echoing input; returns original URL without trailing slashes, unlike Go's parsed rendering. WHATWG numeric loopback shorthands are accepted. */
export function validateBaseURL(value: string): string {
  if (!value) throw new TypeError('base URL is required');
  if (!/^[!-~]+$/.test(value)) throw new TypeError('base URL must be printable ASCII without spaces');
  if (value.includes('?') || value.includes('#')) throw new TypeError('base URL cannot contain a query or a fragment');
  let parsed: URL;
  try { parsed = new URL(value); } catch { throw new TypeError('base URL must be an absolute URL with a host'); }
  if (!parsed.hostname) throw new TypeError('base URL must be an absolute URL with a host');
  if (parsed.username || parsed.password) throw new TypeError('base URL cannot contain user information');
  if (parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && (parsed.hostname === 'localhost' || isLoopbackIPAddressLiteral(parsed.hostname)))) {
    throw new TypeError('base URL must use HTTPS unless its host is loopback');
  }
  return value.replace(/\/+$/, '');
}

/** Check whether a credential fits one HTTP header without alteration. */
export function isHeaderSafeCredential(value: string): boolean { return /^[!-~]+$/.test(value); }

/** Read a stream to at most maxBytes. A negative limit throws; Go clamps it to zero. */
export async function readBoundedBody(body: AsyncIterable<Uint8Array>, maxBytes: number): Promise<Uint8Array> {
  if (!Number.isSafeInteger(maxBytes) || maxBytes < 0) throw new RangeError('body limit must be a non-negative integer');
  const chunks: Uint8Array[] = [];
  let length = 0;
  for await (const chunk of body) {
    length += chunk.length;
    if (length > maxBytes) throw new BodyTooLargeError();
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, length);
}

/** Return safe tokens at ordered JSON pointers. Numeric lexemes may normalize through JavaScript Number. */
export function readErrorTokens(body: Uint8Array, pointers: readonly string[]): string[] {
  let document: unknown;
  try { document = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return []; }
  const tokens: string[] = [];
  for (const pointer of pointers) {
    const value = resolvePointer(document, pointer);
    if ((typeof value === 'string' || typeof value === 'number') && /^[A-Za-z0-9_.:-]{1,64}$/.test(String(value))) tokens.push(String(value));
  }
  return tokens;
}

/** Send once using pinned, policy-checked DNS results and no redirect replay. */
export async function sendProviderRequest(request: ProviderRequest): Promise<ProviderResponse> {
  if (!Number.isSafeInteger(request.maxResponseBytes) || request.maxResponseBytes < 0) throw new RangeError('response body limit must be a non-negative integer');
  if (request.addressPolicy !== 'publicHTTPS' && request.addressPolicy !== 'privateNetworkHTTP') throw new AddressRefusal('address policy is invalid');
  let parsed: URL;
  try { parsed = new URL(request.url); } catch { throw new AddressRefusal('URL is invalid'); }
  if (parsed.username || parsed.password || !['http:', 'https:'].includes(parsed.protocol)) throw new AddressRefusal('URL scheme or user information is not allowed');
  if (request.addressPolicy === 'publicHTTPS' && parsed.protocol !== 'https:') throw new AddressRefusal('plain http is not allowed');
  if (request.timeoutMs !== undefined && (!Number.isSafeInteger(request.timeoutMs) || request.timeoutMs < 0)) throw new RangeError('request timeout must be non-negative milliseconds');
  const timeoutSignal = request.timeoutMs ? AbortSignal.timeout(request.timeoutMs) : undefined;
  const signal = timeoutSignal && request.signal ? AbortSignal.any([timeoutSignal, request.signal]) : timeoutSignal ?? request.signal;
  const hostname = parsed.hostname.replace(/^\[|\]$/g, '');
  let addresses: { address: string; family: number }[];
  try { addresses = isIP(hostname) ? [{ address: hostname, family: isIP(hostname) }] : await lookupAddresses(hostname, { all: true, verbatim: true }); }
  catch { throw new ProviderTransportError(true); }
  if (addresses.length === 0 || addresses.some(({ address }) => !isAllowedProviderIPAddress(address, parsed.protocol, request.addressPolicy))) {
    throw new AddressRefusal('resolved address violates policy');
  }
  const selected = addresses[0];
  if (!selected) throw new AddressRefusal('host has no resolved address');
  const options: RequestOptions = {
    method: request.method, headers: request.headers, signal,
    lookup: (_hostname, _options, callback) => callback(null, selected.address, selected.family),
  };
  return new Promise((resolve, reject) => {
    let hasSocket = false;
    let hasWrittenRequest = false;
    let clientRequest: ClientRequest;
    try {
      clientRequest = (parsed.protocol === 'https:' ? https : http).request(parsed, options, (response: IncomingMessage) => {
        void readBoundedBody(response, request.maxResponseBytes).then(body => {
          resolve({ status: response.statusCode ?? 0, headers: response.headers, body });
        }).catch((error: unknown) => {
          response.destroy();
          reject(error instanceof BodyTooLargeError ? error : new ProviderTransportError(false));
        });
      });
    } catch { reject(new ProviderTransportError(true)); return; }
    clientRequest.on('socket', () => { hasSocket = true; });
    clientRequest.on('finish', () => { hasWrittenRequest = true; });
    clientRequest.on('error', () => reject(new ProviderTransportError(!hasSocket && !hasWrittenRequest)));
    try { clientRequest.end(request.body); }
    catch { reject(new ProviderTransportError(!hasSocket && !hasWrittenRequest)); }
  });
}

function resolvePointer(document: unknown, pointer: string): unknown {
  if (pointer === '') return document;
  if (!pointer.startsWith('/')) return undefined;
  let current: unknown = document;
  for (const encoded of pointer.slice(1).split('/')) {
    if (/~(?![01])/.test(encoded)) return undefined;
    const segment = encoded.replace(/~1/g, '/').replace(/~0/g, '~');
    if (Array.isArray(current)) {
      if (!/^(0|[1-9][0-9]*)$/.test(segment)) return undefined;
      current = current[Number(segment)];
    } else if (current !== null && typeof current === 'object') {
      current = (current as Record<string, unknown>)[segment];
    } else return undefined;
  }
  return current;
}
