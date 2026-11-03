// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { lookup as lookupAddresses } from 'node:dns/promises';
import { readFileSync } from 'node:fs';
import { createServer } from 'node:http';
import https, { createServer as createTLSServer } from 'node:https';
import { createServer as createTCPServer, type Socket } from 'node:net';
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
      if (request.url === '/large') { response.writeHead(429, { 'Retry-After': '7' }); response.end('body exceeds limit'); return; }
      response.write('a');
      const interval = setInterval(() => response.write('a'), 10);
      response.on('close', () => clearInterval(interval));
    });
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    const url = `http://127.0.0.1:${address.port}`;
    await expect(sendProviderRequest({ url: `${url}/large`, method: 'GET', maxResponseBytes: 4, addressPolicy: 'privateNetworkHTTP' })).rejects.toMatchObject({ name: BodyTooLargeError.name, status: 429, headers: { 'retry-after': '7' } });
    await expect(sendProviderRequest({ url: `${url}/drip`, method: 'GET', maxResponseBytes: 10_000, timeoutMs: 40, addressPolicy: 'privateNetworkHTTP' })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: false });
  });

  it('drains an oversized response to EOF without retaining or observing later chunks', async () => {
    let hasFinishedResponse = false;
    const server = createServer((_request, response) => {
      response.writeHead(200, { 'X-Request-ID': 'drained' });
      response.write('abc');
      setTimeout(() => response.write('def'), 20);
      setTimeout(() => response.end('gh'), 40);
      response.on('finish', () => { hasFinishedResponse = true; });
    });
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    const observedChunks: string[] = [];
    await expect(sendProviderRequest({
      url: `http://127.0.0.1:${address.port}/`, method: 'GET', maxResponseBytes: 4,
      onBodyLimit: 'drain', addressPolicy: 'privateNetworkHTTP',
      onResponseChunk: chunk => { observedChunks.push(Buffer.from(chunk).toString()); },
    })).rejects.toMatchObject({ name: BodyTooLargeError.name, status: 200, headers: { 'x-request-id': 'drained' } });
    expect(hasFinishedResponse).toBe(true);
    expect(observedChunks).toEqual(['abc']);
  });

  it('destroys an oversized response by default', async () => {
    let hasFinishedResponse = false;
    const server = createServer((_request, response) => {
      response.write('oversized');
      response.on('finish', () => { hasFinishedResponse = true; });
      response.on('close', () => { response.destroy(); });
    });
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    await expect(sendProviderRequest({ url: `http://127.0.0.1:${address.port}/`, method: 'GET', maxResponseBytes: 4, addressPolicy: 'privateNetworkHTTP' })).rejects.toBeInstanceOf(BodyTooLargeError);
    expect(hasFinishedResponse).toBe(false);
  });

  it('honors cancellation while draining an oversized response', async () => {
    const server = createServer((_request, response) => {
      response.write('oversized');
      response.on('close', () => { response.destroy(); });
    });
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    const controller = new AbortController();
    setTimeout(() => controller.abort(), 20);
    await expect(sendProviderRequest({
      url: `http://127.0.0.1:${address.port}/`, method: 'GET', maxResponseBytes: 4,
      onBodyLimit: 'drain', addressPolicy: 'privateNetworkHTTP', signal: controller.signal,
    })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: false });
    await expect(sendProviderRequest({
      url: `http://127.0.0.1:${address.port}/`, method: 'GET', maxResponseBytes: 4,
      onBodyLimit: 'drain', addressPolicy: 'privateNetworkHTTP', timeoutMs: 20,
    })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: false });
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

  it('sends to a localhost hostname and proves a later refused connection was not sent', async () => {
    const addresses = await lookupAddresses('localhost', { all: true, verbatim: true });
    const selected = addresses[0];
    if (!selected) throw new Error('localhost has no resolved address');
    const server = createServer((_request, response) => response.end('ok'));
    await new Promise<void>(resolve => server.listen(0, selected.address, resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    const request = { url: `http://localhost:${address.port}/`, method: 'GET', maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP' as const };
    try {
      const response = await sendProviderRequest(request);
      expect(response.status).toBe(200);
      expect(Buffer.from(response.body).toString()).toBe('ok');
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    }
    await expect(sendProviderRequest(request)).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: true });
  });

  it('fails closed when a socket may have received request bytes', async () => {
    const server = createServer((_request, response) => response.destroy());
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    await expect(sendProviderRequest({ url: `http://127.0.0.1:${address.port}/`, method: 'POST', body: Buffer.from('write'), maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP' })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: false });
  });

  it('proves refused connections and DNS failures did not send a request', async () => {
    const server = createServer();
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    const request = { method: 'POST', body: Buffer.from('write'), maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP' as const };
    await expect(sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/` })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: true });
    await expect(sendProviderRequest({ ...request, url: 'https://nonexistent.invalid/' })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: true });
  });

  it('proves a TLS handshake timeout did not send HTTP bytes', async () => {
    const sockets: Socket[] = [];
    const server = createTCPServer(socket => { sockets.push(socket); });
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    try {
      await expect(sendProviderRequest({ url: `https://127.0.0.1:${address.port}/`, method: 'POST', body: Buffer.from('write'), maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP', timeoutMs: 300 })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: true });
    } finally {
      for (const socket of sockets) socket.destroy();
      await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    }
  });

  it('treats an HTTPS reset after the request arrives as possibly sent', async () => {
    const certificate = readFileSync(new URL('./fixtures/localhost-cert.pem', import.meta.url));
    const key = readFileSync(new URL('./fixtures/localhost-key.pem', import.meta.url));
    let hasReceivedRequest = false;
    const server = createTLSServer({ cert: certificate, key }, request => {
      hasReceivedRequest = true;
      request.socket.destroy();
    });
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    const previousCertificateAuthority = https.globalAgent.options.ca;
    https.globalAgent.options.ca = certificate;
    try {
      await expect(sendProviderRequest({ url: `https://127.0.0.1:${address.port}/`, method: 'POST', body: Buffer.from('write'), maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP' })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: false });
      expect(hasReceivedRequest).toBe(true);
    } finally {
      https.globalAgent.options.ca = previousCertificateAuthority;
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    }
  });

  it('proves a TLS handshake failure did not send HTTP request bytes', async () => {
    const server = createServer((_request, response) => response.end());
    servers.push(server);
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('server address unavailable');
    await expect(sendProviderRequest({ url: `https://127.0.0.1:${address.port}/`, method: 'POST', body: Buffer.from('write'), maxResponseBytes: 16, addressPolicy: 'privateNetworkHTTP' })).rejects.toMatchObject({ name: ProviderTransportError.name, isProvablyNotSent: true });
  });
});
