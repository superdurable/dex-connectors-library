// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * Signs and verifies requests that a Dex host relays to an application with its scope token, such as
 * relayed provider Trigger callbacks or platform events. The scheme matches the Go Connector SDK:
 * RelayTimestampHeader carries the Unix second of signing, and RelaySignatureHeader carries "v1=" and the
 * hexadecimal HMAC-SHA256 of "v1:<timestamp>:<raw body>" keyed with the scope token. A request is valid
 * only when the signature matches in constant time and the timestamp is within five minutes of receipt.
 *
 * These functions never log, store, or return the token. Applications with a loaded project use
 * LoadedProject.verifyRelayedRequest, which reads the scope token and rereads it after a mismatch.
 */

import { createHmac, timingSafeEqual } from 'node:crypto';

/** Header carrying the Unix second at which the host signed a relayed request. */
export const RelayTimestampHeader = 'Dex-Relay-Timestamp';
/** Header carrying "v1=" and the hexadecimal HMAC-SHA256 of "v1:<timestamp>:<body>" keyed with the scope token. */
export const RelaySignatureHeader = 'Dex-Relay-Signature';
/** How far a relayed request's timestamp may be from the receiver's clock, in milliseconds. */
export const RelayTimestampToleranceMs = 5 * 60_000;
const relaySignatureVersion = 'v1';

/** Request headers as Node's IncomingMessage, a Fetch Headers object, or a plain record supplies them. */
export type RelayedRequestHeaders = Headers | Readonly<Record<string, string | readonly string[] | undefined>>;

/**
 * Why a relayed request is refused: a header is absent, a header is malformed, the timestamp is outside
 * the tolerance, or the signature does not match the token.
 */
export type RelaySignatureRefusal = 'missing' | 'malformed' | 'stale' | 'mismatch';

/** The outcome of verifying one relayed request. */
export type RelaySignatureVerification = { readonly isValid: true; readonly signedAt: Date } | { readonly isValid: false; readonly refusal: RelaySignatureRefusal };

/** The signed parts of one relayed request, parsed before the token is needed. */
export type ParsedRelaySignature = { readonly isParsed: true; readonly timestamp: string; readonly signedAt: Date; readonly signature: Buffer } | { readonly isParsed: false; readonly refusal: Exclude<RelaySignatureRefusal, 'mismatch'> };

/**
 * Return the RelaySignatureHeader value for a relayed request: "v1=" and the hexadecimal HMAC-SHA256 of
 * "v1:<Unix seconds of timestamp>:<body>" keyed with token. Send the same Unix seconds in RelayTimestampHeader.
 */
export function signRelayedRequest(token: string, timestamp: Date, body: Uint8Array): string {
  return `${relaySignatureVersion}=${relaySignature(token, String(Math.floor(timestamp.getTime() / 1000)), body).toString('hex')}`;
}

/**
 * Parse the relay headers and check the timestamp window without the token, so a receiver can refuse an
 * unsigned, malformed, or stale request before reading any secret.
 */
export function parseRelaySignature(headers: RelayedRequestHeaders, receivedAt: Date): ParsedRelaySignature {
  const timestamp = relayHeader(headers, RelayTimestampHeader);
  const signatureHeader = relayHeader(headers, RelaySignatureHeader);
  if (!timestamp || !signatureHeader) return { isParsed: false, refusal: 'missing' };
  if (!/^-?\d{1,15}$/.test(timestamp) || !signatureHeader.startsWith(`${relaySignatureVersion}=`)) return { isParsed: false, refusal: 'malformed' };
  const encoded = signatureHeader.slice(relaySignatureVersion.length + 1);
  if (!/^[0-9a-fA-F]{64}$/.test(encoded)) return { isParsed: false, refusal: 'malformed' };
  const signedAt = new Date(Number(timestamp) * 1000);
  if (!(Math.abs(receivedAt.getTime() - signedAt.getTime()) <= RelayTimestampToleranceMs)) return { isParsed: false, refusal: 'stale' };
  return { isParsed: true, timestamp, signedAt, signature: Buffer.from(encoded, 'hex') };
}

/** Report whether a parsed signature matches token over the raw body, in constant time. */
export function matchesRelaySignature(parsed: Extract<ParsedRelaySignature, { isParsed: true }>, token: string, body: Uint8Array): boolean {
  return timingSafeEqual(parsed.signature, relaySignature(token, parsed.timestamp, body));
}

/** Verify one relayed request's headers and raw body against token at receivedAt. */
export function verifyRelaySignature(token: string, headers: RelayedRequestHeaders, body: Uint8Array, receivedAt: Date): RelaySignatureVerification {
  const parsed = parseRelaySignature(headers, receivedAt);
  if (!parsed.isParsed) return { isValid: false, refusal: parsed.refusal };
  return matchesRelaySignature(parsed, token, body) ? { isValid: true, signedAt: parsed.signedAt } : { isValid: false, refusal: 'mismatch' };
}

function relaySignature(token: string, timestamp: string, body: Uint8Array): Buffer {
  return createHmac('sha256', token).update(`${relaySignatureVersion}:${timestamp}:`).update(body).digest();
}

function relayHeader(headers: RelayedRequestHeaders, name: string): string {
  if (headers instanceof Headers) return headers.get(name) ?? '';
  const wanted = name.toLowerCase();
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === wanted)?.[1];
  return (typeof value === 'string' ? value : value?.[0]) ?? '';
}
