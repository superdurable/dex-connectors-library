// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Go's net/url Parse and URL.String for the https URLs the profileChanged Trigger keeps, so a picture URL
// is stored and hashed as the Go connector stores it. Strings below are byte strings, one character per byte.
import { isIPv6 } from 'node:net';
import { goBoundedString } from './go_text.js';

type Encoding = 'path' | 'host' | 'fragment';

interface ParsedURL { scheme: string; host: string; hasUser: boolean; path: string; rawPath: string; forceQuery: boolean; rawQuery: string; fragment: string; rawFragment: string }

const isAlphanumeric = (code: number) => (code >= 0x61 && code <= 0x7a) || (code >= 0x41 && code <= 0x5a) || (code >= 0x30 && code <= 0x39);
const isHex = (code: number) => (code >= 0x30 && code <= 0x39) || (code >= 0x61 && code <= 0x66) || (code >= 0x41 && code <= 0x46);

function shouldEscape(code: number, mode: Encoding): boolean {
  if (isAlphanumeric(code)) return false;
  const character = String.fromCharCode(code);
  if (mode === 'host' && '!$&\'()*+,;=:[]<>"'.includes(character)) return false;
  if ('-_.~'.includes(character)) return false;
  if ('$&+,/:;=?@'.includes(character)) {
    if (mode === 'path') return character === '?';
    if (mode === 'fragment') return false;
  }
  return !(mode === 'fragment' && '!()*'.includes(character));
}

/** Go's unescape, or undefined for an invalid escape or host byte. */
function unescape(text: string, mode: Encoding): string | undefined {
  let unescaped = '';
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index);
    if (code === 0x25) {
      if (index + 2 >= text.length || !isHex(text.charCodeAt(index + 1)) || !isHex(text.charCodeAt(index + 2))) return undefined;
      const value = parseInt(text.slice(index + 1, index + 3), 16);
      // A host may escape only non-ASCII bytes, and %25 for an IPv6 zone.
      if (mode === 'host' && value < 0x80 && value !== 0x25) return undefined;
      unescaped += String.fromCharCode(value);
      index += 2;
    } else {
      if (mode === 'host' && code < 0x80 && shouldEscape(code, mode)) return undefined;
      unescaped += text[index];
    }
  }
  return unescaped;
}

function escape(text: string, mode: Encoding): string {
  let escaped = '';
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index);
    escaped += shouldEscape(code, mode) ? `%${code.toString(16).toUpperCase().padStart(2, '0')}` : text[index];
  }
  return escaped;
}

function isValidEncoded(text: string, mode: Encoding): boolean {
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index);
    if (!'!$&\'()*+,;=:@[]%'.includes(text[index]!) && shouldEscape(code, mode)) return false;
  }
  return true;
}

const isValidOptionalPort = (port: string) => port === '' || /^:[0-9]*$/.test(port);

function parseHost(host: string): string | undefined {
  if (host.includes('[')) {
    const open = host.lastIndexOf('['), close = host.lastIndexOf(']');
    if (close < 0 || !isValidOptionalPort(host.slice(close + 1))) return undefined;
    const inner = host.slice(open + 1, close), zoneIndex = inner.indexOf('%25');
    // Go checks the address part with netip.ParseAddr and keeps an escaped %25 zone identifier.
    const hostname = unescape(zoneIndex < 0 ? inner : inner.slice(0, zoneIndex), 'host');
    const zone = zoneIndex < 0 ? '' : unescape(inner.slice(zoneIndex + 3), 'host');
    if (hostname === undefined || zone === undefined || hostname.includes('%') || !isIPv6(hostname)) return undefined;
    return `[${hostname}${zoneIndex < 0 ? '' : `%${zone}`}]${host.slice(close + 1)}`;
  }
  const colon = host.lastIndexOf(':');
  if (colon >= 0 && !isValidOptionalPort(host.slice(colon))) return undefined;
  return unescape(host, 'host');
}

function hostname(host: string): string {
  const colon = host.lastIndexOf(':');
  let name = colon >= 0 && isValidOptionalPort(host.slice(colon)) ? host.slice(0, colon) : host;
  if (name.startsWith('[') && name.endsWith(']')) name = name.slice(1, -1);
  return name;
}

/** Go's url.Parse of an absolute URL with an authority; undefined for anything else or an error. */
function parseAbsoluteURL(raw: string): ParsedURL | undefined {
  const hash = raw.indexOf('#');
  const beforeFragment = hash < 0 ? raw : raw.slice(0, hash), rawFragmentText = hash < 0 ? '' : raw.slice(hash + 1);
  if ([...beforeFragment].some(character => character.charCodeAt(0) < 0x20 || character.charCodeAt(0) === 0x7f)) return undefined;
  const scheme = /^([A-Za-z][A-Za-z0-9+.-]*):/.exec(beforeFragment)?.[1];
  if (!scheme) return undefined;
  let rest = beforeFragment.slice(scheme.length + 1), rawQuery = '', forceQuery = false;
  if (rest.endsWith('?') && rest.indexOf('?') === rest.length - 1) { forceQuery = true; rest = rest.slice(0, -1); }
  else if (rest.includes('?')) { rawQuery = rest.slice(rest.indexOf('?') + 1); rest = rest.slice(0, rest.indexOf('?')); }
  if (!rest.startsWith('//')) return undefined;
  let authority = rest.slice(2);
  const slash = authority.indexOf('/');
  rest = slash < 0 ? '' : authority.slice(slash);
  if (slash >= 0) authority = authority.slice(0, slash);
  const at = authority.lastIndexOf('@');
  const host = parseHost(at < 0 ? authority : authority.slice(at + 1));
  const path = unescape(rest, 'path');
  if (host === undefined || path === undefined) return undefined;
  const parsed: ParsedURL = {
    scheme: scheme.toLowerCase(), host, hasUser: at >= 0, path, rawPath: escape(path, 'path') === rest ? '' : rest,
    forceQuery, rawQuery, fragment: '', rawFragment: '',
  };
  if (rawFragmentText) {
    const fragment = unescape(rawFragmentText, 'fragment');
    if (fragment === undefined) return undefined;
    parsed.fragment = fragment;
    parsed.rawFragment = escape(fragment, 'fragment') === rawFragmentText ? '' : rawFragmentText;
  }
  return parsed;
}

function escapedPath(url: ParsedURL): string {
  if (url.rawPath && isValidEncoded(url.rawPath, 'path') && unescape(url.rawPath, 'path') === url.path) return url.rawPath;
  return url.path === '*' ? '*' : escape(url.path, 'path');
}

function escapedFragment(url: ParsedURL): string {
  if (url.rawFragment && isValidEncoded(url.rawFragment, 'fragment') && unescape(url.rawFragment, 'fragment') === url.fragment) return url.rawFragment;
  return escape(url.fragment, 'fragment');
}

function urlString(url: ParsedURL): string {
  let text = `${url.scheme}:`;
  if (url.host || url.path) text += '//';
  text += escape(url.host, 'host');
  const path = escapedPath(url);
  if (path && !path.startsWith('/') && url.host) text += '/';
  text += path;
  if (url.forceQuery || url.rawQuery) text += `?${url.rawQuery}`;
  if (url.fragment) text += `#${escapedFragment(url)}`;
  return text;
}

const toBytes = (text: string) => Buffer.from(text, 'utf8').toString('latin1');
const fromBytes = (bytes: string) => Buffer.from(bytes, 'latin1').toString('utf8');

/** Go's safeURL: the bounded URL as url.URL.String writes it when it is https with a host and no user, else ''. */
export function goSafeURL(value: string): string {
  const parsed = parseAbsoluteURL(toBytes(goBoundedString(value, 2048)));
  if (!parsed || parsed.scheme !== 'https' || parsed.hasUser || !hostname(parsed.host)) return '';
  return fromBytes(urlString(parsed));
}

/** Go's pictureIdentity of a URL goSafeURL wrote: the URL without its query and fragment, keeping a bare '?'. */
export function pictureIdentity(pictureURL: string): string {
  const withoutFragment = pictureURL.split('#', 1)[0]!;
  const question = withoutFragment.indexOf('?');
  if (question < 0 || (question === withoutFragment.length - 1)) return withoutFragment;
  return withoutFragment.slice(0, question);
}
