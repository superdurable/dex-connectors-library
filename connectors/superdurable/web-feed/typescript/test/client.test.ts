// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { gzipSync } from 'node:zlib';
import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { WebFeedClient } from '../src/client.js';
import { refusedAddressLiteralReason } from '../src/document.js';
import { parseGoURL } from '../src/go_url.js';
import { collapseWhitespace, plainTextFromHTML, truncateCharacters } from '../src/text.js';
import { optionalTimestamp } from '../src/timestamp.js';

const fixedClock = new Date('2026-11-10T10:00:00Z');
const call: Call = {
  context: { flowId: 'web-feed-unit', cancellationSignal: new AbortController().signal } as Call['context'],
  id: 'call-1', connection: { provider: 'web-feed', name: 'web-feed-public' }, operation: { connectorId: 'web-feed', operationId: 'readFeed' },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

type Route = (request: ProviderRequest) => ProviderResponse | Error;
function clientFor(routes: Record<string, Route>, config = {}) {
  const requests: ProviderRequest[] = [];
  const client = new WebFeedClient(config, {
    now: () => fixedClock,
    send: async request => {
      requests.push(request);
      const route = routes[request.url];
      if (!route) return { status: 404, headers: {}, body: new Uint8Array() };
      const answer = route(request);
      if (answer instanceof Error) throw answer;
      return answer;
    },
  });
  return { client, requests };
}
const ok = (contentType: string, body: string | Uint8Array, headers: Record<string, string> = {}): Route => () =>
  ({ status: 200, headers: { 'content-type': contentType, ...headers }, body: typeof body === 'string' ? Buffer.from(body) : body });

describe('readFeed', () => {
  it('decompresses gzip within the limit and rejects a larger body', async () => {
    const { client } = clientFor({
      'https://blog.example.com/feed.xml.gz': ok('application/gzip', gzipSync('<rss><channel><title>Compressed</title></channel></rss>')),
      'https://blog.example.com/bomb.xml.gz': ok('application/gzip', gzipSync('<rss>' + ' '.repeat(1 << 20) + '</rss>')),
    }, { maxDocumentBytes: 64 << 10 });
    const read = await client.readFeed().invoke(call, { url: 'https://blog.example.com/feed.xml.gz' });
    expect(read).toMatchObject({ kind: 'branch', branch: 'read', value: { title: 'Compressed' } });
    const bomb = await client.readFeed().invoke(call, { url: 'https://blog.example.com/bomb.xml.gz' });
    expect(bomb).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
  });

  it('decodes the declared and the HTTP charset', async () => {
    const latin = Buffer.concat([Buffer.from('<?xml version="1.0" encoding="ISO-8859-1"?><rss version="2.0"><channel><title>Caf'), Buffer.from([0xe9]), Buffer.from('</title></channel></rss>')]);
    const windows = Buffer.concat([Buffer.from('<rss version="2.0"><channel><title>Na'), Buffer.from([0xef]), Buffer.from('ve</title></channel></rss>')]);
    const { client } = clientFor({
      'https://blog.example.com/latin1.xml': ok('text/xml', latin),
      'https://blog.example.com/windows.xml': ok('text/xml; charset=windows-1252', windows),
    });
    expect(await client.readFeed().invoke(call, { url: 'https://blog.example.com/latin1.xml' })).toMatchObject({ branch: 'read', value: { title: 'Café' } });
    expect(await client.readFeed().invoke(call, { url: 'https://blog.example.com/windows.xml' })).toMatchObject({ branch: 'read', value: { title: 'Naïve', items: [] } });
  });

  it('refuses private literals at the first hop and after a redirect without sending', async () => {
    const { client, requests } = clientFor({
      'https://blog.example.com/to-literal': () => ({ status: 302, headers: { location: 'https://[::ffff:169.254.169.254]/latest' }, body: new Uint8Array() }),
    });
    for (const url of ['https://10.0.0.5/feed', 'https://[::1]/feed', 'http://169.254.169.254/latest', 'https://100.64.0.9/', 'https://[fd12:3456::1]/']) {
      expect(await client.readFeed().invoke(call, { url })).toMatchObject({ branch: 'unreachable', failure: { kind: 'VALIDATION' } });
    }
    expect(requests).toHaveLength(0);
    const redirected = await client.readFeed().invoke(call, { url: 'https://blog.example.com/to-literal' });
    expect(redirected).toMatchObject({ branch: 'unreachable', failure: { message: expect.stringContaining('link-local') } });
    expect(requests).toHaveLength(1);
  });

  it('retries transient failures and keeps a provider delay', async () => {
    const { client } = clientFor({
      'https://blog.example.com/429': () => ({ status: 429, headers: { 'retry-after': '7' }, body: new Uint8Array() }),
      'https://blog.example.com/503': () => ({ status: 503, headers: { 'retry-after': new Date(fixedClock.getTime() + 3000).toUTCString() }, body: new Uint8Array() }),
      'https://blog.example.com/dropped': () => new ProviderTransportError(false),
    });
    expect(await client.readFeed().invoke(call, { url: 'https://blog.example.com/429' })).toMatchObject({ kind: 'retry', retryAfterMs: 7000, failure: { kind: 'RATE_LIMIT' } });
    expect(await client.readFeed().invoke(call, { url: 'https://blog.example.com/503' })).toMatchObject({ kind: 'retry', retryAfterMs: 3000 });
    expect(await client.readFeed().invoke(call, { url: 'https://blog.example.com/dropped' })).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
  });

  it('rejects invalid input before sending', async () => {
    const { client, requests } = clientFor({});
    for (const input of [{ url: '' }, { url: 'ftp://blog.example.com/' }, { url: 'http://blog.example.com:8080/' }, { url: 'https://user:secret@blog.example.com/' },
      { url: 'https://blog.example.com/', limit: 201 }, { url: 'https://blog.example.com/', etag: 'café' }]) {
      const attempt = await client.readFeed().invoke(call, input);
      expect(attempt).toMatchObject({ branch: 'defect', failure: { kind: 'VALIDATION' } });
      expect(JSON.stringify(attempt)).not.toContain('secret');
    }
    expect(requests).toHaveLength(0);
  });
});

describe('the default transport', () => {
  it('refuses a named host that resolves to loopback', async () => {
    const client = new WebFeedClient({}, { now: () => fixedClock });
    const attempt = await client.readFeed().invoke(call, { url: 'https://localhost/feed.xml' });
    expect(attempt).toMatchObject({ branch: 'unreachable', failure: { kind: 'VALIDATION' } });
  });
});

describe('page reads', () => {
  it('reads only the page prefix and reports truncation', async () => {
    const page = '<html><head><title>Long page</title></head><body>' + '<p>text</p>'.repeat(1000) + '<meta name="description" content="too late"></body></html>';
    const { client } = clientFor({ 'https://www.example.com/long': ok('text/html', page) }, { maxPageBytes: 2048 });
    const attempt = await client.readPageMetadata().invoke(call, { url: 'https://www.example.com/long' });
    expect(attempt).toMatchObject({ branch: 'read', value: { title: 'Long page', isTruncated: true } });
    expect(attempt.kind === 'branch' ? attempt.value.description : 'retry').toBeUndefined();
  });

  it('decodes a page whose meta tag names its charset', async () => {
    const page = Buffer.concat([Buffer.from('<html><head><meta charset="windows-1252"><title>Caf'), Buffer.from([0xe9]), Buffer.from('</title><link rel="alternate" type="application/atom+xml" href="/feed"></head></html>')]);
    const { client } = clientFor({ 'https://www.example.com/': ok('text/html', page) });
    expect(await client.discoverFeeds().invoke(call, { url: 'https://www.example.com/' })).toMatchObject({
      branch: 'discovered', value: { pageTitle: 'Café', feeds: [{ url: 'https://www.example.com/feed', mediaType: 'application/atom+xml' }] },
    });
  });
});

describe('Go-compatible helpers', () => {
  it('parses the timestamp spellings the Go connector reads', () => {
    expect(optionalTimestamp('Tue, 10 Nov 2026 04:30:00 EST')).toBe('2026-11-10T09:30:00Z');
    expect(optionalTimestamp('Tuesday, 10-Nov-26 09:30:00 GMT')).toBe('2026-11-10T09:30:00Z');
    expect(optionalTimestamp('2026-11-10T09:30:00.123456+01:00')).toBe('2026-11-10T08:30:00.123456Z');
    expect(optionalTimestamp('2026-11-10 09:30:00 -0800 PST')).toBe('2026-11-10T17:30:00Z');
    expect(optionalTimestamp('10 Sept 2026 09:30 GMT')).toBe('2026-09-10T09:30:00Z');
    expect(optionalTimestamp('Tue Nov 10 09:30:00 2026')).toBe('2026-11-10T09:30:00Z');
    expect(optionalTimestamp('2026')).toBe('2026-01-01T00:00:00Z');
    for (const missing of ['', 'yesterday', '0001-01-01T00:00:00Z', 'Thu, 01 Jan 1970 00:00:00 GMT', '2026-13-01']) expect(optionalTimestamp(missing)).toBeUndefined();
  });

  it('prints and resolves URLs as Go net/url does', () => {
    const base = parseGoURL('https://Example.com');
    expect(base.toString()).toBe('https://Example.com');
    expect(base.parseReference('a b/../c?x=1').toString()).toBe('https://Example.com/c?x=1');
    expect(parseGoURL('https://example.com/blog/').parseReference('../feed.xml#top').toString()).toBe('https://example.com/feed.xml#top');
    expect(parseGoURL('https://example.com/%7Euser/caf%C3%A9').toString()).toBe('https://example.com/%7Euser/caf%C3%A9');
    expect(parseGoURL('https://example.com/a%2Fb/ c').toString()).toBe('https://example.com/a/b/%20c');
  });

  it('matches the Go literal refusal rules', () => {
    expect(refusedAddressLiteralReason('::ffff:10.0.0.5')).toBe('only public addresses are allowed');
    expect(refusedAddressLiteralReason('fd00:ec2::254')).toContain('link-local');
    expect(refusedAddressLiteralReason('224.0.0.1')).toContain('link-local');
    expect(refusedAddressLiteralReason('224.1.0.1')).toBe('only unicast addresses are allowed');
    expect(refusedAddressLiteralReason('203.0.113.10')).toBe('');
    expect(refusedAddressLiteralReason('blog.example.com')).toBe('');
  });

  it('bounds and cleans provider text', () => {
    expect(plainTextFromHTML('<b>Bo</b>ld <i>word</i><script>x()</script><p>next</p>')).toBe('Bold word next');
    expect(truncateCharacters('one two three four', 10)).toBe('one two…');
    expect(truncateCharacters('日本語のテキスト', 4)).toBe('日本語…');
    expect(collapseWhitespace(' a\u00a0b\u0085c\ufeffd ')).toBe('a b c\ufeffd');
  });
});
