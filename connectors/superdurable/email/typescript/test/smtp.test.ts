// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer, type Server } from 'node:tls';
import { afterAll, beforeAll, expect, test } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { EmailClient } from '../src/index.js';

const directory = mkdtempSync(join(tmpdir(), 'dex-email-smtp-test-'));
const certificatePath = join(directory, 'server.crt');
const keyPath = join(directory, 'server.key');
execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost,IP:127.0.0.1', '-keyout', keyPath, '-out', certificatePath], { stdio: 'ignore' });
const certificate = readFileSync(certificatePath);
const key = readFileSync(keyPath);
let server: Server;
let port: number;
let submissions = 0;
let loseFinalAnswer = false;
let recipientReply = 250;
let authReply = 235;
let lastContent = '';
let sizeCapability = '250 SIZE 1000000';
let lastMailFrom = '';
const recipients: string[] = [];
beforeAll(async () => {
  server = createServer({ cert: certificate, key }, socket => {
    socket.write('220 localhost ESMTP ready\r\n');
    let buffer = '';
    let inData = false;
    socket.on('data', chunk => {
      buffer += chunk.toString('utf8');
      for (;;) {
        const end = buffer.indexOf('\r\n');
        if (end < 0) break;
        const line = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        if (inData) {
          if (line === '.') { submissions++; inData = false; if (loseFinalAnswer) socket.destroy(); else socket.write('250 accepted\r\n'); }
          else lastContent += `${line}\r\n`;
        } else if (line.startsWith('EHLO')) socket.write(`250-localhost\r\n250-AUTH PLAIN\r\n${sizeCapability}\r\n`);
        else if (line.startsWith('AUTH PLAIN')) socket.write(`${authReply} authentication result\r\n`);
        else if (line.startsWith('MAIL FROM')) { lastMailFrom = line; socket.write('250 ok\r\n'); }
        else if (line.startsWith('RCPT TO')) { recipients.push(line); socket.write(`${recipientReply} recipient result\r\n`); }
        else if (line === 'DATA') { inData = true; socket.write('354 send content\r\n'); }
        else socket.write('500 unrecognized\r\n');
      }
    });
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('SMTP test listener has no port');
  port = address.port;
});
afterAll(async () => { await new Promise<void>(resolve => server.close(() => resolve())); rmSync(directory, { recursive: true, force: true }); });

function call(): { call: Call; heartbeats: unknown[] } {
  const heartbeats: unknown[] = [];
  return { call: { id: 'send-test-call', idempotencyKey: 'send-test-call', connection: { provider: 'email', name: 'email' }, operation: { connectorId: 'email', operationId: 'sendMessage' }, context: { attempt: 1, hasLastHeartbeatValue: () => false, recordHeartbeat: async (value: unknown) => { heartbeats.push(value); } } } as unknown as Call, heartbeats };
}
function client() { return new EmailClient({ imapHost: 'localhost', smtpHost: 'localhost', smtpPort: port, fromAddress: 'support@example.com', fromName: 'Support' }, { resolve: async () => ({ username: 'support@example.com', password: new SecretString('test-password') }) }, { ca: certificate, now: () => new Date('2026-01-28T10:05:00Z') }); }

test('SMTP accepts one message after a checkpoint and hides Bcc', async () => {
  loseFinalAnswer = false; recipientReply = 250; authReply = 235;
  const { call: invocation, heartbeats } = call();
  const result = await client().sendMessage().invoke(invocation, { to: ['jane@example.com'], bcc: ['audit@example.com'], subject: 'Überweisung', text: 'A line\n.\nDone' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('sent'); expect(result.value.messageId).toBe('dex-send-test-call@example.com'); }
  expect(heartbeats).toEqual([{ emailSubmittedCallId: 'send-test-call' }]);
  expect(submissions).toBe(1);
  expect(lastContent).toContain('Message-ID: <dex-send-test-call@example.com>');
  expect(lastContent).not.toContain('Bcc:');
  expect(lastContent).toContain('..');
  expect(lastContent).not.toContain('Überweisung');
  expect(lastContent).toContain('=?UTF-8?B?');
});

test('SIZE 0 permits submission and keeps the first recipient spelling', async () => {
  sizeCapability = '250 SIZE 0'; loseFinalAnswer = false; recipientReply = 250; authReply = 235;
  recipients.length = 0;
  const result = await client().sendMessage().invoke(call().call, { to: ['Jane@example.com'], cc: ['jane@example.com'], subject: 'No fixed limit', text: 'Hello' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('sent'); expect(result.value.recipients).toEqual(['Jane@example.com']); }
  expect(lastMailFrom).not.toContain(' SIZE=');
  expect(recipients).toEqual(['RCPT TO:<Jane@example.com>']);
  sizeCapability = '250 SIZE 1000000';
});

test('long ASCII subjects fold below the RFC line limit', async () => {
  loseFinalAnswer = false; recipientReply = 250; authReply = 235; lastContent = '';
  const result = await client().sendMessage().invoke(call().call, { to: ['jane@example.com'], subject: 'A'.repeat(990), text: 'Hello' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('sent');
  expect(lastContent.split('\r\n').every(line => Buffer.byteLength(line) <= 998)).toBe(true);
  expect(lastContent).toContain('Subject: =?UTF-8?B?');
});

test('lost answer after the final dot is uncertain and retains the checkpoint', async () => {
  const before = submissions;
  loseFinalAnswer = true; recipientReply = 250; authReply = 235;
  const { call: invocation, heartbeats } = call();
  const result = await client().sendMessage().invoke(invocation, { to: ['jane@example.com'], subject: 'Maybe sent', text: 'The answer was lost.' });
  expect(result.kind).toBe('uncertain');
  expect(heartbeats).toEqual([{ emailSubmittedCallId: 'send-test-call' }]);
  expect(submissions).toBe(before + 1);
});

test('STARTTLS upgrades before AUTH on a fake SMTP server', async () => {
  const { createServer: createTCPServer } = await import('node:net');
  const { TLSSocket, createSecureContext } = await import('node:tls');
  let authenticatedAfterTLS = false;
  const listener = createTCPServer(raw => {
    raw.write('220 localhost ESMTP ready\r\n');
    let plaintext = '';
    const onPlaintext = (chunk: Buffer) => {
      plaintext += chunk.toString();
      for (;;) {
        const end = plaintext.indexOf('\r\n');
        if (end < 0) break;
        const line = plaintext.slice(0, end); plaintext = plaintext.slice(end + 2);
        if (line.startsWith('EHLO')) raw.write('250-localhost\r\n250 STARTTLS\r\n');
        else if (line === 'STARTTLS') {
          raw.write('220 ready to start TLS\r\n', () => {
            raw.off('data', onPlaintext);
            const secure = new TLSSocket(raw, { isServer: true, secureContext: createSecureContext({ cert: certificate, key }) });
            let securedBuffer = '';
            let inData = false;
            secure.on('data', data => {
              securedBuffer += data.toString();
              for (;;) {
                const secureEnd = securedBuffer.indexOf('\r\n');
                if (secureEnd < 0) break;
                const secureLine = securedBuffer.slice(0, secureEnd); securedBuffer = securedBuffer.slice(secureEnd + 2);
                if (inData) { if (secureLine === '.') { inData = false; secure.write('250 accepted\r\n'); } }
                else if (secureLine.startsWith('EHLO')) secure.write('250-localhost\r\n250 AUTH PLAIN\r\n');
                else if (secureLine.startsWith('AUTH PLAIN')) { authenticatedAfterTLS = true; secure.write('235 authenticated\r\n'); }
                else if (secureLine.startsWith('MAIL FROM') || secureLine.startsWith('RCPT TO')) secure.write('250 ok\r\n');
                else if (secureLine === 'DATA') { inData = true; secure.write('354 go ahead\r\n'); }
                else if (secureLine === 'QUIT') secure.write('221 bye\r\n');
              }
            });
          });
        }
      }
    };
    raw.on('data', onPlaintext);
  });
  await new Promise<void>(resolve => listener.listen(0, '127.0.0.1', resolve));
  const address = listener.address();
  if (!address || typeof address === 'string') throw new Error('STARTTLS test listener has no port');
  try {
    const email = new EmailClient({ imapHost: 'localhost', smtpHost: 'localhost', smtpPort: address.port, smtpSecurity: 'startTLS', fromAddress: 'support@example.com' }, { resolve: async () => ({ username: 'support@example.com', password: new SecretString('test-password') }) }, { ca: certificate });
    const result = await email.sendMessage().invoke(call().call, { to: ['jane@example.com'], subject: 'Start TLS', text: 'Hello over TLS' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('sent');
    expect(authenticatedAfterTLS).toBe(true);
  } finally { await new Promise<void>(resolve => listener.close(() => resolve())); }
});


test('SMTP temporary recipient refusal retries and clears the claim', async () => {
  const before = submissions;
  loseFinalAnswer = false; recipientReply = 450; authReply = 235;
  const { call: invocation, heartbeats } = call();
  const result = await client().sendMessage().invoke(invocation, { to: ['jane@example.com'], subject: 'Try later', text: 'Hello' });
  expect(result.kind).toBe('retry');
  if (result.kind === 'retry') expect(result.failure.kind).toBe('AVAILABILITY');
  expect(heartbeats).toEqual([{ emailSubmittedCallId: 'send-test-call' }, undefined]);
  expect(submissions).toBe(before);
});

test('SMTP authentication failure is terminal before DATA', async () => {
  const before = submissions;
  loseFinalAnswer = false; recipientReply = 250; authReply = 535;
  const { call: invocation, heartbeats } = call();
  const result = await client().sendMessage().invoke(invocation, { to: ['jane@example.com'], subject: 'Wrong credential', text: 'Hello' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('providerRejected'); expect(result.failure?.kind).toBe('AUTHENTICATION'); }
  expect(heartbeats).toEqual([{ emailSubmittedCallId: 'send-test-call' }, undefined]);
  expect(submissions).toBe(before);
});

test('untrusted SMTP certificate is terminal before credentials', async () => {
  const email = new EmailClient({ imapHost: 'localhost', smtpHost: 'localhost', smtpPort: port, fromAddress: 'support@example.com' }, { resolve: async () => ({ username: 'support@example.com', password: new SecretString('test-password') }) });
  const { call: invocation, heartbeats } = call();
  const result = await email.sendMessage().invoke(invocation, { to: ['jane@example.com'], subject: 'Untrusted', text: 'Hello' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('providerRejected'); expect(result.failure?.kind).toBe('PROTOCOL'); }
  expect(heartbeats).toEqual([{ emailSubmittedCallId: 'send-test-call' }, undefined]);
});

test('permanent SMTP recipient refusal is terminal and clears the claim', async () => {
  const before = submissions;
  loseFinalAnswer = false; recipientReply = 550; authReply = 235;
  const { call: invocation, heartbeats } = call();
  const result = await client().sendMessage().invoke(invocation, { to: ['jane@example.com'], subject: 'Rejected', text: 'Hello' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('providerRejected'); expect(result.failure?.kind).toBe('PROVIDER_REJECTION'); }
  expect(heartbeats).toEqual([{ emailSubmittedCallId: 'send-test-call' }, undefined]);
  expect(submissions).toBe(before);
});
