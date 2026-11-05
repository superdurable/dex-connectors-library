// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { CredentialHeader } from '../wire_format.js';

const maxRecordedRequestBytes = 16 << 20;
const redactedCredential = '[REDACTED]';

/** One delayed piece of an event-stream reply. Durations are milliseconds. */
export interface FakeStreamChunk {
  /** Delay after the prior piece. */ delayMs?: number;
  /** Bytes to flush. */ data: string;
}

/** One scripted response, with zero status meaning HTTP 200. */
export interface FakeReply {
  /** HTTP status. */ statusCode?: number;
  /** Public response headers. */ headers?: Record<string, string>;
  /** Initial body bytes. */ body?: string;
  /** Silence before headers or a dropped connection, in milliseconds. */ delayMs?: number;
  /** Ordered flushed body chunks. */ streamChunks?: readonly FakeStreamChunk[];
  /** Drop the connection before sending headers. */ shouldDropConnection?: boolean;
}

/** Credential-redacted record of one provider request. */
export interface RecordedRequest {
  /** HTTP method. */ method: string;
  /** Request path and query. */ path: string;
  /** Request headers, excluding the credential slot. */ headers: Record<string, string>;
  /** Request body, bounded and credential-redacted. */ body: Buffer;
  /** Receipt time. */ receivedAt: Date;
  /** Whether the credential appeared in the declared header. */ hasCredentialInSlot: boolean;
  /** Whether the credential appeared anywhere else. */ hasCredentialOutsideSlot: boolean;
}

/** Scripted loopback provider for shared text-generation exchange tests. */
export class FakeProvider {
  readonly #server: Server;
  readonly #credentialHeader: CredentialHeader;
  readonly #credential: string;
  readonly #replies: FakeReply[] = [];
  readonly #requests: RecordedRequest[] = [];

  private constructor(credentialHeader: CredentialHeader, credential: string) {
    this.#credentialHeader = credentialHeader;
    this.#credential = credential;
    this.#server = createServer(this.serveRequest.bind(this));
  }

  /** Start one fixture server; close it after the suite. */
  static async start(credentialHeader: CredentialHeader, credential: string): Promise<FakeProvider> {
    if (!credentialHeader.name || !credential) throw new TypeError('textgentest fake provider needs a credential header and credential');
    const provider = new FakeProvider(credentialHeader, credential);
    await new Promise<void>(resolve => provider.#server.listen(0, '127.0.0.1', resolve));
    return provider;
  }

  /** Return the loopback base URL. */
  baseURL(): string {
    const address = this.#server.address();
    if (!address || typeof address === 'string') throw new Error('fake provider is closed');
    return `http://127.0.0.1:${address.port}`;
  }

  /** Queue replies in request order. */
  enqueueReplies(...replies: FakeReply[]): void { this.#replies.push(...replies); }

  /** Return a snapshot of sanitized requests. */
  requests(): readonly RecordedRequest[] { return [...this.#requests]; }

  /** Stop the fixture and all active connections. */
  async close(): Promise<void> {
    this.#server.closeAllConnections();
    await new Promise<void>(resolve => this.#server.close(() => resolve()));
  }

  private async serveRequest(incoming: IncomingMessage, response: ServerResponse): Promise<void> {
    const chunks: Buffer[] = [];
    let byteCount = 0;
    try {
      for await (const chunk of incoming) {
        const bytes = chunk as Buffer;
        byteCount += bytes.length;
        if (byteCount > maxRecordedRequestBytes) { response.writeHead(413); response.end(); return; }
        chunks.push(bytes);
      }
    } catch { return; }
    const body = Buffer.concat(chunks);
    this.#requests.push(this.recordRequest(incoming, body));
    const reply = this.#replies.shift();
    if (!reply) {
      response.writeHead(500, { 'Content-Type': 'application/json' });
      response.end('{"error":{"type":"textgentest_no_scripted_reply"}}');
      return;
    }
    if (!await this.waitForDelay(response, reply.delayMs ?? 0)) return;
    if (reply.shouldDropConnection) { response.destroy(); return; }
    response.writeHead(reply.statusCode ?? 200, reply.headers);
    if (reply.body) response.write(reply.body);
    for (const piece of reply.streamChunks ?? []) {
      if (!await this.waitForDelay(response, piece.delayMs ?? 0)) return;
      if (response.destroyed) return;
      response.write(piece.data);
    }
    response.end();
  }

  private recordRequest(incoming: IncomingMessage, body: Buffer): RecordedRequest {
    const slotName = this.#credentialHeader.name.toLowerCase();
    const expected = this.#credentialHeader.prefix + this.#credential;
    const headers: Record<string, string> = {};
    let hasCredentialOutsideSlot = (incoming.url ?? '').includes(this.#credential) || body.includes(this.#credential);
    for (const [name, value] of Object.entries(incoming.headers)) {
      const rendered = Array.isArray(value) ? value.join(',') : value ?? '';
      if (name === slotName) continue;
      if (rendered.includes(this.#credential)) hasCredentialOutsideSlot = true;
      headers[name] = rendered.replaceAll(this.#credential, redactedCredential);
    }
    return {
      method: incoming.method ?? '', path: (incoming.url ?? '').replaceAll(this.#credential, redactedCredential),
      headers, body: Buffer.from(body.toString().replaceAll(this.#credential, redactedCredential)),
      receivedAt: new Date(), hasCredentialInSlot: incoming.headers[slotName] === expected,
      hasCredentialOutsideSlot,
    };
  }

  private async waitForDelay(response: ServerResponse, delayMs: number): Promise<boolean> {
    if (delayMs <= 0) return !response.destroyed;
    return new Promise(resolve => {
      const timer = setTimeout(() => { response.off('close', onClose); resolve(!response.destroyed); }, delayMs);
      const onClose = () => { clearTimeout(timer); resolve(false); };
      response.once('close', onClose);
    });
  }
}
