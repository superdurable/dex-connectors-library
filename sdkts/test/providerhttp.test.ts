// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createServer } from 'node:http';
import { afterEach, describe, expect, it } from 'vitest';
import {
  AddressRefusal, BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter,
  readBoundedBody, readErrorTokens, sendProviderRequest, validateBaseURL,
} from '../src/providerhttp/index.js';
import { isAllowedProviderIPAddress, isLocalStorageHost, isPrivateBrokerHost } from '../src/netpolicy.js';

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => { await Promise.all(servers.splice(0).map(server => new Promise<void>(resolve => server.close(() => resolve())))); });

describe('provider HTTP', () => {
  it('parses Retry-After and keeps only safe error tokens', () => {
    expect(parseRetryAfter('7')).toBe(7000);
    expect(parseRetryAfter('999999999')).toBe(3600000);
    expect(parseRetryAfter('Wed, 21 Oct 2015 07:28:00 GMT', new Date('2015-10-21T07:27:00Z'))).toBe(60000);
    expect(parseRetryAfter('bad')).toBe(0);
    expect(parseRetryAfter('Wed, 21 Oct 2015 07:28:00 PST')).toBe(0);
    expect(parseRetryAfter('2015-10-21T07:28:00Z')).toBe(0);
    expect(readErrorTokens(Buffer.from('{"error":{"code":"RATE_LIMIT","message":"do not show"}}'), ['/error/code', '/error/message'])).toEqual(['RATE_LIMIT']);
  });

  it('bounds bodies and validates credential URLs', async () => {
    async function* chunks(): AsyncGenerator<Uint8Array> { yield Buffer.from('ab'); yield Buffer.from('cd'); }
    await expect(readBoundedBody(chunks(), 3)).rejects.toBeInstanceOf(BodyTooLargeError);
    expect(validateBaseURL('https://api.example.test/')).toBe('https://api.example.test');
    expect(validateBaseURL('http://[::1]/')).toBe('http://[::1]');
    expect(() => validateBaseURL('http://127.evil.com/')).toThrow('loopback');
    expect(() => validateBaseURL('http://10.attacker.com/')).toThrow('loopback');
    expect(() => validateBaseURL('https://token@api.example.test')).toThrow('user information');
    expect(isHeaderSafeCredential('abc|123')).toBe(true);
    expect(isHeaderSafeCredential('abc\n123')).toBe(false);
  });

  it('uses IP CIDR ranges and exact local hostnames', () => {
    for (const host of ['127.evil.com', '10.attacker.com', 'localhost.evil.com']) {
      expect(isPrivateBrokerHost(host)).toBe(false);
      expect(isLocalStorageHost(host)).toBe(false);
    }
    for (const address of ['127.0.0.1', '10.2.3.4', '172.16.0.1', '172.31.255.255', '192.168.1.2', '[fc00::1]']) {
      expect(isAllowedProviderIPAddress(address, 'http:', 'privateNetworkHTTP')).toBe(true);
      expect(isAllowedProviderIPAddress(address, 'https:', 'publicHTTPS')).toBe(false);
    }
    for (const address of ['169.254.169.254', 'fe80::1', 'febf::1', 'fd00:ec2::254', '0.0.0.0', '224.0.0.1', 'ff02::1']) {
      expect(isAllowedProviderIPAddress(address, 'https:', 'privateNetworkHTTP')).toBe(false);
    }
    expect(isAllowedProviderIPAddress('100.64.0.1', 'https:', 'publicHTTPS')).toBe(false);
    expect(isAllowedProviderIPAddress('8.8.8.8', 'http:', 'privateNetworkHTTP')).toBe(false);
    expect(isAllowedProviderIPAddress('8.8.8.8', 'https:', 'publicHTTPS')).toBe(true);
  });

  it('bounds the real response body and applies one total deadline', async () => {
    const server = createServer((request, response) => {
      if (request.url === '/large') { response.end('body exceeds limit'); return; }
      response.write('a');
      const interval = setInterval(() => response.write('a'), 10);
      response.on('close', () => clearInterval(interval));
    });
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    const url = `http://127.0.0.1:${address.port}`;
    await expect(sendProviderRequest({ url: `${url}/large`, method: 'GET', maxResponseBytes: 4, addressPolicy: 'privateNetworkHTTP' })).rejects.toBeInstanceOf(BodyTooLargeError);
    await expect(sendProviderRequest({ url: `${url}/drip`, method: 'GET', maxResponseBytes: 10_000, timeoutMs: 40, addressPolicy: 'privateNetworkHTTP' })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: false });
  });

  it('returns redirects without replaying a credential', async () => {
    let requests = 0;
    const server = createServer((_request, response) => { requests++; response.writeHead(302, { Location: 'https://other.example.test/' }); response.end(); });
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    const result = await sendProviderRequest({ url: `http://127.0.0.1:${address.port}/`, method: 'GET', maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP', headers: { Authorization: 'Bearer secret' } });
    expect(result.status).toBe(302);
    expect(requests).toBe(1);
    await expect(sendProviderRequest({ url: `http://127.0.0.1:${address.port}/`, method: 'GET', maxResponseBytes: 16, addressPolicy: 'publicHTTPS' })).rejects.toBeInstanceOf(AddressRefusal);
  });

  it('fails closed when a socket may have received request bytes', async () => {
    const server = createServer((_request, response) => response.destroy());
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    await expect(sendProviderRequest({ url: `http://127.0.0.1:${address.port}/`, method: 'POST', body: Buffer.from('write'), maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP' })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: false });
  });
});
