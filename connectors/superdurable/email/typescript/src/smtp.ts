// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { connect as connectTCP, type Socket } from 'node:net';
import { connect as connectTLS, type TLSSocket } from 'node:tls';
import type { Credentials } from './generated/models.js';
import { classifyMailError, MailFailure, type EmailClient } from './client.js';

/** A complete message and its SMTP envelope. */
export interface OutgoingMail { from: string; fromName: string; to: string[]; cc: string[]; bcc: string[]; replyTo?: string; subject: string; text: string; messageId: string; inReplyTo?: string; references?: string[]; date: Date }
/** Classified SMTP submission result. */
export type SubmissionResult = { outcome: 'accepted' } | { outcome: 'notSent' | 'uncertain'; failure: MailFailure };

class SMTPReplyError extends Error {
  constructor(readonly code: number, readonly command: string) { super(`SMTP ${code} during ${command}`); }
}

class SMTPWire {
  private buffer = '';
  private lines: string[] = [];
  private waiter?: { resolve: (line: string) => void; reject: (error: Error) => void };
  private closedError?: Error;
  constructor(readonly socket: Socket | TLSSocket) {
    socket.on('data', this.onData.bind(this));
    socket.on('error', this.onError.bind(this));
    socket.on('close', () => this.onError(new Error('SMTP connection closed')));
    socket.setTimeout(30_000, () => socket.destroy(new Error('SMTP command timed out')));
  }
  private onData(data: Buffer): void {
    this.buffer += data.toString('utf8');
    for (;;) {
      const end = this.buffer.indexOf('\n');
      if (end < 0) break;
      const line = this.buffer.slice(0, end).replace(/\r$/, '');
      this.buffer = this.buffer.slice(end + 1);
      if (this.waiter) { const waiter = this.waiter; this.waiter = undefined; waiter.resolve(line); }
      else this.lines.push(line);
    }
    if (this.buffer.length > 64 * 1024) this.socket.destroy(new Error('SMTP response line too long'));
  }
  private onError(error: Error): void {
    this.closedError = error;
    if (this.waiter) { const waiter = this.waiter; this.waiter = undefined; waiter.reject(error); }
  }
  private readLine(): Promise<string> {
    const line = this.lines.shift();
    if (line !== undefined) return Promise.resolve(line);
    if (this.closedError) return Promise.reject(this.closedError);
    return new Promise((resolve, reject) => { this.waiter = { resolve, reject }; });
  }
  async reply(command: string): Promise<{ code: number; lines: string[] }> {
    const lines: string[] = [];
    for (let index = 0; index < 100; index++) {
      const line = await this.readLine();
      if (!/^\d{3}[ -]/.test(line)) throw new MailFailure('rejected', 'PROTOCOL', 'the SMTP server returned an invalid reply');
      lines.push(line);
      if (line[3] === ' ') {
        const code = Number(line.slice(0, 3));
        if (code >= 400) throw new SMTPReplyError(code, command);
        return { code, lines };
      }
    }
    throw new MailFailure('rejected', 'PROTOCOL', 'the SMTP server sent too many reply lines');
  }
  async command(command: string, label: string, expected: number[]): Promise<{ code: number; lines: string[] }> {
    await this.write(`${command}\r\n`);
    const response = await this.reply(label);
    if (!expected.includes(response.code)) throw new MailFailure('rejected', 'PROTOCOL', `the SMTP server answered ${label} with an unexpected code`);
    return response;
  }
  setTimeout(milliseconds: number): void { this.socket.setTimeout(milliseconds); }
  async write(content: string): Promise<void> {
    if (this.closedError) throw this.closedError;
    await new Promise<void>((resolve, reject) => { this.socket.write(content, error => error ? reject(error) : resolve()); });
  }
  detach(): void { this.socket.setTimeout(0); this.socket.removeAllListeners('timeout'); this.socket.removeAllListeners('data'); this.socket.removeAllListeners('error'); this.socket.removeAllListeners('close'); }
}

function classifySMTP(error: unknown, command: string): MailFailure {
  if (error instanceof MailFailure) return error;
  if (error instanceof SMTPReplyError) {
    if (Math.floor(error.code / 100) === 4) return new MailFailure('retry', 'AVAILABILITY', `the SMTP server temporarily refused ${error.command}`);
    if ([530, 534, 535, 538].includes(error.code)) return new MailFailure('rejected', 'AUTHENTICATION', `the SMTP server refused ${error.command}`);
    if (error.code === 552) return new MailFailure('rejected', 'RESPONSE_TOO_LARGE', 'the SMTP server refused the message size');
    return new MailFailure('rejected', 'PROVIDER_REJECTION', `the SMTP server refused ${error.command}`);
  }
  const classified = classifyMailError(error, 'SMTP');
  if (classified.kind === 'PROTOCOL' && classified.message === 'the SMTP server returned an unusable response') return new MailFailure('retry', 'TRANSPORT', `the SMTP connection was lost during ${command}`);
  return classified;
}

async function connectSocket(host: string, port: number, secure: boolean, ca?: string | Buffer): Promise<Socket | TLSSocket> {
  return new Promise((resolve, reject) => {
    const socket = secure ? connectTLS({ host, port, servername: host, rejectUnauthorized: true, ...(ca ? { ca } : {}) }) : connectTCP({ host, port });
    const timer = setTimeout(() => socket.destroy(new Error('SMTP connection timed out')), 15_000);
    const event = secure ? 'secureConnect' : 'connect';
    socket.once(event, () => { clearTimeout(timer); socket.removeListener('error', reject); resolve(socket); });
    socket.once('error', error => { clearTimeout(timer); reject(error); });
  });
}

async function upgradeTLS(wire: SMTPWire, host: string, ca?: string | Buffer): Promise<SMTPWire> {
  const raw = wire.socket;
  wire.detach();
  const socket = connectTLS({ socket: raw, servername: host, rejectUnauthorized: true, ...(ca ? { ca } : {}) });
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => socket.destroy(new Error('SMTP TLS handshake timed out')), 15_000);
    socket.once('secureConnect', () => { clearTimeout(timer); socket.removeListener('error', reject); resolve(); });
    socket.once('error', error => { clearTimeout(timer); reject(error); });
  });
  return new SMTPWire(socket);
}

function addressHeader(address: string, name = ''): string {
  if (!name) return `<${address}>`;
  if (!/^[\x20-\x7e]*$/.test(name) || Buffer.byteLength(name) > 68) return `${encodedHeader(name)} <${address}>`;
  return `"${name.replace(/["\\]/g, '\\$&')}" <${address}>`;
}
function encodedHeader(text: string): string {
  if (/^[\x20-\x7e]*$/.test(text) && Buffer.byteLength(text) <= 68) return text;
  const words: string[] = [];
  let part = '';
  for (const character of text) {
    if (Buffer.byteLength(part + character) > 45 && part) { words.push(`=?UTF-8?B?${Buffer.from(part).toString('base64')}?=`); part = ''; }
    part += character;
  }
  if (part) words.push(`=?UTF-8?B?${Buffer.from(part).toString('base64')}?=`);
  return words.join('\r\n ');
}
function quotedPrintable(text: string): string {
  const lines = text.replace(/\r\n?/g, '\n').split('\n');
  return lines.map(line => {
    let output = '';
    let width = 0;
    const bytes = Buffer.from(line);
    for (let index = 0; index < bytes.length; index++) {
      const byte = bytes[index]!;
      const isTrailingWhitespace = index === bytes.length - 1 && (byte === 9 || byte === 32);
      const token = !isTrailingWhitespace && (byte === 9 || byte === 32 || byte >= 33 && byte <= 60 || byte >= 62 && byte <= 126)
        ? String.fromCharCode(byte) : `=${byte.toString(16).toUpperCase().padStart(2, '0')}`;
      if (width + token.length > 75) { output += '=\r\n'; width = 0; }
      output += token;
      width += token.length;
    }
    return output;
  }).join('\r\n');
}

/** Render one UTF-8 plain-text message without a Bcc header. */
export function renderMail(message: OutgoingMail): string {
  const headers = [`Date: ${message.date.toUTCString()}`, `From: ${addressHeader(message.from, message.fromName)}`,
    `To: ${message.to.map(address => addressHeader(address)).join(', ')}`];
  if (message.cc.length) headers.push(`Cc: ${message.cc.map(address => addressHeader(address)).join(', ')}`);
  if (message.replyTo) headers.push(`Reply-To: ${addressHeader(message.replyTo)}`);
  headers.push(`Subject: ${encodedHeader(message.subject)}`, `Message-ID: <${message.messageId}>`);
  if (message.inReplyTo) headers.push(`In-Reply-To: <${message.inReplyTo}>`);
  if (message.references?.length) headers.push(`References: ${message.references.map(value => `<${value}>`).join(' ')}`);
  headers.push('MIME-Version: 1.0', 'Content-Type: text/plain; charset=utf-8', 'Content-Transfer-Encoding: quoted-printable');
  return `${headers.join('\r\n')}\r\n\r\n${quotedPrintable(message.text)}\r\n`;
}

/** Submit one SMTP message, distinguishing failure before and after the final dot. */
export async function submitMail(client: EmailClient, credentials: Credentials, message: OutgoingMail): Promise<SubmissionResult> {
  let wire: SMTPWire | undefined;
  let phase = 'connection';
  let finalDotWritten = false;
  let operationDeadline: NodeJS.Timeout | undefined;
  try {
    wire = new SMTPWire(await connectSocket(client.config.smtpHost, client.config.smtpPort, client.config.smtpSecurity === 'implicitTLS', client.ca));
    operationDeadline = setTimeout(() => wire?.socket.destroy(new Error('SMTP operation timed out')), 140_000);
    await wire.reply('greeting');
    if (client.config.smtpSecurity === 'startTLS') {
      phase = 'EHLO';
      const greeting = await wire.command('EHLO localhost', 'EHLO', [250]);
      if (!greeting.lines.some(line => /\bSTARTTLS\b/i.test(line))) throw new MailFailure('rejected', 'PROTOCOL', 'the SMTP server does not offer STARTTLS, so the connector did not authenticate');
      phase = 'STARTTLS';
      await wire.command('STARTTLS', 'STARTTLS', [220]);
      wire = await upgradeTLS(wire, client.config.smtpHost, client.ca);
    }
    phase = 'EHLO';
    const ehlo = await wire.command('EHLO localhost', 'EHLO', [250]);
    const capabilities = ehlo.lines.join(' ');
    const username = credentials.smtpUsername || credentials.username;
    const password = credentials.smtpPassword?.reveal() || credentials.password.reveal();
    if (/\bAUTH\b[^\r\n]*\bPLAIN\b/i.test(capabilities)) {
      phase = 'AUTH';
      const response = Buffer.from(`\0${username}\0${password}`).toString('base64');
      const initial = await wire.command(`AUTH PLAIN ${response}`, 'AUTH', [235, 334]);
      if (initial.code === 334) await wire.command(response, 'AUTH', [235]);
    } else if (/\bAUTH\b[^\r\n]*\bLOGIN\b/i.test(capabilities)) {
      phase = 'AUTH';
      await wire.command('AUTH LOGIN', 'AUTH', [334]);
      await wire.command(Buffer.from(username).toString('base64'), 'AUTH', [334]);
      await wire.command(Buffer.from(password).toString('base64'), 'AUTH', [235]);
    } else throw new MailFailure('rejected', 'AUTHENTICATION', 'the SMTP server offers neither AUTH PLAIN nor AUTH LOGIN over TLS');
    const content = renderMail(message);
    const size = /\bSIZE\s+(\d+)/i.exec(capabilities)?.[1];
    const sizeLimit = size ? Number(size) : 0;
    if (sizeLimit > 0 && Buffer.byteLength(content) > sizeLimit) throw new MailFailure('rejected', 'PROVIDER_REJECTION', 'the message exceeds the SMTP server size limit');
    phase = 'MAIL FROM';
    await wire.command(`MAIL FROM:<${message.from}>${sizeLimit > 0 ? ` SIZE=${Buffer.byteLength(content)}` : ''}`, phase, [250]);
    const seenRecipients = new Set<string>();
    const recipients = [...message.to, ...message.cc, ...message.bcc].filter(value => { const key = value.toLowerCase(); if (seenRecipients.has(key)) return false; seenRecipients.add(key); return true; });
    for (const recipient of recipients) { phase = 'RCPT TO'; await wire.command(`RCPT TO:<${recipient}>`, phase, [250, 251]); }
    phase = 'DATA';
    await wire.command('DATA', phase, [354]);
    const escaped = content.replace(/(^|\r\n)\./g, '$1..');
    await wire.write(escaped);
    phase = 'the end of the message';
    wire.setTimeout(90_000);
    finalDotWritten = true;
    await wire.write('.\r\n');
    const dataReply = await wire.reply(phase);
    if (dataReply.code !== 250) throw new MailFailure('rejected', 'PROTOCOL', 'the SMTP server returned an unexpected acceptance code');
    wire.setTimeout(2_000);
    try { await wire.command('QUIT', 'QUIT', [221]); } catch { /* SMTP accepted the message before this courtesy command. */ }
    return { outcome: 'accepted' };
  } catch (error) {
    const failure = classifySMTP(error, phase);
    if (finalDotWritten && !(error instanceof SMTPReplyError) && !(error instanceof MailFailure && error.message === 'the SMTP server returned an unexpected acceptance code')) return { outcome: 'uncertain', failure: new MailFailure('rejected', 'TRANSPORT', "the message was submitted but the SMTP server's answer did not arrive, so it may have been sent; it is not submitted again") };
    return { outcome: 'notSent', failure };
  } finally { if (operationDeadline) clearTimeout(operationDeadline); wire?.socket.destroy(); }
}
