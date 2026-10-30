// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { BodyTooLargeError, readBoundedBody } from '../providerhttp/index.js';
import { ReauthorizationRequiredError } from '../errors.js';
import { isPrivateBrokerHost } from '../netpolicy.js';
import { RFC3339NanoSchema } from './model.js';

/** Broker temporarily unavailable; another read may succeed. */
export class CredentialBrokerUnavailableError extends Error { constructor() { super('credential broker is unavailable'); this.name = 'CredentialBrokerUnavailableError'; } }
/** Broker rejected the scope or returned an invalid answer. */
export class CredentialBrokerRejectedError extends Error { constructor() { super('credential broker rejected the request or answered invalidly'); this.name = 'CredentialBrokerRejectedError'; } }
/** Loaded scope no longer selects this shared connection. */
export class ConnectionNotSelectedError extends Error { constructor() { super('project connection is not selected by this scope'); this.name = 'ConnectionNotSelectedError'; } }
/** Broker returned the same revision after provider rejection; the call must stop. */
export class RejectedCredentialNotRefreshedError extends Error { constructor() { super('the rejected credential was not refreshed'); this.name = 'RejectedCredentialNotRefreshedError'; } }

/** Complete provider credential returned only at the private boundary. */
export interface BrokerCredential {
  /** Credential object without host-kept renewal secrets. */ credential: Record<string, unknown>;
  /** Positive broker revision. */ revision: number;
  /** Optional issuance time. */ issuedAt?: string;
  /** Optional expiry time. */ expiresAt?: string | null;
}

/** Additive broker wire response, including terminal statuses. */
export const BrokerAnswerSchema = z.object({
  status: z.enum(['READY', 'REAUTHORIZATION_REQUIRED', 'CONNECTION_NOT_SELECTED']),
  credential: z.record(z.string(), z.unknown()).nullable().optional(), revision: z.number().int().nonnegative().optional(),
  issuedAt: RFC3339NanoSchema.nullable().optional(), expiresAt: RFC3339NanoSchema.nullable().optional(),
});

/** Client for one trusted credential broker endpoint and scope token source. */
export class CredentialBrokerClient {
  #token: string | undefined;
  /** Bind a trusted endpoint, token source, and optional transport; tokens renew only after 401. */
  constructor(private readonly endpoint: string, private readonly readToken: (renew?: boolean) => Promise<string>, private readonly transport: typeof fetch = fetch) {
    let parsed: URL;
    try { parsed = new URL(endpoint); } catch { throw new TypeError('credential broker endpoint is invalid'); }
    if (!['https:', 'http:'].includes(parsed.protocol) || parsed.username || parsed.password || parsed.search || parsed.hash || (parsed.protocol === 'http:' && !isPrivateBrokerHost(parsed.hostname))) throw new TypeError('credential broker endpoint is invalid');
  }

  /** Read a shared credential; rejectedRevision is sent only when positive. */
  async read(sharedConnectionId: string, rejectedRevision = 0, signal?: AbortSignal): Promise<BrokerCredential> {
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(sharedConnectionId)) throw new TypeError('shared connection ID is invalid');
    const timeoutSignal = AbortSignal.timeout(40_000);
    const requestSignal = signal ? AbortSignal.any([signal, timeoutSignal]) : timeoutSignal;
    return new Promise<BrokerCredential>((resolve, reject) => {
      const onAbort = () => reject(new CredentialBrokerUnavailableError());
      if (requestSignal.aborted) { onAbort(); return; }
      requestSignal.addEventListener('abort', onAbort, { once: true });
      void this.readWithinDeadline(sharedConnectionId, rejectedRevision, requestSignal).then(resolve, reject).finally(() => requestSignal.removeEventListener('abort', onAbort));
    });
  }

  private async readWithinDeadline(sharedConnectionId: string, rejectedRevision: number, requestSignal: AbortSignal): Promise<BrokerCredential> {
    let token = this.#token ?? await this.readToken();
    if (!isHeaderSafeBrokerToken(token)) throw new CredentialBrokerRejectedError();
    this.#token = token;
    const payload = JSON.stringify({ sharedConnectionId, ...(rejectedRevision > 0 ? { rejectedRevision } : {}) });
    let response: Response;
    response = await this.post(token, payload, requestSignal);
    if (response.status === 401) {
      const renewedToken = await this.readToken(true);
      if (!isHeaderSafeBrokerToken(renewedToken) || renewedToken === token) throw new CredentialBrokerRejectedError();
      token = renewedToken;
      this.#token = token;
      response = await this.post(token, payload, requestSignal);
    }
    if (response.status !== 200) {
      if ([408, 425, 429].includes(response.status) || response.status >= 500) throw new CredentialBrokerUnavailableError();
      throw new CredentialBrokerRejectedError();
    }
    let body: Uint8Array;
    try { body = await readBoundedBody(responseChunks(response.body), 1 << 20); }
    catch (error: unknown) { throw error instanceof BodyTooLargeError || error instanceof CredentialBrokerRejectedError ? new CredentialBrokerRejectedError() : new CredentialBrokerUnavailableError(); }
    let decoded: unknown;
    try { decoded = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
    catch { throw new CredentialBrokerRejectedError(); }
    const parsed = BrokerAnswerSchema.safeParse(decoded);
    if (!parsed.success) throw new CredentialBrokerRejectedError();
    if (parsed.data.status === 'REAUTHORIZATION_REQUIRED') throw new ReauthorizationRequiredError();
    if (parsed.data.status === 'CONNECTION_NOT_SELECTED') throw new ConnectionNotSelectedError();
    if (!parsed.data.credential || !parsed.data.revision || Buffer.byteLength(JSON.stringify(parsed.data.credential)) > 64 << 10) throw new CredentialBrokerRejectedError();
    if (parsed.data.expiresAt && Date.parse(parsed.data.expiresAt) <= Date.now()) throw new CredentialBrokerUnavailableError();
    return {
      credential: parsed.data.credential, revision: parsed.data.revision,
      ...(parsed.data.issuedAt ? { issuedAt: parsed.data.issuedAt } : {}),
      ...(parsed.data.expiresAt !== undefined ? { expiresAt: parsed.data.expiresAt } : {}),
    };
  }

  private async post(token: string, payload: string, signal?: AbortSignal): Promise<Response> {
    try {
      return await this.transport(this.endpoint, {
        method: 'POST', redirect: 'manual', signal: signal ?? null,
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json' }, body: payload,
      });
    } catch { throw new CredentialBrokerUnavailableError(); }
  }
}

function isHeaderSafeBrokerToken(token: string): boolean {
  return Boolean(token) && ![...token].some(character => { const code = character.charCodeAt(0); return code < 0x21 || code > 0x7e; });
}

async function* responseChunks(body: ReadableStream<Uint8Array> | null): AsyncGenerator<Uint8Array> {
  if (!body) throw new CredentialBrokerRejectedError();
  const reader = body.getReader();
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) return;
      yield next.value;
    }
  } finally {
    try { await reader.cancel(); } catch { /* A failed best-effort cancellation does not change the response result. */ }
    reader.releaseLock();
  }
}
