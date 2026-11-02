// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { lookup as lookupAddresses } from 'node:dns/promises';
import { createServer } from 'node:http';
import { parseRetryAfter, sendProviderRequest } from '../dist/providerhttp/index.js';

const localAddress = (await lookupAddresses('localhost', { all: true, verbatim: true }))[0]?.address;
if (!localAddress) throw new Error('localhost has no resolved address');
const server = createServer((_request, response) => {
  response.writeHead(429, { 'Retry-After': '7', 'Content-Type': 'application/json' });
  response.end('{"error":{"code":"RATE_LIMIT"}}');
});
await new Promise(resolve => server.listen(0, localAddress, resolve));
try {
  const address = server.address();
  const response = await sendProviderRequest({
    url: `http://localhost:${address.port}/`, method: 'GET', maxResponseBytes: 1024,
    addressPolicy: 'privateNetworkHTTP',
  });
  if (response.status !== 429 || parseRetryAfter(response.headers['retry-after']) !== 7000) throw new Error('HTTP example failed');
} finally {
  await new Promise(resolve => server.close(resolve));
}
const refusedServer = createServer();
await new Promise(resolve => refusedServer.listen(0, localAddress, resolve));
const refusedAddress = refusedServer.address();
await new Promise(resolve => refusedServer.close(resolve));
try {
  await sendProviderRequest({
    url: `http://localhost:${refusedAddress.port}/`, method: 'POST', body: Buffer.from('write'),
    maxResponseBytes: 1024, addressPolicy: 'privateNetworkHTTP',
  });
  throw new Error('expected refused connection');
} catch (error) {
  if (error?.name !== 'ProviderTransportError' || !error.isProvablyNotSent) throw error;
}
