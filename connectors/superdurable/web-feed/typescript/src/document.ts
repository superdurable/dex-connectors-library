// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { lookup } from 'node:dns/promises';
import { BlockList, isIP } from 'node:net';
import { connect } from 'node:tls';
import { gunzipSync } from 'node:zlib';
import type { FailureKind } from '@superdurable/connectors-sdk';
import {
  AddressRefusal, BodyTooLargeError, ProviderStalledError, ProviderTransportError, parseRetryAfter, sendProviderRequest,
  type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { type GoURL, parseGoURL } from './go_url.js';
import { maxURLBytes } from './text.js';
import { XMLTokenizer } from './xml.js';

/** Redirect hops one read follows, which also ends redirect loops. */
export const maxRedirects = 5;
const maxValidatorBytes = 1024;
const maxErrorBodyBytes = 1024;
const diagnosisTimeoutMs = 5000;
/** Starts every refused-address failure, whether the literal check or the transport refused it. */
export const addressRefusalMessage = 'the URL resolves to an address the connector will not connect to: ';
const mediaTypePattern = /^[a-z0-9][a-z0-9.+-]{0,63}\/[a-z0-9][a-z0-9.+-]{0,63}$/;

/** Outcome class of one read before an operation maps it to its own branch. */
export type FetchClass = 'retry' | 'notFound' | 'providerRejected' | 'unreachable' | 'invalidResponse' | 'defect';

/** A classified failure of one read. It never holds a URL, a header, or body text. */
export interface FetchFailure { class: FetchClass; kind: FailureKind; message: string; retryAfterMs: number }

/** HTTP cache validators of a previous read. */
export interface Validators { etag: string; lastModified: string }

/** One GET that follows redirects and reads one body. */
export interface DocumentRequest {
  target: GoURL;
  accept: string;
  validators: Validators;
  maxBytes: number;
  /** Read only the first maxBytes and report truncation instead of rejecting a longer body. */
  isPrefixRead: boolean;
  /** Decompress a body that starts with the gzip magic number. */
  mayBeGzip: boolean;
  acceptsMediaType: (mediaType: string) => boolean;
  expectedContent: string;
}

/** A successful read, or a 304 answer to a conditional GET. */
export interface FetchedDocument {
  finalURL: GoURL;
  mediaType: string;
  charsetLabel: string;
  body: Uint8Array;
  isTruncated: boolean;
  isNotModified: boolean;
  validators: Validators;
}

/** Transport and request settings that every read shares. */
export interface FetchSettings {
  send: (request: ProviderRequest) => Promise<ProviderResponse>;
  userAgent: string;
  timeoutMs: number;
  now: () => Date;
  signal: AbortSignal | undefined;
}

/** The host name does not exist; a repeat cannot help. */
export class HostNotFoundError extends Error { constructor() { super('host does not exist'); this.name = 'HostNotFoundError'; } }
/** The site's certificate failed verification, or it serves no TLS on the port. */
export class TLSRefusalError extends Error {
  constructor(readonly isPlainHTTP: boolean) { super('TLS connection refused'); this.name = 'TLSRefusalError'; }
}

/** Send the GET, follow redirects through the same checks, and read the final body. */
export async function fetchDocument(settings: FetchSettings, request: DocumentRequest): Promise<{ document: FetchedDocument } | { failure: FetchFailure }> {
  let current = request.target;
  for (let hop = 0; ; hop++) {
    const reason = refusedAddressLiteralReason(current.hostname());
    if (reason !== '') return { failure: { class: 'unreachable', kind: 'VALIDATION', message: addressRefusalMessage + reason, retryAfterMs: 0 } };
    const sent = await send(settings, current, request);
    if ('failure' in sent) return sent;
    const { response, prefix } = sent;
    if (!isRedirectStatus(response.status)) return readResponse(settings, response, prefix, current, request);
    if (hop === maxRedirects) return { failure: invalidResponse(`the URL redirected more than ${maxRedirects} times`) };
    try { current = resolveRedirectLocation(current, headerValue(response.headers.location)); }
    catch { return { failure: invalidResponse('a redirect named no usable http or https URL') }; }
  }
}

async function send(settings: FetchSettings, target: GoURL, request: DocumentRequest): Promise<{ response: ProviderResponse; prefix: Uint8Array | undefined } | { failure: FetchFailure }> {
  const headers: Record<string, string> = { Accept: request.accept, 'User-Agent': settings.userAgent };
  if (request.validators.etag !== '') headers['If-None-Match'] = request.validators.etag;
  if (request.validators.lastModified !== '') headers['If-Modified-Since'] = request.validators.lastModified;
  const chunks: Uint8Array[] = [];
  let chunkBytes = 0;
  let hasObservedChunks = false;
  const providerRequest: ProviderRequest = {
    url: target.toString(), method: 'GET', headers, maxResponseBytes: request.maxBytes,
    maxErrorResponseBytes: maxErrorBodyBytes, ignoreErrorBodyReadFailure: true,
    addressPolicy: 'publicHTTPS', timeoutMs: settings.timeoutMs, ...(settings.signal ? { signal: settings.signal } : {}),
  };
  if (request.isPrefixRead) {
    // A page prefix is enough, so the read stops once it exceeds the bound instead of failing.
    providerRequest.onResponseChunk = (chunk, status) => {
      if (status < 200 || status > 299) return true;
      hasObservedChunks = true;
      chunks.push(chunk);
      chunkBytes += chunk.length;
      return chunkBytes <= request.maxBytes;
    };
  }
  try {
    const response = await settings.send(providerRequest);
    return { response, prefix: hasObservedChunks ? Buffer.concat(chunks, chunkBytes) : undefined };
  } catch (error) {
    return { failure: classifyTransportError(error, request) };
  }
}

/** Separate failures a repeat cannot fix from transient ones. */
function classifyTransportError(error: unknown, request: DocumentRequest): FetchFailure {
  if (error instanceof AddressRefusal) return { class: 'unreachable', kind: 'VALIDATION', message: addressRefusalMessage + 'only public addresses are allowed', retryAfterMs: 0 };
  if (error instanceof HostNotFoundError) return { class: 'unreachable', kind: 'TRANSPORT', message: 'the host does not exist', retryAfterMs: 0 };
  if (error instanceof TLSRefusalError) {
    return { class: 'unreachable', kind: 'TRANSPORT', message: error.isPlainHTTP ? 'the site does not serve https on the requested port' : "the site's TLS certificate failed verification", retryAfterMs: 0 };
  }
  if (error instanceof BodyTooLargeError && error.status !== undefined && error.status >= 200 && error.status <= 299) return tooLarge(request.expectedContent);
  if (error instanceof ProviderTransportError && error.status !== undefined) return interruptedResponse();
  if (error instanceof ProviderStalledError) return interruptedResponse();
  return { class: 'retry', kind: 'TRANSPORT', message: 'the request failed before a complete response arrived', retryAfterMs: 0 };
}

/** Classify the final response's status and read its body within the request's bound. */
function readResponse(settings: FetchSettings, response: ProviderResponse, prefix: Uint8Array | undefined, finalURL: GoURL, request: DocumentRequest): { document: FetchedDocument } | { failure: FetchFailure } {
  const status = response.status;
  if (status === 304) {
    if (request.validators.etag === '' && request.validators.lastModified === '') {
      return { failure: invalidResponse('the site answered 304 Not Modified to a request without validators') };
    }
    const refreshed = readValidators(response.headers);
    if (refreshed.etag === '') refreshed.etag = request.validators.etag;
    if (refreshed.lastModified === '') refreshed.lastModified = request.validators.lastModified;
    return { document: { finalURL, mediaType: '', charsetLabel: '', body: new Uint8Array(), isTruncated: false, isNotModified: true, validators: refreshed } };
  }
  if (status === 404 || status === 410) return { failure: { class: 'notFound', kind: 'NOT_FOUND', message: `the site answered HTTP ${status}`, retryAfterMs: 0 } };
  if (status === 429) return { failure: { class: 'retry', kind: 'RATE_LIMIT', message: 'the site answered HTTP 429', retryAfterMs: retryAfter(settings, response) } };
  if (status === 408 || status === 425 || status >= 500) return { failure: { class: 'retry', kind: 'AVAILABILITY', message: `the site answered HTTP ${status}`, retryAfterMs: retryAfter(settings, response) } };
  if (status >= 400) return { failure: { class: 'providerRejected', kind: 'PROVIDER_REJECTION', message: `the site refused the request with HTTP ${status}`, retryAfterMs: 0 } };
  if (status < 200 || status >= 300 || status === 204) return { failure: invalidResponse(`the site answered HTTP ${status} instead of ${request.expectedContent}`) };
  const [mediaType, charsetLabel] = parseContentType(headerValue(response.headers['content-type']));
  if (!request.acceptsMediaType(mediaType)) return { failure: invalidResponse(`the URL serves ${describeMediaType(mediaType)}, not ${request.expectedContent}`) };
  const contentLength = Number(headerValue(response.headers['content-length']) || Number.NaN);
  if (!request.isPrefixRead && Number.isFinite(contentLength) && contentLength > request.maxBytes) return { failure: tooLarge(request.expectedContent) };
  let body = prefix ?? response.body;
  let isTruncated = false;
  if (request.isPrefixRead) {
    if (body.length > request.maxBytes) { body = body.subarray(0, request.maxBytes); isTruncated = true; }
  } else {
    if (body.length > request.maxBytes) return { failure: tooLarge(request.expectedContent) };
    if (request.mayBeGzip && body[0] === 0x1f && body[1] === 0x8b) {
      try { body = gunzipSync(body, { maxOutputLength: request.maxBytes + 1 }); }
      catch (error) {
        if (error instanceof RangeError) return { failure: tooLarge(request.expectedContent) };
        return { failure: invalidResponse('the gzip-compressed body is malformed') };
      }
      if (body.length > request.maxBytes) return { failure: tooLarge(request.expectedContent) };
    }
  }
  return { document: { finalURL, mediaType, charsetLabel, body, isTruncated, isNotModified: false, validators: readValidators(response.headers) } };
}

function retryAfter(settings: FetchSettings, response: ProviderResponse): number {
  return parseRetryAfter(headerValue(response.headers['retry-after']), settings.now());
}

/** Keep the response's validators only when each can be sent back unchanged. */
function readValidators(headers: ProviderResponse['headers']): Validators {
  const etag = headerValue(headers.etag);
  const lastModified = headerValue(headers['last-modified']);
  return { etag: isPrintableHeaderText(etag, maxValidatorBytes) ? etag : '', lastModified: isPrintableHeaderText(lastModified, maxValidatorBytes) ? lastModified : '' };
}

/** Check the validators an application passes back from a previous read. */
export function validatorsFromInput(etag: string | undefined, lastModified: string | undefined): Validators {
  if (etag && !isPrintableHeaderText(etag, maxValidatorBytes)) throw new TypeError('etag must be at most 1024 printable ASCII characters, as a previous read returned it');
  if (lastModified && !isPrintableHeaderText(lastModified, maxValidatorBytes)) throw new TypeError('lastModified must be at most 1024 printable ASCII characters, as a previous read returned it');
  return { etag: etag ?? '', lastModified: lastModified ?? '' };
}

/** Whether value is nonempty printable ASCII, spaces included, of at most maxBytes bytes. */
export function isPrintableHeaderText(value: string, maxBytes: number): boolean {
  return value !== '' && value.length <= maxBytes && /^[\x20-\x7e]+$/.test(value);
}

/** Parse a Content-Type header as Go mime.ParseMediaType; a malformed value reads as unparsable. */
function parseContentType(value: string): [string, string] {
  if (value.trim() === '') return ['', ''];
  const [rawType = '', ...rawParameters] = value.split(';');
  const mediaType = rawType.trim().toLowerCase();
  if (!/^[!#$%&'*+.^_`|~0-9a-z-]+(?:\/[!#$%&'*+.^_`|~0-9a-z-]+)?$/.test(mediaType)) return ['unparsable', ''];
  const parameters = new Map<string, string>();
  for (const rawParameter of rawParameters) {
    if (rawParameter.trim() === '') continue;
    const match = /^\s*([!#$%&'*+.^_`|~0-9A-Za-z-]+)\s*=\s*("(?:[^"\\]|\\.)*"|[!#$%&'*+.^_`|~0-9A-Za-z-]+)\s*$/.exec(rawParameter);
    if (!match) return ['unparsable', ''];
    const key = match[1]!.toLowerCase();
    if (parameters.has(key)) return ['unparsable', ''];
    const rawParameterValue = match[2]!;
    parameters.set(key, rawParameterValue.startsWith('"') ? rawParameterValue.slice(1, -1).replace(/\\(.)/g, '$1') : rawParameterValue);
  }
  return [mediaType, parameters.get('charset') ?? ''];
}

function describeMediaType(mediaType: string): string { return mediaTypePattern.test(mediaType) ? mediaType : 'an unsupported media type'; }

function isRedirectStatus(status: number): boolean { return [301, 302, 303, 307, 308].includes(status); }

/** First value of a response header. */
export function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }

/** Validate an application URL and return the https URL to request; errors never repeat the value. */
export function parseRequestURL(value: string): GoURL {
  const trimmed = value.trim();
  if (trimmed === '') throw new TypeError('url is required');
  if (Buffer.byteLength(trimmed, 'utf8') > maxURLBytes) throw new TypeError('url must be at most 2048 bytes');
  for (const character of trimmed) {
    const code = character.codePointAt(0)!;
    if (code <= 0x20 || code === 0x7f) throw new TypeError('url cannot contain spaces or control characters');
  }
  let parsed: GoURL;
  try { parsed = parseGoURL(trimmed); } catch { throw new TypeError('url must be an absolute http or https URL'); }
  return upgradeToHTTPS(parsed);
}

function resolveRedirectLocation(current: GoURL, location: string): GoURL {
  const trimmed = location.trim();
  if (trimmed === '' || Buffer.byteLength(trimmed, 'utf8') > maxURLBytes) throw new TypeError('redirect location is missing or too long');
  return upgradeToHTTPS(current.parseReference(trimmed));
}

/** Return the https URL for an absolute http or https URL; plain http may name only port 80. */
function upgradeToHTTPS(parsed: GoURL): GoURL {
  if ((parsed.scheme !== 'http' && parsed.scheme !== 'https') || parsed.opaque !== '' || parsed.hostname() === '') {
    throw new TypeError('url must be an absolute http or https URL with a host');
  }
  if (parsed.user !== undefined) throw new TypeError('url cannot contain user information');
  const upgraded = parsed.clone();
  upgraded.fragment = ''; upgraded.rawFragment = '';
  if (upgraded.scheme === 'http') {
    const port = upgraded.port();
    if (port !== '' && port !== '80') throw new TypeError('an http url can name only port 80, because the connector requests it over https');
    upgraded.scheme = 'https';
    const hostname = upgraded.hostname();
    upgraded.host = hostname.includes(':') ? `[${hostname}]` : hostname;
  }
  return upgraded;
}

const linkLocalAddresses = blockList([['169.254.0.0', 16], ['fe80::', 10], ['224.0.0.0', 24], ['ff02::', 16]], ['fd00:ec2::254']);
const unicastExceptions = blockList([['224.0.0.0', 4], ['ff00::', 8]], ['0.0.0.0', '::']);
const nonPublicAddresses = blockList([['127.0.0.0', 8], ['10.0.0.0', 8], ['172.16.0.0', 12], ['192.168.0.0', 16], ['fc00::', 7], ['100.64.0.0', 10]], ['::1']);

/** Apply the Go connector's PublicHTTPS literal rules, so a refused IP literal is never requested. */
export function refusedAddressLiteralReason(hostname: string): string {
  const address = hostname.replace(/^\[|\]$/g, '').replace(/%.*$/, '');
  const family = isIP(address);
  if (family === 0) return '';
  const type = family === 4 ? 'ipv4' : 'ipv6';
  if (linkLocalAddresses.check(address, type)) return 'link-local addresses, which include the cloud metadata address, are never allowed';
  if (unicastExceptions.check(address, type)) return 'only unicast addresses are allowed';
  if (nonPublicAddresses.check(address, type)) return 'only public addresses are allowed';
  return '';
}

function blockList(subnets: Array<[string, number]>, addresses: string[]): BlockList {
  const list = new BlockList();
  for (const [network, prefix] of subnets) list.addSubnet(network, prefix, isIP(network) === 4 ? 'ipv4' : 'ipv6');
  for (const address of addresses) list.addAddress(address, isIP(address) === 4 ? 'ipv4' : 'ipv6');
  return list;
}

/** Send through the SDK's PublicHTTPS transport and name a missing host or a refused TLS handshake. */
export async function sendPublicHTTPS(request: ProviderRequest): Promise<ProviderResponse> {
  try { return await sendProviderRequest(request); }
  catch (error) {
    // Node can report a hostname mismatch after the socket connected, so any failure without a response is diagnosed.
    if (error instanceof ProviderTransportError && error.status === undefined) {
      const diagnosis = await diagnoseUnsentFailure(request.url);
      if (diagnosis) throw diagnosis;
    }
    throw error;
  }
}

// Only after a failed connection: resolve again, check every address like the policy, and try one handshake.
async function diagnoseUnsentFailure(rawURL: string): Promise<Error | undefined> {
  let url: URL;
  try { url = new URL(rawURL); } catch { return undefined; }
  const hostname = url.hostname.replace(/^\[|\]$/g, '');
  let addresses: Array<{ address: string }>;
  if (isIP(hostname) !== 0) addresses = [{ address: hostname }];
  else {
    try { addresses = await lookup(hostname, { all: true, verbatim: true }); }
    catch (error) { return (error as { code?: string }).code === 'ENOTFOUND' ? new HostNotFoundError() : undefined; }
  }
  const address = addresses[0]?.address;
  if (!address || addresses.some(each => refusedAddressLiteralReason(each.address) !== '')) return undefined;
  return new Promise(resolve => {
    const socket = connect({ host: address, port: Number(url.port || 443), ...(isIP(hostname) === 0 ? { servername: hostname } : {}), timeout: diagnosisTimeoutMs });
    socket.once('secureConnect', () => { socket.destroy(); resolve(undefined); });
    socket.once('timeout', () => { socket.destroy(); resolve(undefined); });
    socket.once('error', (error: Error & { code?: string }) => {
      socket.destroy();
      const code = error.code ?? '';
      if (code === 'ERR_TLS_CERT_ALTNAME_INVALID' || /CERT|SELF_SIGNED|UNABLE_TO_VERIFY|UNABLE_TO_GET_ISSUER/.test(code)) resolve(new TLSRefusalError(false));
      else if (code === 'ERR_SSL_WRONG_VERSION_NUMBER' || /wrong version number/i.test(error.message)) resolve(new TLSRefusalError(true));
      else resolve(undefined);
    });
  });
}

/** Decode an XML document with the charset its declaration names, or the HTTP charset when it names none. */
export function decodeXMLDocument(body: Uint8Array, httpCharset: string): XMLTokenizer {
  const prefix = Buffer.from(body.subarray(0, 256)).toString('latin1');
  const declarationEnd = prefix.indexOf('?>');
  const declaration = prefix.startsWith('<?xml') && declarationEnd > 0 ? prefix.slice(0, declarationEnd) : '';
  const declared = /encoding\s*=\s*["']([^"']*)["']/.exec(declaration)?.[1];
  let label = 'utf-8';
  if (declaration.includes('encoding')) label = declared ?? 'utf-8';
  else if (httpCharset !== '' && httpCharset.toLowerCase() !== 'utf-8') {
    try { new TextDecoder(httpCharset); label = httpCharset; } catch { label = 'utf-8'; }
  }
  const isUTF8 = label.toLowerCase() === 'utf-8';
  const decoder = new TextDecoder(label, { fatal: isUTF8 });
  return new XMLTokenizer(decoder.decode(body));
}

/** An invalidResponse failure with a protocol kind. */
export function invalidResponse(message: string): FetchFailure { return { class: 'invalidResponse', kind: 'PROTOCOL', message, retryAfterMs: 0 }; }
function tooLarge(expectedContent: string): FetchFailure {
  return { class: 'invalidResponse', kind: 'RESPONSE_TOO_LARGE', message: `the response is larger than the connection's limit for ${expectedContent}`, retryAfterMs: 0 };
}
function interruptedResponse(): FetchFailure { return { class: 'retry', kind: 'TRANSPORT', message: 'the response was interrupted', retryAfterMs: 0 }; }
/** A defect failure for invalid input. */
export function defectFailure(error: unknown): FetchFailure {
  return { class: 'defect', kind: 'VALIDATION', message: error instanceof Error ? error.message : 'the input is invalid', retryAfterMs: 0 };
}
