// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer as createNetServer } from 'node:net';
import { afterEach, describe, expect, it } from 'vitest';
import { WebSocketServer, type WebSocket } from 'ws';
import { dialSlackSocket } from '../src/index.js';

const servers: WebSocketServer[] = [];
afterEach(async () => { await Promise.all(servers.splice(0).map(server => new Promise(resolve => { for (const client of server.clients) client.terminate(); server.close(resolve); }))); });

async function startServer(onConnection: (socket: WebSocket) => void): Promise<string> {
  const server = new WebSocketServer({ host: '127.0.0.1', port: 0 });
  servers.push(server);
  server.on('connection', onConnection);
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('server address unavailable');
  return `ws://127.0.0.1:${address.port}/link/?ticket=SENTINEL-TICKET`;
}

describe('Socket Mode WebSocket', () => {
  it('keeps a quiet connection that Slack pings alive, reads envelopes, and writes acknowledgements', async () => {
    const acknowledgements: string[] = [];
    let pongs = 0;
    const url = await startServer(socket => {
      socket.on('pong', () => { pongs++; });
      socket.on('message', data => acknowledgements.push(String(data)));
      const pinger = setInterval(() => socket.ping(), 20);
      socket.on('close', () => clearInterval(pinger));
      setTimeout(() => socket.send(JSON.stringify({ envelope_id: 'e1', type: 'events_api' })), 300);
    });
    const connection = await dialSlackSocket(url, new AbortController().signal);
    expect(await connection.readMessage(100)).toEqual({ envelope_id: 'e1', type: 'events_api' });
    await connection.writeMessage({ envelope_id: 'e1' });
    await expect.poll(() => acknowledgements).toEqual(['{"envelope_id":"e1"}']);
    expect(pongs).toBeGreaterThan(3);
    connection.close();
  });

  it('pauses reading while an envelope waits and refuses a message over the size limit', async () => {
    const messageCount = 40;
    const padding = 'x'.repeat(64 * 1024);
    const url = await startServer(socket => { for (let index = 0; index < messageCount; index++) socket.send(JSON.stringify({ envelope_id: `e${index}`, padding })); });
    const connection = await dialSlackSocket(url, new AbortController().signal);
    await new Promise(resolve => setTimeout(resolve, 300));
    // Unread envelopes stay in Slack's socket instead of this process: only what was already received is held.
    expect((connection as unknown as { pendingMessageCount: number }).pendingMessageCount).toBeLessThan(messageCount / 2);
    for (let index = 0; index < messageCount; index++) expect(await connection.readMessage(2_000)).toMatchObject({ envelope_id: `e${index}` });
    connection.close();

    const oversizedURL = await startServer(socket => socket.send(JSON.stringify({ padding: 'y'.repeat(2048) })));
    const limited = await dialSlackSocket(oversizedURL, new AbortController().signal, 1024);
    await expect(limited.readMessage(2_000)).rejects.toThrow(/read Slack Socket Mode envelope/);
  });

  it('stops parsing after a same-chunk burst of small frames and delivers every envelope in order', async () => {
    const frameCount = 10_000;
    const url = await startServer(socket => {
      // Write every frame in one TCP write, so many envelopes reach the client in the same network chunks.
      const frames = Array.from({ length: frameCount }, (_value, index) => { const payload = Buffer.from(JSON.stringify({ envelope_id: `e${index}` })); return Buffer.concat([Buffer.from([0x81, payload.length]), payload]); });
      (socket as unknown as { _socket: { write(data: Buffer): void } })._socket.write(Buffer.concat(frames));
    });
    const connection = await dialSlackSocket(url, new AbortController().signal);
    await new Promise(resolve => setTimeout(resolve, 300));
    const pending = (connection as unknown as { pendingMessageCount: number }).pendingMessageCount;
    // Parsing stops after the chunk in progress instead of draining every frame Slack sent.
    expect(pending).toBeGreaterThan(0);
    expect(pending).toBeLessThan(frameCount);
    for (let index = 0; index < frameCount; index++) expect(await connection.readMessage(2_000)).toEqual({ envelope_id: `e${index}` });
    connection.close();
  });

  it('rejects a pre-aborted dial without connecting and refuses a message limit ws cannot hold', async () => {
    let connections = 0;
    const server = createNetServer(socket => { connections++; socket.on('error', () => undefined); });
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    try {
      const address = server.address();
      if (!address || typeof address === 'string') throw new Error('server address unavailable');
      const aborted = new AbortController();
      aborted.abort();
      const started = Date.now();
      await expect(dialSlackSocket(`ws://127.0.0.1:${address.port}/link/?ticket=SENTINEL-TICKET`, aborted.signal)).rejects.toThrow('connect Slack Socket Mode: aborted');
      expect(Date.now() - started).toBeLessThan(500);
      expect(connections).toBe(0);
      await expect(dialSlackSocket('ws://127.0.0.1:9/', new AbortController().signal, 2 ** 31)).rejects.toThrow(TypeError);
    } finally { await new Promise(resolve => server.close(resolve)); }
  });

  it('ends a silent connection at the read timeout and never names the URL in errors', async () => {
    const url = await startServer(() => undefined);
    const connection = await dialSlackSocket(url, new AbortController().signal);
    await expect(connection.readMessage(50)).rejects.toThrow('read timeout');
    const refused = await dialSlackSocket('ws://127.0.0.1:9/link/?ticket=SENTINEL-TICKET', new AbortController().signal).catch((error: unknown) => error);
    expect(String((refused as Error).message)).toMatch(/^connect Slack Socket Mode/);
    expect(String((refused as Error).message)).not.toContain('SENTINEL');
  });
});
