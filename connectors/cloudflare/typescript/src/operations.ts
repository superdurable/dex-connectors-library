// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { CloudflareClient, CloudflareExchange } from './client.js';
import { ListZonesDefinition, PurgeCacheByURLsDefinition } from './generated/definitions.js';
import type { CachePurge, ListZonesInput, PurgeCacheByURLsInput, ZonesPage } from './generated/models.js';

const emptyZonesPage: ZonesPage = { success: false, result: [] };
const emptyCachePurge: CachePurge = { success: false, result: { id: '' } };

/** Read one Cloudflare zone page. */
export class ListZonesOperation implements Query<ListZonesInput, ZonesPage> {
  /** Bind the client. */ constructor(private readonly client: CloudflareClient) {}
  /** Return the generated operation contract. */ definition() { return ListZonesDefinition; }
  /** Send one zone query and classify its outcome. */
  async invoke(call: Call, input: ListZonesInput): Promise<QueryAttempt<ZonesPage>> {
    void input;
    const exchange = await this.client.exchange(call, 'listZones', 'GET', '/zones');
    if (!('response' in exchange)) return queryOutcome(exchange, emptyZonesPage);
    try {
      const value = decodeZonesPage(exchange.response.body);
      return newQueryBranch('listed', value);
    } catch { return newQueryBranch('invalidResponse', emptyZonesPage, {}, failure('listZones', 'PROTOCOL', 'response was malformed')); }
  }
}

/** Purge a set of URLs from one Cloudflare zone. */
export class PurgeCacheByURLsOperation implements Mutation<PurgeCacheByURLsInput, CachePurge> {
  /** Bind the client. */ constructor(private readonly client: CloudflareClient) {}
  /** Return the generated operation contract. */ definition() { return PurgeCacheByURLsDefinition; }
  /** Identify repeat-safe purges by the stable Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and send one URL purge. */
  async invoke(call: Call, input: PurgeCacheByURLsInput): Promise<MutationAttempt<CachePurge>> {
    if (!input.zoneId?.trim() || !input.urls || input.urls.length < 1 || input.urls.length > 30) {
      return newMutationBranch('defect', emptyCachePurge, {}, failure('purgeCacheByURLs', 'VALIDATION', 'zoneId and 1 to 30 URLs are required'));
    }
    if (!input.urls.every(isAbsoluteHTTPURL)) {
      return newMutationBranch('defect', emptyCachePurge, {}, failure('purgeCacheByURLs', 'VALIDATION', 'each URL must be an absolute HTTP or HTTPS URL'));
    }
    const path = `/zones/${goPathEscape(input.zoneId)}/purge_cache`;
    const body = Buffer.from(JSON.stringify({ files: input.urls }), 'utf8');
    const exchange = await this.client.exchange(call, 'purgeCacheByURLs', 'POST', path, body, true);
    if (!('response' in exchange)) return mutationOutcome(exchange, emptyCachePurge);
    try {
      const value = decodeCachePurge(exchange.response.body);
      return newMutationBranch('purged', value);
    } catch { return newMutationBranch('invalidResponse', emptyCachePurge, {}, failure('purgeCacheByURLs', 'PROTOCOL', 'response was malformed')); }
  }
}

function queryOutcome(exchange: Exclude<CloudflareExchange, { response: unknown }>, empty: ZonesPage): QueryAttempt<ZonesPage> {
  if (exchange.disposition === 'retry') return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  return newQueryBranch(exchange.disposition === 'notFound' ? 'providerRejected' : exchange.disposition, empty, {}, exchange.failure);
}
function mutationOutcome(exchange: Exclude<CloudflareExchange, { response: unknown }>, empty: CachePurge): MutationAttempt<CachePurge> {
  if (exchange.disposition === 'retry') return newMutationRetry(exchange.failure, exchange.retryAfterMs);
  return newMutationBranch(exchange.disposition, empty, {}, exchange.failure);
}
function failure(operation: string, kind: 'VALIDATION' | 'PROTOCOL', message: string) {
  return { kind, provider: 'cloudflare', operation, message } as const;
}
function isAbsoluteHTTPURL(rawURL: string): boolean {
  if ([...rawURL].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) return false;
  if (/%(?![0-9a-fA-F]{2})/.test(rawURL)) return false;
  const authority = /^(https?):\/\/([^/?#]+)/.exec(rawURL);
  const scheme = authority?.[1];
  const host = authority?.[2];
  if (!scheme || !host || host.includes('@')) return false;
  const portStart = host.startsWith('[') ? host.lastIndexOf(']') + 1 : host.indexOf(':');
  if (host.startsWith('[') && portStart === 0) return false;
  const hostname = portStart > 0 ? host.slice(0, portStart) : host;
  const port = portStart > 0 ? host.slice(portStart) : '';
  if (port && !/^:[0-9]*$/.test(port)) return false;
  try {
    const url = new URL(`${scheme}://${hostname}/`);
    return !!url.host;
  } catch { return false; }
}

function goPathEscape(value: string): string {
  let escaped = '';
  for (const byte of Buffer.from(value, 'utf8')) {
    const character = String.fromCharCode(byte);
    escaped += /[A-Za-z0-9._~$&+:=@-]/.test(character) ? character : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }
  return escaped;
}

function decodeZonesPage(body: Uint8Array): ZonesPage {
  const document = parseObject(body);
  if (document.success !== true || !Array.isArray(document.result)) throw new Error('missing successful zone result');
  return { success: true, result: document.result.map(item => {
    const zone = item === null ? {} : requireObject(item);
    return { id: goString(zone.id), name: goString(zone.name), status: goString(zone.status) };
  }) };
}

function decodeCachePurge(body: Uint8Array): CachePurge {
  const document = parseObject(body);
  if (document.success !== true) throw new Error('purge was not accepted');
  const result = document.result == null ? {} : requireObject(document.result);
  return { success: true, result: { id: goString(result.id) } };
}

function parseObject(body: Uint8Array): Record<string, unknown> {
  return requireObject(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
}
function requireObject(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('response is not an object');
  return value as Record<string, unknown>;
}
function goString(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value !== 'string') throw new Error('response field is not a string');
  return value;
}
