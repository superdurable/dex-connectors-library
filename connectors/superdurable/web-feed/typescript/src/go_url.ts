// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// A port of the parts of Go net/url that the Go connector uses, so both languages resolve and print URLs
// byte for byte alike. WHATWG URL normalizes hosts and paths differently, which would change results.

const enum Encoding { Path = 1, PathSegment, Host, Zone, UserPassword, QueryComponent, Fragment }

/** User information of a parsed URL; the connector refuses URLs that carry it. */
interface UserInformation { username: string; password: string; hasPassword: boolean }

/** Parsed URL with Go net/url field semantics. */
export class GoURL {
  scheme = '';
  opaque = '';
  user: UserInformation | undefined;
  host = '';
  path = '';
  rawPath = '';
  omitHost = false;
  forceQuery = false;
  rawQuery = '';
  fragment = '';
  rawFragment = '';

  /** Copy every field into a new value. */
  clone(): GoURL { return Object.assign(new GoURL(), this, { user: this.user ? { ...this.user } : undefined }); }

  /** Host without the port and without IPv6 brackets, as Go URL.Hostname. */
  hostname(): string { return splitHostPort(this.host)[0]; }

  /** Port of the host, as Go URL.Port. */
  port(): string { return splitHostPort(this.host)[1]; }

  /** Escaped path, as Go URL.EscapedPath. */
  escapedPath(): string {
    if (this.rawPath !== '' && validEncoded(this.rawPath, Encoding.Path)) {
      const unescaped = tryUnescape(this.rawPath, Encoding.Path);
      if (unescaped !== undefined && unescaped === this.path) return this.rawPath;
    }
    if (this.path === '*') return '*';
    return escape(this.path, Encoding.Path);
  }

  /** Escaped fragment, as Go URL.EscapedFragment. */
  escapedFragment(): string {
    if (this.rawFragment !== '' && validEncoded(this.rawFragment, Encoding.Fragment)) {
      const unescaped = tryUnescape(this.rawFragment, Encoding.Fragment);
      if (unescaped !== undefined && unescaped === this.fragment) return this.rawFragment;
    }
    return escape(this.fragment, Encoding.Fragment);
  }

  /** Reassemble the URL, as Go URL.String. */
  toString(): string {
    let text = '';
    if (this.scheme !== '') text += this.scheme + ':';
    if (this.opaque !== '') text += this.opaque;
    else {
      if (this.scheme !== '' || this.host !== '' || this.user !== undefined) {
        if (!(this.omitHost && this.host === '' && this.user === undefined)) {
          if (this.host !== '' || this.path !== '' || this.user !== undefined) text += '//';
          if (this.user !== undefined) {
            text += escape(this.user.username, Encoding.UserPassword);
            if (this.user.hasPassword) text += ':' + escape(this.user.password, Encoding.UserPassword);
            text += '@';
          }
          if (this.host !== '') text += escape(this.host, Encoding.Host);
        }
      }
      const path = this.escapedPath();
      if (path !== '' && path[0] !== '/' && this.host !== '') text += '/';
      if (text === '' && (path.split('/', 1)[0] ?? '').includes(':')) text += './';
      text += path;
    }
    if (this.forceQuery || this.rawQuery !== '') text += '?' + this.rawQuery;
    if (this.fragment !== '') text += '#' + this.escapedFragment();
    return text;
  }

  /** Parse ref and resolve it against this URL, as Go URL.Parse. */
  parseReference(reference: string): GoURL { return this.resolveReference(parseGoURL(reference)); }

  /** Resolve a reference, as Go URL.ResolveReference. */
  resolveReference(reference: GoURL): GoURL {
    const url = reference.clone();
    if (reference.scheme === '') url.scheme = this.scheme;
    if (reference.scheme !== '' || reference.host !== '' || reference.user !== undefined) {
      setPath(url, resolvePath(reference.escapedPath(), ''));
      return url;
    }
    if (reference.opaque !== '') {
      url.user = undefined; url.host = ''; url.path = '';
      return url;
    }
    if (reference.path === '' && !reference.forceQuery && reference.rawQuery === '') {
      url.rawQuery = this.rawQuery;
      if (reference.fragment === '') { url.fragment = this.fragment; url.rawFragment = this.rawFragment; }
    }
    if (reference.path === '' && this.opaque !== '') {
      url.opaque = this.opaque; url.user = undefined; url.host = ''; url.path = '';
      return url;
    }
    url.host = this.host;
    url.user = this.user ? { ...this.user } : undefined;
    setPath(url, resolvePath(this.escapedPath(), reference.escapedPath()));
    return url;
  }
}

/** Parse a URL with Go url.Parse rules; throws a TypeError that never repeats the value. */
export function parseGoURL(raw: string): GoURL {
  const fragmentIndex = raw.indexOf('#');
  const withoutFragment = fragmentIndex < 0 ? raw : raw.slice(0, fragmentIndex);
  const url = parseWithoutFragment(withoutFragment);
  if (fragmentIndex >= 0) {
    const fragment = raw.slice(fragmentIndex + 1);
    if (fragment !== '') {
      url.fragment = unescape(fragment, Encoding.Fragment);
      url.rawFragment = escape(url.fragment, Encoding.Fragment) === fragment ? '' : fragment;
    }
  }
  return url;
}

function parseWithoutFragment(raw: string): GoURL {
  for (let index = 0; index < raw.length; index++) {
    const code = raw.charCodeAt(index);
    if (code < 0x20 || code === 0x7f) throw new TypeError('invalid control character in URL');
  }
  const url = new GoURL();
  if (raw === '*') { url.path = '*'; return url; }
  let rest: string;
  [url.scheme, rest] = getScheme(raw);
  url.scheme = url.scheme.toLowerCase();
  if (rest.endsWith('?') && rest.split('?').length === 2) {
    url.forceQuery = true;
    rest = rest.slice(0, -1);
  } else {
    const queryIndex = rest.indexOf('?');
    if (queryIndex >= 0) { url.rawQuery = rest.slice(queryIndex + 1); rest = rest.slice(0, queryIndex); }
  }
  if (!rest.startsWith('/')) {
    if (url.scheme !== '') { url.opaque = rest; return url; }
    if ((rest.split('/', 1)[0] ?? '').includes(':')) throw new TypeError('first path segment in URL cannot contain colon');
  }
  if ((url.scheme !== '' || !rest.startsWith('///')) && rest.startsWith('//')) {
    let authority = rest.slice(2);
    rest = '';
    const slashIndex = authority.indexOf('/');
    if (slashIndex >= 0) { rest = authority.slice(slashIndex); authority = authority.slice(0, slashIndex); }
    [url.user, url.host] = parseAuthority(authority);
  } else if (url.scheme !== '' && rest.startsWith('/')) {
    url.omitHost = true;
  }
  setPath(url, rest);
  return url;
}

function getScheme(raw: string): [string, string] {
  for (let index = 0; index < raw.length; index++) {
    const character = raw[index]!;
    if (/[a-zA-Z]/.test(character)) continue;
    if (/[0-9+.-]/.test(character)) {
      if (index === 0) return ['', raw];
      continue;
    }
    if (character === ':') {
      if (index === 0) throw new TypeError('missing protocol scheme');
      return [raw.slice(0, index), raw.slice(index + 1)];
    }
    return ['', raw];
  }
  return ['', raw];
}

function parseAuthority(authority: string): [UserInformation | undefined, string] {
  const at = authority.lastIndexOf('@');
  const host = parseHost(at < 0 ? authority : authority.slice(at + 1));
  if (at < 0) return [undefined, host];
  const userInformation = authority.slice(0, at);
  if (!/^[A-Za-z0-9\-._:~!$&'()*+,;=%@]*$/.test(userInformation)) throw new TypeError('invalid userinfo');
  const colon = userInformation.indexOf(':');
  if (colon < 0) return [{ username: unescape(userInformation, Encoding.UserPassword), password: '', hasPassword: false }, host];
  return [{
    username: unescape(userInformation.slice(0, colon), Encoding.UserPassword),
    password: unescape(userInformation.slice(colon + 1), Encoding.UserPassword), hasPassword: true,
  }, host];
}

function parseHost(host: string): string {
  const open = host.lastIndexOf('[');
  if (open !== -1) {
    const close = host.lastIndexOf(']');
    if (close < 0) throw new TypeError("missing ']' in host");
    const colonPort = host.slice(close + 1);
    if (!validOptionalPort(colonPort)) throw new TypeError('invalid port after host');
    const unescapedPort = unescape(colonPort, Encoding.Host);
    const hostname = host.slice(open + 1, close);
    const zone = hostname.indexOf('%25');
    const unescapedHostname = zone >= 0
      ? unescape(hostname.slice(0, zone), Encoding.Host) + unescape(hostname.slice(zone), Encoding.Zone)
      : unescape(hostname, Encoding.Host);
    if (!isIPv6Literal(unescapedHostname)) throw new TypeError('invalid IP-literal');
    return '[' + unescapedHostname + ']' + unescapedPort;
  }
  const colon = host.lastIndexOf(':');
  if (colon !== -1 && !validOptionalPort(host.slice(colon))) throw new TypeError('invalid port after host');
  return unescape(host, Encoding.Host);
}

function isIPv6Literal(value: string): boolean {
  const address = value.split('%', 1)[0] ?? '';
  return address.includes(':') && /^[0-9A-Fa-f:.]+$/.test(address);
}

function setPath(url: GoURL, escapedPath: string): void {
  url.path = unescape(escapedPath, Encoding.Path);
  url.rawPath = escape(url.path, Encoding.Path) === escapedPath ? '' : escapedPath;
}

function validOptionalPort(port: string): boolean {
  if (port === '') return true;
  return port[0] === ':' && /^[0-9]*$/.test(port.slice(1));
}

function splitHostPort(hostPort: string): [string, string] {
  let host = hostPort;
  let port = '';
  const colon = host.lastIndexOf(':');
  if (colon !== -1 && validOptionalPort(host.slice(colon))) { port = host.slice(colon + 1); host = host.slice(0, colon); }
  if (host.startsWith('[') && host.endsWith(']')) host = host.slice(1, -1);
  return [host, port];
}

/** Resolve a path against a base path, as Go resolvePath. */
function resolvePath(base: string, reference: string): string {
  let full: string;
  if (reference === '') full = base;
  else if (reference[0] !== '/') full = base.slice(0, base.lastIndexOf('/') + 1) + reference;
  else full = reference;
  if (full === '') return '';
  let destination = '/';
  let isFirst = true;
  let element = '';
  const elements = full.split('/');
  for (element of elements) {
    if (element === '.') { isFirst = false; continue; }
    if (element === '..') {
      const current = destination.slice(1);
      const index = current.lastIndexOf('/');
      destination = '/';
      if (index === -1) isFirst = true;
      else destination += current.slice(0, index);
    } else {
      if (!isFirst) destination += '/';
      destination += element;
      isFirst = false;
    }
  }
  if (element === '.' || element === '..') destination += '/';
  return destination.length > 1 && destination[1] === '/' ? destination.slice(1) : destination;
}

function shouldEscape(byte: number, mode: Encoding): boolean {
  if ((byte >= 0x61 && byte <= 0x7a) || (byte >= 0x41 && byte <= 0x5a) || (byte >= 0x30 && byte <= 0x39)) return false;
  const character = String.fromCharCode(byte);
  if ((mode === Encoding.Host || mode === Encoding.Zone) && "!$&'()*+,;=:[]<>\"".includes(character)) return false;
  if ('-_.~'.includes(character)) return false;
  if ('$&+,/:;=?@'.includes(character)) {
    switch (mode) {
      case Encoding.Path: return character === '?';
      case Encoding.PathSegment: return character === '/' || character === ';' || character === ',' || character === '?';
      case Encoding.UserPassword: return character === '@' || character === '/' || character === '?' || character === ':';
      case Encoding.QueryComponent: return true;
      case Encoding.Fragment: return false;
      default: break;
    }
  }
  if (mode === Encoding.Fragment && '!()*'.includes(character)) return false;
  return true;
}

function escape(value: string, mode: Encoding): string {
  const bytes = Buffer.from(value, 'utf8');
  let text = '';
  for (const byte of bytes) {
    if (byte === 0x20 && mode === Encoding.QueryComponent) text += '+';
    else if (shouldEscape(byte, mode)) text += '%' + byte.toString(16).toUpperCase().padStart(2, '0');
    else text += String.fromCharCode(byte);
  }
  return bytes.every(byte => byte < 0x80) || text !== value ? text : value;
}

function isHex(byte: number | undefined): boolean {
  return byte !== undefined && ((byte >= 0x30 && byte <= 0x39) || (byte >= 0x61 && byte <= 0x66) || (byte >= 0x41 && byte <= 0x46));
}

function unescape(value: string, mode: Encoding): string {
  const bytes = Buffer.from(value, 'utf8');
  const output: number[] = [];
  for (let index = 0; index < bytes.length; index++) {
    const byte = bytes[index]!;
    if (byte === 0x25) {
      if (!isHex(bytes[index + 1]) || !isHex(bytes[index + 2])) throw new TypeError('invalid URL escape');
      const decoded = Number.parseInt(String.fromCharCode(bytes[index + 1]!, bytes[index + 2]!), 16);
      const sequence = String.fromCharCode(byte, bytes[index + 1]!, bytes[index + 2]!);
      if (mode === Encoding.Host && decoded >> 4 < 8 && sequence !== '%25' && Number.parseInt(String.fromCharCode(bytes[index + 1]!), 16) < 8) {
        throw new TypeError('invalid URL escape in host');
      }
      if (mode === Encoding.Zone && sequence !== '%25' && decoded !== 0x20 && shouldEscape(decoded, Encoding.Host)) {
        throw new TypeError('invalid URL escape in zone');
      }
      output.push(decoded);
      index += 2;
    } else if (byte === 0x2b) {
      output.push(mode === Encoding.QueryComponent ? 0x20 : 0x2b);
    } else {
      if ((mode === Encoding.Host || mode === Encoding.Zone) && byte < 0x80 && shouldEscape(byte, mode)) {
        throw new TypeError('invalid character in host name');
      }
      output.push(byte);
    }
  }
  return Buffer.from(output).toString('utf8');
}

function tryUnescape(value: string, mode: Encoding): string | undefined {
  try { return unescape(value, mode); } catch { return undefined; }
}

function validEncoded(value: string, mode: Encoding): boolean {
  for (const byte of Buffer.from(value, 'utf8')) {
    if ("!$&'()*+,;=:@[]%".includes(String.fromCharCode(byte))) continue;
    if (shouldEscape(byte, mode)) return false;
  }
  return true;
}
