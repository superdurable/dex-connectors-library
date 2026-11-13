// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * Serves a provider webhook endpoint for connector Trigger sources.
 *
 * A connector supplies only what differs per provider: how to authenticate a request and how to decode
 * it into an event with a stable ID. The endpoint owns the rest of the push pipeline, in the order that
 * makes delivery durable:
 *
 *  1. accept only POST and read the body within maxBodyBytes;
 *  2. resolve the connection's credentials and call verifyRequest;
 *  3. call decodeEvent, acknowledging an event no binding handles;
 *  4. call prepareTriggerDelivery for every running source that accepts the event, so each binding's
 *     durable inbox records it;
 *  5. answer 200 only after every accepting source recorded the event, then deliver it to each source
 *     in arrival order with deliverTrigger. Verified events record and queue one at a time in the order
 *     they were verified, so the inboxes hold them in that order too.
 *
 * A provider therefore retries any event this process could not record: the endpoint answers 503 while
 * no source is running, while credentials are unavailable, and when recording fails. It answers 400 for a
 * request that fails verification or decoding, 405 for another method, and 413 for an oversized body.
 */

import { createHash } from 'node:crypto';
import type { IncomingHttpHeaders, IncomingMessage, ServerResponse } from 'node:http';
import { createTriggerCall, resolveCredential, type Call, type CredentialProvider, type CredentialRefreshDriver } from '../credentials.js';
import type { TriggerRunner, TriggerSource } from '../trigger.js';
import {
  TriggerDeliveryAbortedError, deliverTrigger, newTriggerContext, prepareTriggerDelivery,
  type TriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget,
} from '../trigger_delivery.js';
import type { CallID, ConnectionRef } from '../types.js';
import { CredentialBrokerUnavailableError } from '../projectconfig/broker.js';
import type { RelayedRequestHeaders } from '../relaysignature/index.js';

/** Default bound of each source's recorded events awaiting delivery. */
const defaultSourceQueueCapacity = 64;
/** Default bound of one arrival slot's recording and queueing. */
const defaultArrivalTimeoutMs = 10_000;

/** A verifyRequest failure the provider should retry, such as an unreadable signing secret; answered 503 instead of 400. */
export class WebhookVerificationUnavailableError extends Error {
  /** Wrap the reason, which never reaches the provider. */
  constructor(cause?: unknown) { super('webhook verification is temporarily unavailable', cause === undefined ? undefined : { cause }); this.name = 'WebhookVerificationUnavailableError'; }
}

/** One received webhook request after its body was read within the endpoint's limit. */
export interface WebhookRequest {
  /** Request URL path and query as received, for providers that sign it. */ url: string;
  /** Request headers with lower-case names. */ headers: IncomingHttpHeaders;
  /** Complete request body. */ body: Uint8Array;
  /** When the endpoint read the request, from the configured clock. */ receivedAt: Date;
}

/** A decoded webhook request: an event with a stable provider ID, or undefined for a valid event no binding handles. */
export type WebhookDecodeResult<Payload> = TriggerEvent<Payload> | undefined;

/** The webhook endpoint of one connection. */
export interface WebhookEndpointConfig<Credentials, Payload> {
  /** Manifest connector ID for the credential call made for each request. */ connectorId: string;
  /** Manifest Trigger name whose bindings this endpoint serves. */ triggerName: string;
  /** Connection whose credentials authenticate requests. */ connection: ConnectionRef;
  /** Resolves the connection's credentials for each request. */ credentials: CredentialProvider<Credentials>;
  /** Refreshes expiring credentials before verifyRequest; omitted resolves without refresh, which suits static signing secrets. */ credentialRefresh?: CredentialRefreshDriver<Credentials>;
  /** Largest accepted body in bytes; must be positive. */ maxBodyBytes: number;
  /** Authenticate a request; reject with WebhookVerificationUnavailableError for a retryable failure. Error text never reaches the provider. */
  verifyRequest(request: WebhookRequest, credentials: Credentials, signal: AbortSignal): Promise<void>;
  /** Decode a verified request into an event with a stable provider ID, or undefined for a valid event no binding handles. */
  decodeEvent(request: WebhookRequest): WebhookDecodeResult<Payload> | Promise<WebhookDecodeResult<Payload>>;
  /** Receipt clock; omitted uses the system clock. */ now?: () => Date;
  /** Bound of each source's recorded events awaiting delivery; omitted uses 64. A full queue answers 503. */ sourceQueueCapacity?: number;
  /** Longest a verified event may wait for and take in its arrival slot, from reservation, in milliseconds; omitted uses 10 seconds. A slower one answers 503. */ arrivalTimeoutMs?: number;
  /** Receives each source's delivery retry and skip records. */ logger?: TriggerLogger;
}

/** Serves one connection's webhook URL and fans each event out to its running sources. */
export class WebhookEndpoint<Credentials, Payload> {
  readonly #config: WebhookEndpointConfig<Credentials, Payload>;
  readonly #now: () => Date;
  readonly #queueCapacity: number;
  readonly #sources = new Map<WebhookSource<Credentials, Payload>, TriggerTarget<Payload>>();
  /** Settles when the last arrival slot finished; each verified event records and queues after the one before it. */
  #arrivalTail: Promise<void> = Promise.resolve();
  readonly #arrivalTimeoutMs: number;

  /** Validate config; the endpoint starts with no running sources. */
  constructor(config: WebhookEndpointConfig<Credentials, Payload>) {
    if (!config.connectorId?.trim() || !config.triggerName?.trim()) throw new TypeError('webhook endpoint connector ID and Trigger name are required');
    if (!config.connection?.name?.trim()) throw new TypeError('webhook endpoint connection name is required');
    if (!config.credentials || !config.decodeEvent || !config.verifyRequest) throw new TypeError('webhook endpoint credentials, decodeEvent, and verifyRequest are required');
    if (!Number.isSafeInteger(config.maxBodyBytes) || config.maxBodyBytes <= 0) throw new TypeError('webhook endpoint maxBodyBytes must be a positive integer');
    if (config.sourceQueueCapacity !== undefined && (!Number.isSafeInteger(config.sourceQueueCapacity) || config.sourceQueueCapacity <= 0)) throw new TypeError('webhook endpoint sourceQueueCapacity must be a positive integer');
    this.#config = config;
    this.#now = config.now ?? (() => new Date());
    this.#queueCapacity = config.sourceQueueCapacity ?? defaultSourceQueueCapacity;
    if (config.arrivalTimeoutMs !== undefined && (!Number.isSafeInteger(config.arrivalTimeoutMs) || config.arrivalTimeoutMs <= 0)) throw new TypeError('webhook endpoint arrivalTimeoutMs must be a positive integer');
    this.#arrivalTimeoutMs = config.arrivalTimeoutMs ?? defaultArrivalTimeoutMs;
  }

  /** Return a Trigger source that receives the endpoint's events acceptEvent accepts; omitted accepts all. It receives events only while running. */
  newSource(acceptEvent: (event: TriggerEvent<Payload>) => boolean = () => true): TriggerSource<Payload> {
    return new WebhookSource(this, acceptEvent, this.#queueCapacity, this.#config.logger);
  }

  /** How many sources are receiving events. While zero the endpoint answers 503 to every handled event. */
  runningSourceCount(): number { return this.#sources.size; }

  /** Verify, record, and acknowledge one webhook request; usable as a node:http request listener. */
  async serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> {
    const answer = (status: number, text?: string, headers: Record<string, string> = {}) => {
      response.writeHead(status, text ? { 'Content-Type': 'text/plain; charset=utf-8', ...headers } : headers);
      response.end(text ? `${text}\n` : undefined);
    };
    if (request.method !== 'POST') { request.resume(); answer(405, 'method not allowed', { Allow: 'POST' }); return; }
    let body: Uint8Array;
    try { body = await readLimitedBody(request, this.#config.maxBodyBytes); }
    catch (error: unknown) {
      if (error instanceof RequestBodyTooLargeError) { answer(413, 'request body too large', { Connection: 'close' }); return; }
      answer(400, 'invalid request body');
      return;
    }
    const abort = new AbortController();
    const onClose = () => { if (!response.writableFinished) abort.abort(); };
    response.once('close', onClose);
    try {
      const received: WebhookRequest = { url: request.url ?? '/', headers: request.headers, body, receivedAt: this.#now() };
      let credentials: Credentials;
      try { credentials = await this.#resolveCredentials(body, abort.signal); }
      catch { answer(503, 'webhook temporarily unavailable'); return; }
      try { await this.#config.verifyRequest(received, credentials, abort.signal); }
      catch (error: unknown) {
        if (error instanceof WebhookVerificationUnavailableError) { answer(503, 'webhook temporarily unavailable'); return; }
        answer(400, 'invalid webhook signature');
        return;
      }
      let event: WebhookDecodeResult<Payload>;
      try { event = await this.#config.decodeEvent(received); }
      catch { answer(400, 'invalid webhook event'); return; }
      if (!event) { answer(200); return; }
      // Only a verified, decoded event takes an arrival slot, so an unauthorized request never holds up valid ones.
      try { await this.#recordInArrivalOrder(event, abort.signal); }
      catch { answer(503, 'webhook delivery temporarily unavailable'); return; }
      answer(200);
    } finally { response.off('close', onClose); }
  }

  /** @internal Make a source receive events with its runner's target. */
  activate(source: WebhookSource<Credentials, Payload>, target: TriggerTarget<Payload>): void { this.#sources.set(source, target); }
  /** @internal Stop a source receiving events. */
  deactivate(source: WebhookSource<Credentials, Payload>): void { this.#sources.delete(source); }

  /** Record for every accepting source and return them; no running source fails for a retry. */
  async #record(context: TriggerContext, event: TriggerEvent<Payload>): Promise<Array<WebhookSource<Credentials, Payload>>> {
    const running = [...this.#sources];
    if (running.length === 0) throw new Error('no webhook Trigger source is running');
    const accepting: Array<WebhookSource<Credentials, Payload>> = [];
    for (const [source, target] of running) {
      if (!source.acceptsEvent(event)) continue;
      await prepareTriggerDelivery(context, target, event);
      accepting.push(source);
    }
    return accepting;
  }

  /**
   * Record and queue one event after every event that took an earlier slot, so the inboxes store events and
   * the sources receive them in arrival order. arrivalTimeoutMs runs from the slot's reservation: when it
   * expires the request fails with 503 for a provider retry, later slots proceed, a slot still waiting is
   * skipped, and a durable inbox refuses a write it has not started. A write already in flight that then
   * succeeds still queues its event, so a running source delivers it at least once, possibly after events
   * that arrived later.
   */
  #recordInArrivalOrder(event: TriggerEvent<Payload>, requestSignal: AbortSignal): Promise<void> {
    const slot = new AbortController();
    const expire = () => slot.abort();
    const timer = setTimeout(expire, this.#arrivalTimeoutMs);
    requestSignal.addEventListener('abort', expire, { once: true });
    const expired = new Promise<never>((_resolve, reject) => {
      const onAbort = () => reject(new TriggerDeliveryAbortedError());
      if (slot.signal.aborted) onAbort(); else slot.signal.addEventListener('abort', onAbort, { once: true });
    });
    // The race handles a late rejection; an unobserved rejection here would otherwise be reported.
    expired.catch(() => undefined);
    const record = async () => {
      const accepting = await this.#record(newTriggerContext(slot.signal), event);
      for (const source of accepting) source.enqueue(event);
    };
    const turn = this.#arrivalTail.then(() => slot.signal.aborted ? Promise.reject(new TriggerDeliveryAbortedError()) : Promise.race([record(), expired]));
    this.#arrivalTail = turn.catch(() => undefined);
    const answered = Promise.race([turn, expired]);
    void answered.then(() => undefined, () => undefined).finally(() => {
      clearTimeout(timer);
      requestSignal.removeEventListener('abort', expire);
    });
    return answered;
  }

  async #resolveCredentials(body: Uint8Array, signal: AbortSignal): Promise<Credentials> {
    const call = this.#credentialCall(body, signal);
    if (this.#config.credentialRefresh) return resolveCredential(this.#config.credentials, call, this.#config.credentialRefresh, signal);
    const provider = this.#config.credentials as CredentialProvider<Credentials> & { resolveContext?: (call: Call, signal: AbortSignal) => Promise<Credentials> };
    return provider.resolveContext ? provider.resolveContext(call, signal) : provider.resolve(call, signal);
  }

  /** Derive a stable call ID from the connection and body, so a redelivery resolves alike. */
  #credentialCall(body: Uint8Array, signal: AbortSignal): Call {
    const digest = createHash('sha256');
    for (const part of [this.#config.connectorId, this.#config.connection.provider, this.#config.connection.name]) digest.update(part).update(Buffer.from([0]));
    const identity = digest.update(body).digest().subarray(0, 16);
    identity[6] = (identity[6]! & 0x0f) | 0x50;
    identity[8] = (identity[8]! & 0x3f) | 0x80;
    const hex = identity.toString('hex');
    const id = `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}` as CallID;
    return createTriggerCall(this.#config.connection, { connectorId: this.#config.connectorId, operationId: this.#config.triggerName }, id, signal);
  }
}

/** One binding's source fed by a WebhookEndpoint. */
class WebhookSource<Credentials, Payload> implements TriggerSource<Payload> {
  readonly #queue: Array<TriggerEvent<Payload>> = [];
  #wake: (() => void) | undefined;
  #isRunning = false;

  constructor(private readonly endpoint: WebhookEndpoint<Credentials, Payload>, private readonly acceptEvent: (event: TriggerEvent<Payload>) => boolean, private readonly capacity: number, private readonly logger: TriggerLogger | undefined) {}

  acceptsEvent(event: TriggerEvent<Payload>): boolean { return this.acceptEvent(event); }

  enqueue(event: TriggerEvent<Payload>): void {
    if (this.#queue.length >= this.capacity) throw new Error('webhook Trigger delivery queue is full');
    this.#queue.push(event);
    this.#wake?.();
  }

  /** Receive the endpoint's events until the context ends, delivering each in order. */
  async run(context: TriggerContext, target: TriggerTarget<Payload>): Promise<void> {
    if (!target) throw new TypeError('webhook Trigger target is required');
    if (this.#isRunning) throw new Error('webhook Trigger source is already running');
    this.#isRunning = true;
    this.endpoint.activate(this, target);
    try {
      for (;;) {
        if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
        const event = this.#queue.shift();
        if (event) { await deliverTrigger(context, target, event, { ...(this.logger ? { logger: this.logger } : {}) }); continue; }
        await new Promise<void>(resolve => {
          const onAbort = () => resolve();
          this.#wake = () => { context.signal.removeEventListener('abort', onAbort); resolve(); };
          context.signal.addEventListener('abort', onAbort, { once: true });
        });
        this.#wake = undefined;
      }
    } finally {
      this.endpoint.deactivate(this);
      this.#isRunning = false;
    }
  }
}

/** A verifier of host-relayed requests, such as a LoadedProject. */
export interface RelayedRequestVerifier {
  /** Resolve for a request the host signed; reject with RelaySignatureInvalidError or CredentialBrokerUnavailableError. */
  verifyRelayedRequest(headers: RelayedRequestHeaders, body: Uint8Array, receivedAt: Date): Promise<void>;
}

/**
 * Return a verifyRequest for a WebhookEndpoint that serves requests a Dex host relays and signs with the
 * scope token. A bad or stale signature is answered 400, and an unreadable token 503 so the host retries.
 * The endpoint's credentials are then unused, so a static empty credential provider suits it.
 */
export function newRelayedRequestVerification<Credentials>(verifier: RelayedRequestVerifier): WebhookEndpointConfig<Credentials, unknown>['verifyRequest'] {
  if (!verifier) throw new TypeError('relayed request verifier is required');
  return async request => {
    try { await verifier.verifyRelayedRequest(request.headers, request.body, request.receivedAt); }
    catch (error: unknown) {
      if (error instanceof CredentialBrokerUnavailableError) throw new WebhookVerificationUnavailableError(error);
      throw error;
    }
  };
}

/** Serves one webhook endpoint while running the Trigger runners of its bindings. */
export class WebhookEndpointRunner {
  /** Bind a node:http request listener, usually a WebhookEndpoint's serveHTTP, and at least one runner its sources feed. */
  constructor(private readonly endpoint: { serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> }, private readonly runners: readonly TriggerRunner[]) {
    if (!endpoint || runners.length === 0 || runners.some(runner => !runner)) throw new TypeError('webhook endpoint runner requires a handler and at least one Trigger runner');
  }

  /** Receive the shared webhook endpoint. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.endpoint.serveHTTP(request, response); }

  /**
   * Run every Trigger runner until the context ends or one runner fails, then stop the others. It
   * rejects with TriggerDeliveryAbortedError after the context ends and otherwise with the first failure.
   */
  async run(context: TriggerContext): Promise<void> {
    const controller = new AbortController();
    const stop = () => controller.abort();
    context.signal.addEventListener('abort', stop, { once: true });
    const runContext: TriggerContext = { signal: controller.signal, logAttributes: context.logAttributes, recordSkip: () => context.recordSkip() };
    try {
      const results = this.runners.map(runner => runner.run(runContext));
      let firstFailure: unknown;
      await Promise.race(results.map(result => result.then(() => undefined, (error: unknown) => { firstFailure ??= error; })));
      controller.abort();
      await Promise.allSettled(results);
      if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
      if (firstFailure !== undefined && !(firstFailure instanceof TriggerDeliveryAbortedError)) throw firstFailure;
    } finally { context.signal.removeEventListener('abort', stop); }
  }
}

class RequestBodyTooLargeError extends Error {}

async function readLimitedBody(request: IncomingMessage, maxBytes: number): Promise<Uint8Array> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk as Uint8Array);
    size += buffer.length;
    if (size > maxBytes) { request.resume(); throw new RequestBodyTooLargeError(); }
    chunks.push(buffer);
  }
  return Buffer.concat(chunks);
}
