// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { BodyTooLargeError, readBoundedBody } from '../providerhttp/index.js';
import { ReauthorizationRequiredError } from '../errors.js';
import { isValidCredentialBrokerEndpoint } from './credential_broker_endpoint.js';
import { RFC3339NanoSchema } from './model.js';
import { matchesRelaySignature, parseRelaySignature, type RelayedRequestHeaders } from '../relaysignature/index.js';

/** Broker temporarily unavailable; another read may succeed. */
export class CredentialBrokerUnavailableError extends Error { constructor() { super('credential broker is unavailable'); this.name = 'CredentialBrokerUnavailableError'; } }
/** Broker rejected the scope or returned an invalid answer. */
export class CredentialBrokerRejectedError extends Error { constructor() { super('credential broker rejected the request or answered invalidly'); this.name = 'CredentialBrokerRejectedError'; } }
/** Loaded scope no longer selects this shared connection. */
export class ConnectionNotSelectedError extends Error { constructor() { super('project connection is not selected by this scope'); this.name = 'ConnectionNotSelectedError'; } }
/** The broker does not relay this shared connection's events; the connector receives them directly or fails. */
export class RelayedTriggerNotRelayedError extends Error { constructor() { super("the credential broker does not relay this connection's events"); this.name = 'RelayedTriggerNotRelayedError'; } }
/** A relayed request's signature or timestamp is missing, malformed, stale, or does not match the scope token. */
export class RelaySignatureInvalidError extends Error { constructor() { super('relayed request signature is missing, invalid or stale'); this.name = 'RelaySignatureInvalidError'; } }
/**
 * A relayed request's well-formed, fresh signature does not match the scope token, and the token was reread
 * within the last 30 seconds, so a rotated token cannot be checked yet. Receivers answer it as retryable, so
 * the host's retry is accepted once the token can be reread; a forged request is never delivered.
 */
export class RelayTokenRenewalDeferredError extends CredentialBrokerUnavailableError {
  constructor() { super(); this.message = 'relayed request signature cannot be checked against a renewed scope token yet'; this.name = 'RelayTokenRenewalDeferredError'; }
}
/** Broker returned the same revision after provider rejection; the call must stop. */
export class RejectedCredentialNotRefreshedError extends Error { constructor() { super('the rejected credential was not refreshed'); this.name = 'RejectedCredentialNotRefreshedError'; } }

/** Spacing of token rereads after signature mismatches, because the receiver's route is public. */
const relayTokenRenewalIntervalMs = 30_000;
/** Marks a token renewal that the 30-second spacing defers. */
const renewalDeferred = Symbol('renewalDeferred');
/** The broker's limit for one registration request. */
const maximumRelayedTriggerRegistrationBytes = 4 << 10;
/** Bound of one broker exchange, including the scope token read it waits for. */
const brokerExchangeTimeoutMs = 40_000;

export { RelaySignatureHeader, RelayTimestampHeader, signRelayedRequest, type RelayedRequestHeaders } from '../relaysignature/index.js';

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
  readonly #requestURL: string;
  #tokenLockTail: Promise<void> = Promise.resolve();
  #lastMismatchRenewalMs: number | undefined;
  #mismatchRenewal: Promise<string | undefined> | undefined;
  /**
   * Bind a trusted endpoint, token source, optional transport, and clock in milliseconds; tokens renew only
   * after 401 or a relay signature mismatch.
   *
   * Concurrent reads share one token read and one renewal: a caller whose refused token another caller already
   * replaced reuses the newer token instead of renewing again. A caller's abort or 40-second deadline also ends its
   * wait for the token and any token read it started, so a stalled read cannot block later calls.
   */
  constructor(endpoint: string, private readonly readToken: (renew?: boolean) => Promise<string>, private readonly transport: typeof fetch = fetch, private readonly nowMs: () => number = Date.now) {
    if (!isValidCredentialBrokerEndpoint(endpoint)) throw new TypeError('credential broker endpoint is invalid');
    this.#requestURL = new URL(endpoint).href;
  }

  /** Read a shared credential; rejectedRevision is sent only when positive. */
  async read(sharedConnectionId: string, rejectedRevision = 0, signal?: AbortSignal): Promise<BrokerCredential> {
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(sharedConnectionId)) throw new TypeError('shared connection ID is invalid');
    const payload = JSON.stringify({ sharedConnectionId, ...(rejectedRevision > 0 ? { rejectedRevision } : {}) });
    const body = await this.#exchangeWithinDeadline(this.#requestURL, payload, signal);
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

  /**
   * Register a shared connection's relayed Trigger route at registrationPath on the broker endpoint's
   * origin; LoadedProject.registerRelayedTrigger documents the answers. The scope token authorizes it.
   */
  async registerRelayedTrigger(sharedConnectionId: string, connectionName: string, registrationPath: string, signal?: AbortSignal): Promise<void> {
    const target = this.originURL(registrationPath);
    const payload = JSON.stringify({ sharedConnectionId, connectionName });
    if (Buffer.byteLength(payload) > maximumRelayedTriggerRegistrationBytes) throw new TypeError('relayed Trigger registration request is too large');
    const body = await this.#exchangeWithinDeadline(target, payload, signal);
    let decoded: unknown;
    try { decoded = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
    catch { throw new CredentialBrokerRejectedError(); }
    const status = decoded !== null && typeof decoded === 'object' && !Array.isArray(decoded) ? (decoded as Record<string, unknown>).status : undefined;
    if (status === 'REGISTERED') return;
    if (status === 'CONNECTION_NOT_SELECTED') throw new ConnectionNotSelectedError();
    if (status === 'NOT_RELAYED') throw new RelayedTriggerNotRelayedError();
    throw new CredentialBrokerRejectedError();
  }

  /**
   * Verify that the host signed a relayed Trigger request with the scope token; LoadedProject.verifyRelayedRequest
   * documents the contract. Only a well-formed, fresh signature that does not match may reread the token,
   * at most once every 30 seconds; a request that waits for an in-flight reread is checked against its result.
   */
  async verifyRelayedRequest(headers: RelayedRequestHeaders, body: Uint8Array, receivedAt: Date): Promise<void> {
    const parsed = parseRelaySignature(headers, receivedAt);
    if (!parsed.isParsed) throw new RelaySignatureInvalidError();
    const signal = AbortSignal.timeout(brokerExchangeTimeoutMs);
    let token: string;
    try { token = await this.#bearerToken(signal); }
    catch { throw new CredentialBrokerUnavailableError(); }
    if (matchesRelaySignature(parsed, token, body)) return;
    let renewed: string | undefined | typeof renewalDeferred;
    try { renewed = await this.#renewBearerTokenAfterMismatch(token, signal); }
    catch { throw new CredentialBrokerUnavailableError(); }
    // A rotated token cannot be read yet, so the request is retryable rather than refused.
    if (renewed === renewalDeferred) throw new RelayTokenRenewalDeferredError();
    if (renewed !== undefined && matchesRelaySignature(parsed, renewed, body)) return;
    throw new RelaySignatureInvalidError();
  }

  /** Resolve an absolute path against the origin of the broker endpoint. */
  originURL(path: string): string {
    if (!path.startsWith('/') || path.startsWith('//') || /[?#\\]/.test(path)) throw new TypeError('credential broker endpoint path must be an absolute path without a query or fragment');
    return new URL(path, new URL(this.#requestURL).origin).toString();
  }

  /** Post payload to target within the 40-second deadline and return the body of an HTTP 200 answer. */
  async #exchangeWithinDeadline(target: string, payload: string, signal?: AbortSignal): Promise<Uint8Array> {
    const timeoutSignal = AbortSignal.timeout(brokerExchangeTimeoutMs);
    const requestSignal = signal ? AbortSignal.any([signal, timeoutSignal]) : timeoutSignal;
    return new Promise<Uint8Array>((resolve, reject) => {
      const onAbort = () => reject(new CredentialBrokerUnavailableError());
      if (requestSignal.aborted) { onAbort(); return; }
      requestSignal.addEventListener('abort', onAbort, { once: true });
      void this.#exchange(target, payload, requestSignal).then(resolve, reject).finally(() => requestSignal.removeEventListener('abort', onAbort));
    });
  }

  /** Post with the scope token; after HTTP 401, renew the token once and ask again. */
  async #exchange(target: string, payload: string, requestSignal: AbortSignal): Promise<Uint8Array> {
    let token = await this.#bearerToken(requestSignal);
    let response = await this.post(target, token, payload, requestSignal);
    if (response.status === 401) {
      token = await this.#renewBearerToken(token, requestSignal);
      response = await this.post(target, token, payload, requestSignal);
    }
    if (response.status !== 200) {
      if ([408, 425, 429].includes(response.status) || response.status >= 500) throw new CredentialBrokerUnavailableError();
      throw new CredentialBrokerRejectedError();
    }
    try { return await readBoundedBody(responseChunks(response.body), 1 << 20); }
    catch (error: unknown) { throw error instanceof BodyTooLargeError || error instanceof CredentialBrokerRejectedError ? new CredentialBrokerRejectedError() : new CredentialBrokerUnavailableError(); }
  }

  async #bearerToken(signal: AbortSignal): Promise<string> {
    // A cached token needs no lock, so a slow relay mismatch reread cannot hold up reads; a 401 renewal clears it first.
    if (this.#token) return this.#token;
    return this.#withTokenLock(signal, async () => {
      if (this.#token) return this.#token;
      const token = await settleBeforeAbort(this.readToken(), signal);
      if (!isHeaderSafeBrokerToken(token)) throw new CredentialBrokerRejectedError();
      this.#token = token;
      return token;
    });
  }

  async #renewBearerToken(refusedToken: string, signal: AbortSignal): Promise<string> {
    return this.#withTokenLock(signal, async () => {
      if (this.#token && this.#token !== refusedToken) return this.#token;
      this.#token = undefined;
      const token = await settleBeforeAbort(this.readToken(true), signal);
      if (!isHeaderSafeBrokerToken(token) || token === refusedToken) throw new CredentialBrokerRejectedError();
      this.#token = token;
      return token;
    });
  }

  /**
   * Renew a token a relay signature did not match. A caller finds a token another caller already renewed or
   * waits for an in-flight renewal; otherwise at most one renewal starts every 30 seconds, and renewalDeferred
   * reports a mismatch inside that window. undefined means the renewal found no newer token.
   */
  async #renewBearerTokenAfterMismatch(mismatched: string, signal: AbortSignal): Promise<string | undefined | typeof renewalDeferred> {
    if (this.#token && this.#token !== mismatched) return this.#token;
    if (this.#mismatchRenewal) return settleBeforeAbort(this.#mismatchRenewal, signal);
    const now = this.nowMs();
    if (this.#lastMismatchRenewalMs !== undefined && now - this.#lastMismatchRenewalMs < relayTokenRenewalIntervalMs) return renewalDeferred;
    this.#lastMismatchRenewalMs = now;
    const renewal = this.#withTokenLock(signal, async () => {
      if (this.#token && this.#token !== mismatched) return this.#token;
      const token = await settleBeforeAbort(this.readToken(true), signal);
      if (!isHeaderSafeBrokerToken(token)) throw new CredentialBrokerRejectedError();
      this.#token = token;
      return token === mismatched ? undefined : token;
    });
    this.#mismatchRenewal = renewal;
    try { return await renewal; }
    finally { if (this.#mismatchRenewal === renewal) this.#mismatchRenewal = undefined; }
  }

  // Like Go's tokenMutex held under a context: an aborted waiter leaves the queue, and the holder's read is bounded.
  async #withTokenLock<T>(signal: AbortSignal, action: () => Promise<T>): Promise<T> {
    const previous = this.#tokenLockTail;
    let release!: () => void;
    const current = new Promise<void>(resolve => { release = resolve; });
    this.#tokenLockTail = previous.then(() => current);
    try { await settleBeforeAbort(previous, signal); }
    catch (error: unknown) { release(); throw error; }
    try { return await action(); }
    finally { release(); }
  }

  private async post(target: string, token: string, payload: string, signal?: AbortSignal): Promise<Response> {
    try {
      return await this.transport(target, {
        method: 'POST', redirect: 'manual', signal: signal ?? null,
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json' }, body: payload,
      });
    } catch { throw new CredentialBrokerUnavailableError(); }
  }
}

function settleBeforeAbort<T>(operation: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) return Promise.reject(new CredentialBrokerUnavailableError());
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(new CredentialBrokerUnavailableError());
    signal.addEventListener('abort', onAbort, { once: true });
    operation.then(value => { signal.removeEventListener('abort', onAbort); resolve(value); }, (error: unknown) => { signal.removeEventListener('abort', onAbort); reject(error); });
  });
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
