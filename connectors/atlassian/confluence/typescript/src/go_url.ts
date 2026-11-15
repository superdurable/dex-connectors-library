// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Go parses Confluence's _links.next with net/url Parse and Query().Get; WHATWG URL accepts links,
// query pairs, and escapes Go rejects, so this mirrors Go's rules.

const hexDigits = new Set(Buffer.from('0123456789abcdefABCDEF'));
const userinfoBytes = new Set(Buffer.from("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._:~!$&'()*+,;=%@"));
const hostBytes = new Set(Buffer.from('abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~!$&\'()*+,;=:[]<>"'));
const cursorPattern = /^[\x21-\x7e]{1,4096}$/;

class GoURLError extends Error {}

/** Return the cursor of a next link, or '' for none; throws where Go's readNextCursor returns an error. */
export function readNextCursor(nextLink: string): string {
  if (!nextLink) return '';
  const cursor = queryGet(parseRawQuery(Buffer.from(nextLink, 'utf8')), 'cursor').toString('latin1');
  if (!cursorPattern.test(cursor)) throw new TypeError('cursor is missing or invalid');
  return cursor;
}

function unescape(value: Buffer, options: { isPlusSpace?: boolean; isHost?: boolean } = {}): Buffer {
  const decoded: number[] = [];
  for (let index = 0; index < value.length;) {
    const byte = value[index]!;
    if (byte === 0x25) {
      if (index + 2 >= value.length || !hexDigits.has(value[index + 1]!) || !hexDigits.has(value[index + 2]!)) throw new GoURLError('invalid URL escape');
      const high = Number.parseInt(String.fromCharCode(value[index + 1]!), 16);
      if (options.isHost && high < 8 && value.subarray(index, index + 3).toString('latin1') !== '%25') throw new GoURLError('invalid URL escape in host');
      decoded.push(Number.parseInt(value.subarray(index + 1, index + 3).toString('latin1'), 16));
      index += 3;
      continue;
    }
    if (options.isHost && byte < 0x80 && !hostBytes.has(byte)) throw new GoURLError('invalid character in host name');
    decoded.push(options.isPlusSpace && byte === 0x2b ? 0x20 : byte);
    index++;
  }
  return Buffer.from(decoded);
}

/** Validate raw as Go's url.Parse does and return its raw query. */
function parseRawQuery(raw: Buffer): Buffer {
  const hash = raw.indexOf(0x23);
  const link = hash < 0 ? raw : raw.subarray(0, hash);
  const fragment = hash < 0 ? Buffer.alloc(0) : raw.subarray(hash + 1);
  if (link.some(byte => byte < 0x20 || byte === 0x7f)) throw new GoURLError('invalid control character in URL');
  const [scheme, afterScheme] = splitScheme(link);
  let rest: Buffer = afterScheme, query: Buffer = Buffer.alloc(0);
  const questionMarks = rest.filter(byte => byte === 0x3f).length;
  if (rest.length > 0 && rest[rest.length - 1] === 0x3f && questionMarks === 1) rest = rest.subarray(0, -1);
  else if (questionMarks > 0) { const question = rest.indexOf(0x3f); query = rest.subarray(question + 1); rest = rest.subarray(0, question); }
  if (rest[0] !== 0x2f) {
    if (scheme.length > 0) { unescape(fragment); return query; } // Go leaves an opaque URL's opaque part escaped.
    const slash = rest.indexOf(0x2f);
    if ((slash < 0 ? rest : rest.subarray(0, slash)).includes(0x3a)) throw new GoURLError('first path segment in URL cannot contain colon');
  }
  if (rest[0] === 0x2f && rest[1] === 0x2f && (scheme.length > 0 || rest[2] !== 0x2f)) {
    const slash = rest.indexOf(0x2f, 2);
    parseAuthority(slash < 0 ? rest.subarray(2) : rest.subarray(2, slash));
    rest = slash < 0 ? Buffer.alloc(0) : rest.subarray(slash);
  }
  unescape(rest);
  unescape(fragment);
  return query;
}

function splitScheme(link: Buffer): [Buffer, Buffer] {
  for (let index = 0; index < link.length; index++) {
    const byte = link[index]!;
    if ((byte >= 0x61 && byte <= 0x7a) || (byte >= 0x41 && byte <= 0x5a)) continue;
    if ((byte >= 0x30 && byte <= 0x39) || byte === 0x2b || byte === 0x2d || byte === 0x2e) { if (index === 0) return [Buffer.alloc(0), link]; continue; }
    if (byte === 0x3a) { if (index === 0) throw new GoURLError('missing protocol scheme'); return [link.subarray(0, index), link.subarray(index + 1)]; }
    return [Buffer.alloc(0), link];
  }
  return [Buffer.alloc(0), link];
}

function parseAuthority(authority: Buffer): void {
  const at = authority.lastIndexOf(0x40);
  const host = at < 0 ? authority : authority.subarray(at + 1);
  if (at >= 0) {
    const userinfo = authority.subarray(0, at);
    if (userinfo.some(byte => !userinfoBytes.has(byte))) throw new GoURLError('invalid userinfo');
    const colon = userinfo.indexOf(0x3a);
    unescape(colon < 0 ? userinfo : userinfo.subarray(0, colon));
    if (colon >= 0) unescape(userinfo.subarray(colon + 1));
  }
  if (host[0] === 0x5b) {
    const close = host.lastIndexOf(0x5d);
    if (close < 0) throw new GoURLError("missing ']' in host");
    checkPort(host.subarray(close + 1));
    unescape(host.subarray(0, close + 1), { isHost: true });
    return;
  }
  const colon = host.lastIndexOf(0x3a);
  if (colon >= 0) checkPort(host.subarray(colon));
  unescape(host, { isHost: true });
}

function checkPort(port: Buffer): void {
  if (port.length === 0) return;
  if (port[0] !== 0x3a || port.subarray(1).some(byte => byte < 0x30 || byte > 0x39)) throw new GoURLError('invalid port');
}

/** Return the first value of key as Go's ParseQuery and Values.Get do, skipping malformed pairs. */
function queryGet(query: Buffer, key: string): Buffer {
  let start = 0;
  while (start <= query.length) {
    const ampersand = query.indexOf(0x26, start);
    const pair = query.subarray(start, ampersand < 0 ? query.length : ampersand);
    start = ampersand < 0 ? query.length + 1 : ampersand + 1;
    if (pair.length === 0 || pair.includes(0x3b)) continue;
    const equals = pair.indexOf(0x3d);
    try {
      const name = unescape(equals < 0 ? pair : pair.subarray(0, equals), { isPlusSpace: true });
      const value = unescape(equals < 0 ? Buffer.alloc(0) : pair.subarray(equals + 1), { isPlusSpace: true });
      if (name.toString('latin1') === key) return value;
    } catch (error) {
      if (!(error instanceof GoURLError)) throw error;
    }
  }
  return Buffer.alloc(0);
}
