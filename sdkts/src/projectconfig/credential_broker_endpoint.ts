// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isPrivateBrokerHost, unbracketHostname } from '../netpolicy.js';

// Internal module, not exported from index.ts.

const maximumCredentialBrokerEndpointBytes = 2048;
const absoluteURLPattern = /^([A-Za-z][A-Za-z0-9+.-]*):\/\/([^/?#]*)([^?#]*)(?:\?([^#]*))?(?:#([\s\S]*))?$/;

// Mirrors Go validateCredentialBrokerEndpoint on the raw string, as net/url sees it, rather than the WHATWG URL.
export function isValidCredentialBrokerEndpoint(value: string): boolean {
  if (typeof value !== 'string' || Buffer.byteLength(value, 'utf8') > maximumCredentialBrokerEndpointBytes || hasASCIIControlCharacter(value) || /%(?![0-9A-Fa-f]{2})/.test(value)) return false;
  const parts = absoluteURLPattern.exec(value);
  if (!parts) return false;
  const [, scheme = '', authority = '', , query = '', fragment = ''] = parts;
  const protocol = scheme.toLowerCase();
  if ((protocol !== 'http' && protocol !== 'https') || authority.includes('@') || query || fragment) return false;
  const hostname = goHostname(authority);
  if (hostname === undefined || !hostname || hostname.includes('%')) return false;
  let requestHostname: string;
  try { requestHostname = unbracketHostname(new URL(value).hostname); } catch { return false; }
  // The request goes to the WHATWG host: refuse IDN, percent-encoded, and non-canonical IPv4 hosts that it rewrites.
  if (requestHostname !== hostname.toLowerCase()) return false;
  return protocol === 'https' || (isPrivateBrokerHost(hostname) && isPrivateBrokerHost(requestHostname));
}

function hasASCIIControlCharacter(value: string): boolean {
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

// Go url.URL.Hostname: brackets removed and a valid optional port stripped; undefined when the port is invalid.
function goHostname(authority: string): string | undefined {
  if (authority.startsWith('[')) {
    const end = authority.indexOf(']');
    if (end < 0 || !/^(?::\d*)?$/.test(authority.slice(end + 1))) return undefined;
    return authority.slice(1, end);
  }
  const colon = authority.lastIndexOf(':');
  if (colon < 0) return authority;
  return /^\d*$/.test(authority.slice(colon + 1)) ? authority.slice(0, colon) : undefined;
}
