// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/**
 * An in-process Confluence site for pageChanged tests, ported from the Go confluencepagefake package. It serves
 * the accessible-resources lookup, CQL content search with the subset of CQL the Trigger sends, and page reads
 * in storage format. It checks a bearer token but never echoes it. Pass send to ConfluenceClient's send option.
 */

/** The bearer token the fake accepts. */
export const AccessToken = 'confluence-fake-' + 'access-token';
/** The default fake site ID. */
export const CloudID = '11223344-a1b2-4b33-8c44-def123456789';
const siteBase = 'https://ops.atlassian.net/wiki';
const spaceClause = /space = "([^"]+)"/;
const lastModifiedClause = /lastmodified >= "(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2})"/;
const cursorFormat = /^g(\d+)-o(\d+)-q(\d+)$/;

interface StoredPage { id: string; spaceKey: string; title: string; body: string; version: number; lastModifiedAt: number; createdAt: number }

/** One fake Confluence site whose clock returns Unix milliseconds. */
export class ConfluencePageSite {
  private readonly pages = new Map<string, StoredPage>();
  private nextPageId = 600000;
  private cursorGeneration = 0;
  private throttledSearches = 0;
  private retryAfterSeconds = 0;
  /** The status of every content search answered, in order. */ readonly searchStatuses: number[] = [];
  /** Every CQL query searched, in order. */ readonly cqlQueries: string[] = [];

  /** Create a site with cloudId and a clock. */
  constructor(readonly cloudId = CloudID, private readonly clock: () => number = () => Date.now()) {}

  /** Create a page at version 1 and return its ID. */
  publish(spaceKey: string, title: string, body: string): string {
    const now = this.clock();
    const page = { id: String(++this.nextPageId), spaceKey, title, body, version: 1, lastModifiedAt: now, createdAt: now };
    this.pages.set(page.id, page);
    return page.id;
  }
  /** Save the next version of a page with a new title and body. */
  edit(pageId: string, title: string, body: string): void { const page = this.pages.get(pageId)!; Object.assign(page, { title, body, version: page.version + 1, lastModifiedAt: this.clock() }); }
  /** Make the next count searches answer HTTP 429 with Retry-After in seconds. */
  throttleSearches(count: number, retryAfterSeconds: number): void { this.throttledSearches = count; this.retryAfterSeconds = retryAfterSeconds; }
  /** Make every search cursor issued so far answer HTTP 400. */ expireSearchCursors(): void { this.cursorGeneration++; }

  /** Serve one provider request. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const authorization = Object.entries(request.headers ?? {}).find(([name]) => name.toLowerCase() === 'authorization')?.[1];
    if (authorization !== `Bearer ${AccessToken}`) return json(401, { message: 'Unauthorized' });
    const url = new URL(request.url), prefix = `/ex/confluence/${this.cloudId}/wiki`;
    if (request.method === 'GET' && url.pathname === '/oauth/token/accessible-resources') return json(200, [{ id: this.cloudId, name: 'Ops', scopes: ['search:confluence', 'read:page:confluence'] }]);
    if (request.method === 'GET' && url.pathname === `${prefix}/rest/api/content/search`) return this.search(url);
    if (request.method === 'GET' && url.pathname.startsWith(`${prefix}/api/v2/pages/`)) return this.readPage(url.pathname.slice(`${prefix}/api/v2/pages/`.length));
    return json(404, { message: 'Not Found' });
  };

  private search(url: URL): ProviderResponse {
    if (this.throttledSearches > 0) {
      this.throttledSearches--;
      this.searchStatuses.push(429);
      return json(429, { message: 'Rate limit exceeded' }, { 'retry-after': String(this.retryAfterSeconds) });
    }
    const cql = url.searchParams.get('cql') ?? '';
    this.cqlQueries.push(cql);
    if (!cql.startsWith('type = page') || !cql.endsWith('ORDER BY lastmodified DESC')) { this.searchStatuses.push(400); return json(400, { message: 'unsupported CQL' }); }
    const space = spaceClause.exec(cql)?.[1];
    const bound = lastModifiedClause.exec(cql);
    const lowerBound = bound ? Date.parse(`${bound[1]}T${bound[2]}:00Z`) : -Infinity;
    const matches = [...this.pages.values()].filter(page => (!space || page.spaceKey === space) && page.lastModifiedAt >= lowerBound)
      .sort((left, right) => right.lastModifiedAt - left.lastModifiedAt || (left.id < right.id ? -1 : 1));
    const limit = Number(url.searchParams.get('limit')) || 25;
    let offset = 0;
    const cursor = url.searchParams.get('cursor');
    if (cursor) {
      const parsed = cursorFormat.exec(cursor);
      if (!parsed || Number(parsed[1]) !== this.cursorGeneration || Number(parsed[3]) !== cql.length) { this.searchStatuses.push(400); return json(400, { message: 'Invalid cursor' }); }
      offset = Number(parsed[2]);
    }
    const end = Math.min(offset + limit, matches.length);
    const links: Record<string, string> = { base: siteBase };
    if (end < matches.length) links.next = `/rest/api/content/search?cql=${cql.replace(/ /g, '+')}&cursor=g${this.cursorGeneration}-o${end}-q${cql.length}`;
    this.searchStatuses.push(200);
    return json(200, { results: matches.slice(offset, end).map(page => ({
      id: page.id, type: 'page', status: 'current', title: page.title, space: { id: 98306, key: page.spaceKey },
      version: { number: page.version, when: new Date(page.lastModifiedAt).toISOString() }, _links: { webui: `/spaces/${page.spaceKey}/pages/${page.id}` },
    })), _links: links });
  }

  private readPage(pageId: string): ProviderResponse {
    const page = this.pages.get(pageId);
    if (!page) return json(404, { message: 'Not Found' });
    return json(200, {
      id: page.id, status: 'current', title: page.title, spaceId: '98306', createdAt: new Date(page.createdAt).toISOString(),
      version: { number: page.version, createdAt: new Date(page.lastModifiedAt).toISOString() },
      body: { storage: { representation: 'storage', value: `<p>${escapeHTML(page.body)}</p>` } },
      _links: { webui: `/spaces/${page.spaceKey}/pages/${page.id}`, base: siteBase },
    });
  }
}

function escapeHTML(text: string): string { return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }
function json(status: number, payload: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers: { 'content-type': 'application/json', ...headers }, body: Buffer.from(JSON.stringify(payload)) };
}
