// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import net, { type Server, type Socket } from 'node:net';
import { once } from 'node:events';
import { it, expect } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { MySQLClient } from '../src/client.js';

type ServerMode = 'close-on-execute' | 'rows-then-close' | 'blackhole-setup' | 'blackhole-execute' | 'blackhole-commit';

const capabilities = 0x1 | 0x8 | 0x200 | 0x2000 | 0x8000 | 0x80000 | 0x4 | 0x100000;
const salt = Buffer.from('abcdefghijklmnopqrst');
const handshake = Buffer.concat([
  Buffer.from([10]), Buffer.from('8.4.0\0'), Buffer.from([1, 0, 0, 0]), salt.subarray(0, 8), Buffer.from([0]),
  Buffer.from([capabilities & 255, (capabilities >> 8) & 255]), Buffer.from([255, 2, 0]),
  Buffer.from([(capabilities >> 16) & 255, (capabilities >> 24) & 255]), Buffer.from([21]),
  Buffer.alloc(10), salt.subarray(8), Buffer.from([0]), Buffer.from('mysql_native_password\0'),
]);

function packet(sequence: number, body: Buffer): Buffer {
  const header = Buffer.alloc(4);
  header.writeUIntLE(body.length, 0, 3);
  header[3] = sequence;
  return Buffer.concat([header, body]);
}

function lengthString(value: string): Buffer {
  const bytes = Buffer.from(value);
  return Buffer.concat([Buffer.from([bytes.length]), bytes]);
}

function column(sequence: number, name: string): Buffer {
  const details = Buffer.alloc(13);
  details[0] = 0x0c;
  details.writeUInt16LE(63, 1);
  details.writeUInt32LE(20, 3);
  details[7] = 8;
  return packet(sequence, Buffer.concat([lengthString('def'), lengthString('d'), lengthString('t'), lengthString('t'), lengthString(name), lengthString(name), details]));
}

function textRows(socket: Socket, names: string[], values: string[]): void {
  let sequence = 1;
  socket.write(Buffer.concat([
    packet(sequence++, Buffer.from([names.length])),
    ...names.map(name => column(sequence++, name)),
    packet(sequence++, Buffer.from([0xfe, 0, 0, 2, 0])),
    packet(sequence++, Buffer.concat(values.map(lengthString))),
    packet(sequence, Buffer.from([0xfe, 0, 0, 2, 0])),
  ]));
}

function serveConnection(socket: Socket, mode: ServerMode): void {
  socket.on('error', () => undefined);
  socket.write(packet(0, handshake));
  let authenticated = false;
  let buffered = Buffer.alloc(0);
  socket.on('data', data => {
    buffered = Buffer.concat([buffered, data]);
    while (buffered.length >= 4) {
      const length = buffered.readUIntLE(0, 3);
      if (buffered.length < length + 4) return;
      const body = buffered.subarray(4, length + 4);
      buffered = buffered.subarray(length + 4);
      if (!authenticated) {
        authenticated = true;
        socket.write(packet(2, Buffer.from([0, 0, 0, 2, 0, 0, 0])));
        continue;
      }
      if (body[0] === 3) {
        const statement = body.subarray(1).toString();
        if (statement.startsWith('SELECT CONNECTION_ID')) textRows(socket, ['CONNECTION_ID()', 'VERSION()'], ['7', '8.4.0']);
        else if (statement.startsWith('SET SESSION') && mode === 'blackhole-setup') continue;
        else if (statement.startsWith('SELECT ROW_COUNT')) textRows(socket, ['a', 'b', 'c'], ['1', '0', '0']);
        else if (statement === 'COMMIT' && mode === 'blackhole-commit') continue;
        else socket.write(packet(1, Buffer.from([0, 0, 0, 2, 0, 0, 0])));
      } else if (body[0] === 0x16) {
        const prepared = Buffer.alloc(12);
        prepared.writeUInt32LE(1, 1);
        prepared.writeUInt16LE(1, 5);
        socket.write(Buffer.concat([packet(1, prepared), column(2, 'n'), packet(3, Buffer.from([0xfe, 0, 0, 2, 0]))]));
      } else if (body[0] === 0x17) {
        if (mode === 'close-on-execute') { socket.destroy(); return; }
        if (mode === 'blackhole-execute') continue;
        if (mode === 'rows-then-close') {
          const row = Buffer.alloc(10);
          row.writeBigInt64LE(1n, 2);
          socket.write(Buffer.concat([packet(1, Buffer.from([1])), column(2, 'n'), packet(3, Buffer.from([0xfe, 0, 0, 2, 0])), packet(4, row)]), () => socket.destroy());
          return;
        }
        const row = Buffer.alloc(10);
        row.writeBigInt64LE(1n, 2);
        socket.write(Buffer.concat([packet(1, Buffer.from([1])), column(2, 'n'), packet(3, Buffer.from([0xfe, 0, 0, 2, 0])), packet(4, row), packet(5, Buffer.from([0xfe, 0, 0, 2, 0]))]));
      }
    }
  });
}

async function startServer(mode: ServerMode): Promise<{ server: Server; sockets: Set<Socket>; port: number }> {
  const sockets = new Set<Socket>();
  const server = net.createServer(socket => {
    sockets.add(socket);
    socket.once('close', () => sockets.delete(socket));
    serveConnection(socket, mode);
  });
  for (let attempt = 0; attempt < 100; attempt++) {
    const port = 32700 + Math.floor(Math.random() * 100);
    server.listen(port, '127.0.0.1');
    try { await once(server, 'listening'); return { server, sockets, port }; }
    catch (error) { if ((error as NodeJS.ErrnoException).code !== 'EADDRINUSE') throw error; }
  }
  throw new Error('no test port was available in 32700-32799');
}

const call = {
  id: 'de4b2ac7-66a0-4547-b4c8-6d44a12d6029',
  idempotencyKey: 'de4b2ac7-66a0-4547-b4c8-6d44a12d6029',
  connection: { provider: 'mysql', name: 'mysql-refunds' },
  operation: { connectorId: 'mysql', operationId: 'query' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it.each([
  ['close-on-execute', 'query', 'retry'],
  ['close-on-execute', 'execute', 'retry'],
  ['rows-then-close', 'query', 'retry'],
  ['blackhole-setup', 'query', 'retry'],
  ['blackhole-execute', 'query', 'retry'],
  ['blackhole-commit', 'execute', 'uncertain'],
] as const)('classifies %s during %s as %s before the call deadline', async (mode, operation, expectedKind) => {
  const { server, sockets, port } = await startServer(mode);
  try {
    const client = new MySQLClient({ host: '127.0.0.1', port, database: 'd', user: 'u', sslMode: 'disabled', connectTimeoutMs: 50, statementTimeoutMs: 50 }, { resolve: async () => ({ password: new SecretString('p') }) });
    const started = Date.now();
    const result = operation === 'query'
      ? await client.query().invoke(call, { statement: 'SELECT n FROM t' })
      : await client.execute().invoke(call, { statement: 'UPDATE t SET n = 1' });
    expect(result).toMatchObject({ kind: expectedKind, failure: { kind: 'TRANSPORT' } });
    expect(Date.now() - started).toBeLessThan(7000);
  } finally {
    for (const socket of sockets) socket.destroy();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 8000);
