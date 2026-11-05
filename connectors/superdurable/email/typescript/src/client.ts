// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ImapFlow } from 'imapflow';
import { isIP } from 'node:net';
import type { Call, CredentialProvider, Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { SearchMessagesOperation, GetMessageOperation, SendMessageOperation, ReplyToMessageOperation, MoveMessageOperation, SetFlagsOperation } from './operations.js';

/** One classified mail protocol failure, without provider text or credentials. */
export class MailFailure extends Error {
  /** Decision for a failed protocol exchange. */
  constructor(readonly outcome: 'retry' | 'rejected' | 'notFound' | 'invalidResponse', readonly kind: Failure['kind'], message: string) { super(message); }
}

/** Unavailable or invalid connection credentials are a local connector defect. */
export class CredentialFailure extends MailFailure { constructor() { super('rejected', 'AUTHENTICATION', 'connection credentials are unavailable'); } }

/** Optional trust roots for a private IMAP or SMTP certificate authority. */
export interface EmailClientOptions { /** PEM certificate authority; host verification remains enabled. */ ca?: string | Buffer; /** Clock for sent message dates. */ now?: () => Date }

/** Authenticated IMAP and SMTP client. Each operation opens a separate connection. */
export class EmailClient {
  readonly config: Required<Config>;
  readonly credentials: CredentialProvider<Credentials>;
  readonly ca?: string | Buffer;
  readonly now: () => Date;
  private readonly imapCommandErrors = new WeakMap<ImapFlow, unknown>();

  /** Validate startup configuration and retain credential resolution for each call. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: EmailClientOptions = {}) {
    if (!credentials) throw new TypeError('email credential provider is required');
    for (const [label, host] of [['IMAP', config.imapHost], ['SMTP', config.smtpHost]] as const) {
      if (!isHost(host)) throw new TypeError(`email ${label} host must be a host name or IP address without a scheme or port`);
    }
    const imapPort = config.imapPort || 993, smtpPort = config.smtpPort || 465;
    for (const port of [imapPort, smtpPort]) if (!Number.isInteger(port) || port < 1 || port > 65535) throw new TypeError('email port must be from 1 through 65535');
    if (config.imapSecurity && !['implicitTLS', 'startTLS'].includes(config.imapSecurity)) throw new TypeError('invalid IMAP security mode');
    if (config.smtpSecurity && !['implicitTLS', 'startTLS'].includes(config.smtpSecurity)) throw new TypeError('invalid SMTP security mode');
    if (config.fromAddress && !isBareAddress(config.fromAddress)) throw new TypeError('email fromAddress must be one bare ASCII address');
    if (config.fromName && (config.fromName.trim() !== config.fromName || hasControls(config.fromName) || [...config.fromName].length > 128)) throw new TypeError('email fromName must be at most 128 characters without surrounding spaces or line breaks');
    this.config = { imapHost: config.imapHost, imapPort, imapSecurity: config.imapSecurity || 'implicitTLS', smtpHost: config.smtpHost, smtpPort, smtpSecurity: config.smtpSecurity || 'implicitTLS', fromAddress: config.fromAddress || '', fromName: config.fromName || '' };
    this.credentials = credentials;
    this.ca = options.ca;
    this.now = options.now || (() => new Date());
  }

  /** Search one IMAP mailbox. */ searchMessages(): SearchMessagesOperation { return new SearchMessagesOperation(this); }
  /** Read one IMAP message. */ getMessage(): GetMessageOperation { return new GetMessageOperation(this); }
  /** Submit a new SMTP message. */ sendMessage(): SendMessageOperation { return new SendMessageOperation(this); }
  /** Read a source and submit its threaded SMTP reply. */ replyToMessage(): ReplyToMessageOperation { return new ReplyToMessageOperation(this); }
  /** Move one IMAP message. */ moveMessage(): MoveMessageOperation { return new MoveMessageOperation(this); }
  /** Set absolute IMAP flags. */ setFlags(): SetFlagsOperation { return new SetFlagsOperation(this); }

  /** Resolve and validate current credentials without persisting secret values. */
  async resolveCredentials(call: Call): Promise<Credentials> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call); }
    catch { throw new CredentialFailure(); }
    if (!credentials || !credentials.username || !credentials.password?.reveal() ||
      [credentials.username, credentials.password.reveal(), credentials.smtpUsername || '', credentials.smtpPassword?.reveal() || ''].some(hasControls)) {
      throw new CredentialFailure();
    }
    return credentials;
  }

  /** Classify and clear a command error retained only in memory by the IMAP logger. */
  consumeIMAPCommandFailure(session: ImapFlow): MailFailure | undefined {
    const error = this.imapCommandErrors.get(session);
    this.imapCommandErrors.delete(session);
    return error ? classifyMailError(error, 'IMAP') : undefined;
  }

  /** Open an authenticated IMAP connection with mandatory verified TLS. */
  async openIMAP(credentials: Credentials): Promise<ImapFlow> {
    const client = new ImapFlow({ host: this.config.imapHost, port: this.config.imapPort,
      secure: this.config.imapSecurity === 'implicitTLS', doSTARTTLS: this.config.imapSecurity === 'startTLS',
      tls: { rejectUnauthorized: true, ...(this.ca ? { ca: this.ca } : {}), servername: this.config.imapHost },
      auth: { user: credentials.username, pass: credentials.password.reveal() },
      logger: { debug: () => {}, info: () => {}, warn: event => { const error = (event as { err?: unknown }).err; if (error) this.imapCommandErrors.set(client, error); }, error: () => {} }, disableAutoIdle: true, connectionTimeout: 15_000, greetingTimeout: 15_000, socketTimeout: 25_000,
      maxResponseSize: 8 * 1024 * 1024 });
    try { await client.connect(); return client; }
    catch (error) { try { client.close(); } catch { /* A failed connection has no result to alter. */ } throw classifyMailError(error, 'IMAP'); }
  }
}

/** Convert a protocol error to a stable branch or retry decision. */
export function classifyMailError(error: unknown, protocol: 'IMAP' | 'SMTP'): MailFailure {
  if (error instanceof MailFailure) return error;
  const candidate = error as { code?: string; tlsFailed?: boolean; responseCode?: string; serverResponseCode?: string; responseStatus?: string; status?: string; command?: string };
  const code = String(candidate?.code || '');
  const responseCode = String(candidate?.serverResponseCode || candidate?.responseCode || '').toUpperCase();
  const status = String(candidate?.responseStatus || candidate?.status || '').toUpperCase();
  if (['ENOTFOUND', 'EAI_NONAME'].includes(code)) return new MailFailure('rejected', 'VALIDATION', `the ${protocol} host name does not resolve`);
  if (/^(?:CERT_|UNABLE_|DEPTH_|SELF_SIGNED|ERR_TLS_CERT|ERR_SSL_)/.test(code)) return new MailFailure('rejected', 'PROTOCOL', `the ${protocol} server certificate failed verification for the configured host`);
  if (code === 'EPROTO' && /wrong version number|packet length too long|ssl.*alert|tls.*handshake|handshake failure/i.test(String((error as Error).message))) return new MailFailure('rejected', 'PROTOCOL', `the ${protocol} server did not answer with TLS`);
  if (candidate?.tlsFailed) return new MailFailure('rejected', 'PROTOCOL', `the ${protocol} server did not complete required STARTTLS`);
  if (['ECONNREFUSED', 'ECONNRESET', 'ECONNABORTED', 'ETIMEDOUT', 'ETIMEOUT', 'EPIPE', 'EAI_AGAIN', 'EHOSTUNREACH', 'ENETUNREACH', 'CONNECT_TIMEOUT', 'GREETING_TIMEOUT', 'UPGRADE_TIMEOUT', 'SocketTimeout', 'NoConnection', 'ConnectionClosed', 'EConnectionClosed', 'ClosedAfterConnectTLS', 'ClosedAfterConnectText'].includes(code)) return new MailFailure('retry', 'TRANSPORT', `the ${protocol} server could not be reached`);
  if (protocol === 'IMAP') {
    if (status === 'BYE' || ['UNAVAILABLE', 'INUSE', 'SERVERBUG'].includes(responseCode)) return new MailFailure('retry', 'AVAILABILITY', 'the IMAP server is temporarily unavailable');
    if (responseCode === 'LIMIT') return new MailFailure('retry', 'RATE_LIMIT', 'the IMAP server limited the request');
    if (['AUTHENTICATIONFAILED', 'EXPIRED', 'PRIVACYREQUIRED', 'CONTACTADMIN'].includes(responseCode) || candidate?.command === 'LOGIN' || candidate?.command === 'AUTHENTICATE') return new MailFailure('rejected', 'AUTHENTICATION', 'the IMAP login was refused');
    if (['AUTHORIZATIONFAILED', 'NOPERM'].includes(responseCode)) return new MailFailure('rejected', 'AUTHORIZATION', 'the IMAP operation was not permitted');
    if (['NONEXISTENT', 'TRYCREATE'].includes(responseCode)) return new MailFailure('rejected', 'NOT_FOUND', 'the mailbox does not exist');
    if (status === 'BAD') return new MailFailure('rejected', 'PROTOCOL', 'the IMAP server rejected the command');
    if (status === 'NO') return new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server refused the command');
    return new MailFailure('invalidResponse', 'PROTOCOL', 'the IMAP server returned an unusable response');
  }
  return new MailFailure('rejected', 'PROTOCOL', 'the SMTP server returned an unusable response');
}

/** Validate a host using the Go connector's DNS label and IP rules. */
export function isHost(host: string): boolean {
  if (!host || host.length > 253 || host.trim() !== host) return false;
  if (isIP(host)) return true;
  return host.replace(/\.$/, '').split('.').every(label => label.length > 0 && label.length <= 63 && /^[A-Za-z0-9_](?:[A-Za-z0-9_-]*[A-Za-z0-9_])?$/.test(label));
}

/** Check header or credential control characters. */
export function hasControls(value: string): boolean { return [...value].some(character => { const codePoint = character.codePointAt(0)!; return codePoint < 32 || codePoint >= 127 && codePoint <= 159 || codePoint === 0x2028 || codePoint === 0x2029; }); }

/** Accept a single ASCII mailbox without display name or comments. */
export function isBareAddress(address: string): boolean {
  if (!address || address.length > 254 || /[^\x21-\x7e]/.test(address)) return false;
  const at = address.lastIndexOf('@');
  if (at < 1 || !isHost(address.slice(at + 1)) || !address.slice(at + 1).includes('.')) return false;
  const local = address.slice(0, at);
  return /^[A-Za-z0-9!#$%&'*+\-/=?^_`{|}~]+(?:\.[A-Za-z0-9!#$%&'*+\-/=?^_`{|}~]+)*$/.test(local);
}
