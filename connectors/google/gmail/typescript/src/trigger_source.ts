// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import {
  TriggerDeliveryAbortedError, createTriggerCall, isTriggerUndeliverable, logTriggerRecord, prepareTriggerDelivery, throwIfTriggerAborted, triggerAttempt, triggerErrorAttributes,
  waitForTriggerDelay, withTriggerLogAttributes, withTriggerLoggerAttributes,
  type Call, type CallID, type ConnectionRef, type TriggerContext, type TriggerEvent, type TriggerLogAttributes, type TriggerLogLevel, type TriggerLogger, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import type { GmailClient } from './client.js';
import { decodeGmailMessage, parseMailbox } from './message.js';
import { ConnectorID, type Credentials, type Message } from './models.js';
import type { GmailMessageReference } from './provider.js';
import type { MessageEvent, MessageMatcher } from './trigger.js';

/** One poller's source and the target it delivers to. */
export interface MessageTriggerRoute { source: MessagePollingTriggerSource; target: TriggerTarget<MessageEvent> }

/** One inbox list call, delivered later by MessagePollingTriggerSource.deliver. */
export interface MessageListing { credentials: Credentials; messages: GmailMessageReference[] }

/** An event that a target failed to consume, with the number of failed attempts. */
interface PendingMessageRetry { event: TriggerEvent<MessageEvent>; failures: number }

/** A poll that ended early; its message is safe to log and names no query, sender, or token. */
class GmailPollError extends Error {
  constructor(message: string, cause?: unknown) { super(message, cause === undefined ? undefined : { cause }); this.name = 'GmailPollError'; }
}

const uuidNamespaceURL = Buffer.from('6ba7b8119dad11d180b400c04fd430c8', 'hex');

/**
 * Polls one Gmail binding: it lists the newest inbox page, reads each new message's metadata, and
 * delivers matching roots or replies oldest first, as the Go connector's poller does. The delivered set
 * covers the last listed page in memory; a restart rescans the page, and the Flow start request ID and
 * the application's RPC absorb the duplicates.
 */
export class MessagePollingTriggerSource {
  readonly #client: GmailClient;
  readonly #connection: ConnectionRef;
  readonly #searchQuery: string;
  readonly #matcher: MessageMatcher;
  readonly #requiresReply: boolean;
  readonly #logger: TriggerLogger;
  readonly #triggerName: string;
  #delivered = new Set<string>();
  // Every later poll delivers this event before any listed message, until the target consumes it.
  #retry: PendingMessageRetry | undefined;
  #pollFailures = 0;

  /** Bind one binding's query and matcher; an empty bindingName omits the binding log attribute. */
  constructor(client: GmailClient, connection: ConnectionRef, triggerName: string, bindingName: string, searchQuery: string, matcher: MessageMatcher, requiresReply: boolean) {
    this.#client = client;
    this.#connection = connection;
    this.#triggerName = triggerName;
    this.#searchQuery = searchQuery;
    this.#matcher = matcher;
    this.#requiresReply = requiresReply;
    this.#logger = withTriggerLoggerAttributes(client.triggerLogger, { connector: ConnectorID, connection: connection.name, trigger: triggerName, ...(bindingName ? { binding: bindingName } : {}) });
  }

  /** Scan, then wait one poll interval, until the context ends. */
  async run(context: TriggerContext, target: TriggerTarget<MessageEvent>): Promise<void> {
    await runMessageTriggerRoutes(context, () => this.scan(context, target), this.#pollIntervalMs());
  }

  /** List the inbox and deliver the listed messages. */
  async scan(context: TriggerContext, target: TriggerTarget<MessageEvent>): Promise<void> {
    await this.deliver(context, target, await this.list(context));
  }

  /** Read the newest inbox page that matches the source's search query. */
  async list(context: TriggerContext): Promise<MessageListing> {
    const { http, provider } = this.#client.triggerTransport();
    const call = this.#credentialCall(context.signal);
    let credentials: Credentials;
    try { credentials = await http.resolveCredentials(call, context.signal); }
    catch (error) { throw this.#pollFailed(context, new GmailPollError('Gmail Trigger credentials are unavailable', error)); }
    try {
      const listed = await provider.listInboxMessages(call, credentials, this.#searchQuery, http.settings().pollPageSize, context.signal);
      return { credentials: listed.credentials, messages: listed.messages };
    } catch (error) { throw this.#pollFailed(context, new GmailPollError('Gmail message list is unavailable', error)); }
  }

  /**
   * Hand target the event that failed on an earlier poll, if any, then every new matching message in
   * listing, oldest first. It stops at the first failure, so the next poll retries that event before any
   * later message, even after newer mail pushes it off the listed page.
   */
  async deliver(context: TriggerContext, target: TriggerTarget<MessageEvent>, listing: MessageListing): Promise<void> {
    if (this.#retry) await this.#handle(context, target, this.#retry.event);
    const { provider } = this.#client.triggerTransport();
    let credentials = listing.credentials;
    const current = new Set<string>();
    for (let index = listing.messages.length - 1; index >= 0; index--) {
      const reference = listing.messages[index]!;
      if (!reference.id) continue;
      current.add(reference.id);
      if (this.#delivered.has(reference.id)) continue;
      const messageAttributes = { thread_id: reference.threadId, event_id: reference.id };
      let message: Message;
      try {
        const read = await provider.readMessage(this.#credentialCall(context.signal), credentials, reference.id, 'metadata', context.signal);
        credentials = read.credentials;
        if (!read.resource) throw new GmailPollError('Gmail message lookup failed');
        message = decodeGmailMessage(read.resource);
      } catch (error) {
        throw this.#pollFailed(context, error instanceof GmailPollError ? error : new GmailPollError('Gmail message lookup failed', error), messageAttributes);
      }
      const isReply = (message.inReplyTo ?? '').trim() !== '' || (message.references ?? '').trim() !== '';
      const reason = this.#ignoreReason(isReply, message);
      if (reason) { this.#log(context, 'debug', 'trigger event ignored', { ...messageAttributes, reason }); continue; }
      const event: TriggerEvent<MessageEvent> = {
        id: message.messageId, occurredAt: new Date(message.receivedAt),
        payload: {
          primaryEmail: credentials.primaryEmail, messageId: message.messageId, threadId: message.threadId, from: message.from, subject: message.subject,
          ...(message.snippet ? { snippet: message.snippet } : {}), receivedAt: message.receivedAt, isReply,
        },
      };
      try { await prepareTriggerDelivery(context, target, event); }
      catch (error) { throw this.#pollFailed(context, new GmailPollError('Gmail Trigger delivery could not be recorded', error), messageAttributes); }
      // An undeliverable event is consumed; any other failure ends this scan so the next poll retries it.
      await this.#handle(context, target, event);
    }
    this.#delivered = current;
    this.#pollFailures = 0;
  }

  #pollIntervalMs(): number { return this.#client.triggerTransport().http.settings().pollIntervalMs; }

  /**
   * Make one delivery attempt and log its outcome. It resolves when target consumes the event, and
   * otherwise keeps the event as the source's retry and rethrows. A durable inbox or a newTrigger runner
   * logs the undeliverable events it consumes itself, so the source then logs no recovery.
   */
  async #handle(context: TriggerContext, target: TriggerTarget<MessageEvent>, event: TriggerEvent<MessageEvent>): Promise<void> {
    const failures = this.#retry?.event.id === event.id ? this.#retry.failures : 0;
    const threadAttributes = { thread_id: event.payload.threadId };
    const messageAttributes = { ...threadAttributes, event_id: event.id };
    // The thread ID also reaches the records of the inbox and the Dex targets inside the attempt.
    const attempt = triggerAttempt(withTriggerLogAttributes(context, threadAttributes));
    try {
      await target.handleTrigger(attempt.context, event);
      if (failures > 0 && !attempt.wasSkipped()) this.#log(context, 'info', 'trigger delivered after retry', { ...messageAttributes, attempts: failures + 1 });
    } catch (error) {
      if (isTriggerUndeliverable(error)) {
        this.#log(context, 'warn', 'trigger event skipped: undeliverable', { ...messageAttributes, ...triggerErrorAttributes(error) });
      } else if (context.signal.aborted) {
        throw error;
      } else {
        // Gmail answered, so the poll itself did not fail.
        this.#pollFailures = 0;
        this.#retry = { event, failures: failures + 1 };
        this.#log(context, 'warn', 'trigger delivery failed; retrying', { ...messageAttributes, attempt: failures + 1, delay_ms: this.#pollIntervalMs(), ...triggerErrorAttributes(error) });
        throw error;
      }
    }
    this.#retry = undefined;
    // A consumed retry that Gmail still lists must not be delivered again from the listing.
    this.#delivered.add(event.id);
  }

  /** Explain why the source does not deliver a listed message, or return '' when it matches. */
  #ignoreReason(isReply: boolean, message: Message): string {
    if (this.#requiresReply && !isReply) return 'not_a_reply';
    if (!this.#requiresReply && isReply) return 'not_a_root';
    if (!messageMatcherMatches(this.#matcher, message)) return 'matcher_mismatch';
    return '';
  }

  /** Log a poll that ended early and return error; the next poll, one interval later, retries. */
  #pollFailed(context: TriggerContext, error: GmailPollError, attributes: TriggerLogAttributes = {}): Error {
    if (context.signal.aborted) return new TriggerDeliveryAbortedError();
    this.#pollFailures++;
    const causeAttributes: TriggerLogAttributes = error.cause === undefined ? {} : { error_cause_type: triggerErrorAttributes(error.cause).error_type ?? 'Error' };
    this.#log(context, 'warn', 'gmail poll failed; retrying', { ...attributes, attempt: this.#pollFailures, delay_ms: this.#pollIntervalMs(), error: error.message, ...causeAttributes });
    return error;
  }

  #log(context: TriggerContext, level: TriggerLogLevel, message: string, attributes: TriggerLogAttributes): void {
    logTriggerRecord(this.#logger, context, level, message, attributes);
  }

  /** The Go connector's stable credential call: a version 5 UUID of the connection and Trigger. */
  #credentialCall(signal: AbortSignal): Call {
    const identity = ['https://superdurable.dev/dex-connectors/trigger-credential/v1', this.#connection.provider, this.#connection.name, this.#triggerName].join('/');
    return createTriggerCall(this.#connection, { connectorId: ConnectorID, operationId: this.#triggerName }, nameBasedUUID(identity) as CallID, signal);
  }
}

/** Report whether message passes matcher: subject or snippet text, then the sender allowlist. */
export function messageMatcherMatches(matcher: MessageMatcher, message: Pick<Message, 'subject' | 'snippet' | 'from'>): boolean {
  if (matcher.messageContains) {
    const haystack = `${message.subject}\n${message.snippet ?? ''}`.toLowerCase();
    if (!haystack.includes(matcher.messageContains.toLowerCase())) return false;
  }
  const senders = matcher.senderEmails ?? [];
  if (senders.length === 0) return true;
  const sender = parseMailbox(message.from)?.address.toLowerCase();
  if (!sender) return false;
  return senders.some(allowed => parseMailbox(allowed)?.address.toLowerCase() === sender);
}

/** Run poll, then wait intervalMs, until the context ends; a failed poll is retried after the wait. */
export async function runMessageTriggerRoutes(context: TriggerContext, poll: () => Promise<void>, intervalMs: number): Promise<void> {
  for (;;) {
    throwIfTriggerAborted(context.signal);
    // A failed poll already logged its failure; the next one retries it.
    try { await poll(); } catch { throwIfTriggerAborted(context.signal); }
    await waitForTriggerDelay(context.signal, intervalMs);
  }
}

/**
 * Make one ordered pass over every route. Replies are listed before roots: a listed reply arrived after
 * its root, so the root listing that follows contains that root too, unless an earlier poll delivered it.
 */
export async function pollMessageTriggerRoutes(context: TriggerContext, rootRoutes: readonly MessageTriggerRoute[], replyRoutes: readonly MessageTriggerRoute[]): Promise<void> {
  const replyListings: MessageListing[] = [];
  for (const route of replyRoutes) replyListings.push(await route.source.list(context));
  for (const route of rootRoutes) await route.source.scan(context, route.target);
  for (const [index, route] of replyRoutes.entries()) await route.source.deliver(context, route.target, replyListings[index]!);
}

/** Deliver every route's pending durable inbox events in route order. */
export async function replayMessageTriggerRoutes(context: TriggerContext, routes: readonly MessageTriggerRoute[]): Promise<void> {
  for (const route of routes) {
    if (!route.target.replayTriggerDeliveries) continue;
    try { await route.target.replayTriggerDeliveries(context); }
    catch (error) {
      if (error instanceof TriggerDeliveryAbortedError) throw error;
      throw new Error(`replay Gmail Trigger deliveries: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
  }
}

/** RFC 4122 version 5 UUID of name in the URL namespace, as Go's uuid.NewSHA1(uuid.NameSpaceURL, name). */
export function nameBasedUUID(name: string): string {
  const bytes = createHash('sha1').update(uuidNamespaceURL).update(name, 'utf8').digest().subarray(0, 16);
  bytes[6] = (bytes[6]! & 0x0f) | 0x50;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = bytes.toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
