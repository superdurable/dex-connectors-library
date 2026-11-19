// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { TLSSocket, createSecureContext } from 'node:tls';
import { expect, test } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { EmailClient } from '../src/index.js';

const directory = mkdtempSync(join(tmpdir(), 'dex-email-imap-test-'));
const certificatePath = join(directory, 'server.crt'), keyPath = join(directory, 'server.key');
execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost,IP:127.0.0.1', '-keyout', keyPath, '-out', certificatePath], { stdio: 'ignore' });
const certificate = readFileSync(certificatePath), key = readFileSync(keyPath);

function fakeCall(): Call { return { id: 'imap-test-call', connection: { provider: 'email', name: 'email' }, operation: { connectorId: 'email', operationId: 'searchMessages' }, context: { attempt: 1 } } as unknown as Call; }

test('IMAP STARTTLS upgrades before LOGIN and searches a selected mailbox', async () => {
  let authenticationOnTLS = false;
  const listener = createServer(raw => {
    raw.write('* OK [CAPABILITY IMAP4rev1 STARTTLS] ready\r\n');
    let buffer = '';
    const processLine = (line: string, socket: typeof raw | TLSSocket, secure: boolean) => {
      const [tag, command, ...rest] = line.split(' ');
      if (!tag || !command) return;
      const upper = `${command} ${rest.join(' ')}`.toUpperCase();
      if (upper.startsWith('CAPABILITY')) socket.write('* CAPABILITY IMAP4rev1 STARTTLS AUTH=PLAIN\r\n' + `${tag} OK CAPABILITY completed\r\n`);
      else if (upper.startsWith('STARTTLS')) {
        socket.write(`${tag} OK Begin TLS\r\n`, () => {
          raw.off('data', onPlaintext);
          const tlsSocket = new TLSSocket(raw, { isServer: true, secureContext: createSecureContext({ cert: certificate, key }) });
          let secureBuffer = '';
          tlsSocket.on('data', data => {
            secureBuffer += data.toString();
            for (;;) { const end = secureBuffer.indexOf('\r\n'); if (end < 0) break; const next = secureBuffer.slice(0, end); secureBuffer = secureBuffer.slice(end + 2); processLine(next, tlsSocket, true); }
          });
        });
      } else if (upper.startsWith('LOGIN') || upper.startsWith('AUTHENTICATE')) { authenticationOnTLS = secure; socket.write(`${tag} OK authentication completed\r\n`); }
      else if (upper.startsWith('EXAMINE') || upper.startsWith('SELECT')) socket.write('* FLAGS (\\Seen)\r\n* 0 EXISTS\r\n* OK [UIDVALIDITY 42] valid\r\n' + `${tag} OK [READ-ONLY] selected\r\n`);
      else if (upper.startsWith('UID SEARCH') || upper.startsWith('SEARCH')) socket.write(`* SEARCH\r\n${tag} OK SEARCH completed\r\n`);
      else if (upper.startsWith('LOGOUT')) socket.write(`* BYE closing\r\n${tag} OK LOGOUT completed\r\n`);
      else socket.write(`${tag} OK completed\r\n`);
    };
    const onPlaintext = (data: Buffer) => {
      buffer += data.toString();
      for (;;) { const end = buffer.indexOf('\r\n'); if (end < 0) break; const line = buffer.slice(0, end); buffer = buffer.slice(end + 2); processLine(line, raw, false); }
    };
    raw.on('data', onPlaintext);
  });
  await new Promise<void>(resolve => listener.listen(0, '127.0.0.1', resolve));
  const address = listener.address();
  if (!address || typeof address === 'string') throw new Error('IMAP test listener has no port');
  try {
    const email = new EmailClient({ imapHost: 'localhost', imapPort: address.port, imapSecurity: 'startTLS', smtpHost: 'localhost' }, { resolve: async () => ({ username: 'support@example.com', password: new SecretString('test-password') }) }, { ca: certificate });
    const result = await email.searchMessages().invoke(fakeCall(), { mailbox: 'INBOX' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('searched'); expect(result.value.messages).toEqual([]); }
    expect(authenticationOnTLS).toBe(true);
  } finally { await new Promise<void>(resolve => listener.close(() => resolve())); rmSync(directory, { recursive: true, force: true }); }
});

test('IMAP server without STARTTLS is rejected before authentication', async () => {
  let sawAuthentication = false;
  const listener = createServer(socket => {
    socket.write('* OK [CAPABILITY IMAP4rev1] ready\r\n');
    socket.on('data', data => {
      const line = data.toString();
      if (/LOGIN|AUTHENTICATE/i.test(line)) sawAuthentication = true;
      const tag = line.split(' ')[0];
      if (/CAPABILITY/i.test(line)) socket.write(`* CAPABILITY IMAP4rev1\r\n${tag} OK CAPABILITY completed\r\n`);
    });
  });
  await new Promise<void>(resolve => listener.listen(0, '127.0.0.1', resolve));
  const address = listener.address();
  if (!address || typeof address === 'string') throw new Error('IMAP refusal listener has no port');
  try {
    const email = new EmailClient({ imapHost: 'localhost', imapPort: address.port, imapSecurity: 'startTLS', smtpHost: 'localhost' }, { resolve: async () => ({ username: 'support@example.com', password: new SecretString('test-password') }) }, { ca: certificate });
    const result = await email.searchMessages().invoke(fakeCall(), {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('providerRejected'); expect(result.failure?.kind).toBe('PROTOCOL'); }
    expect(sawAuthentication).toBe(false);
  } finally { await new Promise<void>(resolve => listener.close(() => resolve())); }
});
