// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import net, { type Socket } from 'node:net';
import { afterEach, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { PostgreSQLClient } from '../src/client.js';

const message = (type: string, body: Buffer): Buffer => {
  const encoded = Buffer.alloc(5 + body.length);
  encoded.write(type, 0);
  encoded.writeInt32BE(4 + body.length, 1);
  body.copy(encoded, 5);
  return encoded;
};
const cstring = (value: string): Buffer => Buffer.from(`${value}\0`);
const ready = (): Buffer => message('Z', Buffer.from('T'));
const sockets = new Set<Socket>();
const servers: net.Server[] = [];

afterEach(async () => {
  for (const socket of sockets) socket.destroy();
  await Promise.all(servers.splice(0).map(server => new Promise<void>(resolve => server.close(() => resolve()))));
});

async function wireServer(mode: 'commit' | 'statement' | 'close'): Promise<number> {
  const server = net.createServer(socket => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
    let pending = Buffer.alloc(0);
    let hasStarted = false;
    socket.on('data', chunk => {
      pending = Buffer.concat([pending, chunk]);
      for (;;) {
        if (!hasStarted) {
          if (pending.length < 8) return;
          const length = pending.readInt32BE(0);
          if (pending.length < length) return;
          pending = pending.subarray(length);
          hasStarted = true;
          socket.write(Buffer.concat([message('R', Buffer.alloc(4)), ready()]));
          continue;
        }
        if (pending.length < 5) return;
        const type = String.fromCharCode(pending[0] ?? 0);
        const length = pending.readInt32BE(1);
        if (pending.length < length + 1) return;
        const body = pending.subarray(5, length + 1);
        pending = pending.subarray(length + 1);
        if (type === 'Q') {
          const statement = body.toString('utf8').replace(/\0$/, '');
          if (statement === 'COMMIT' && mode === 'commit') continue;
          socket.write(Buffer.concat([message('C', cstring(statement.split(' ')[0]?.toUpperCase() ?? '')), ready()]));
        } else if (type === 'P') socket.write(message('1', Buffer.alloc(0)));
        else if (type === 'B') socket.write(message('2', Buffer.alloc(0)));
        else if (type === 'D') socket.write(message('n', Buffer.alloc(0)));
        else if (type === 'E') {
          if (mode === 'statement') continue;
          if (mode === 'close') { socket.destroy(); return; }
          socket.write(message('C', cstring('INSERT 0 1')));
        } else if (type === 'C') socket.write(message('3', Buffer.alloc(0)));
        else if (type === 'S') socket.write(ready());
        else if (type === 'X') socket.end();
      }
    });
  });
  servers.push(server);
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('wire test server did not bind a TCP port');
  return address.port;
}

it.each([
  ['commit', 'uncertain'],
  ['statement', 'retry'],
  ['close', 'retry'],
] as const)('classifies a real pg.Client %s socket failure without an uncaught error', async (mode, kind) => {
  const port = await wireServer(mode);
  const client = new PostgreSQLClient({ host: '127.0.0.1', port, database: 'app', user: 'dex_app', sslMode: 'disable', connectTimeoutMs: 500, statementTimeoutMs: 1 }, { resolve: async () => ({ password: new SecretString('test-password') }) });
  const call = { id: '00000000-0000-4000-8000-000000000001', idempotencyKey: '00000000-0000-4000-8000-000000000001' } as Call;
  const result = await client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES (1)' });
  expect(result).toMatchObject({ kind, failure: { kind: 'TRANSPORT' } });
}, 15_000);
