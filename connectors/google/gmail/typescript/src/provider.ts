// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call } from '@superdurable/connectors-sdk';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { z } from 'zod';
import type { Credentials } from './models.js';
import { GmailHTTPClient } from './http.js';
import { GmailMessageResourceSchema, type GmailMessageResource } from './message.js';
import { SentMailReadBack } from './sent_mail_read_back.js';

const MessageListSchema = z.object({ messages: z.array(z.object({ id: z.string().nullish(), threadId: z.string().nullish() })).nullish() });

/** One message reference in an inbox listing, newest first as Gmail lists it. */
export interface GmailMessageReference {
  /** Gmail message ID; empty when Gmail omitted it. */ id: string;
  /** Gmail thread ID; empty when Gmail omitted it. */ threadId: string;
}

/** Provider identity returned by a Gmail send or Sent Mail lookup. */
export interface SentMessageIdentity {
  /** Gmail message ID. */ id: string;
  /** Gmail thread ID, possibly empty in an accepted response. */ threadId: string;
}

/** One Gmail request result, including the response for precise classification. */
export interface GmailReadResult {
  /** Bounded HTTP response and status. */ response: ProviderResponse;
  /** Credential after an optional 401 refresh. */ credentials: Credentials;
  /** Parsed provider resource when HTTP and JSON are usable. */ resource?: GmailMessageResource;
}

/** Bounded provider API methods; classification belongs to connector operations. */
export class GmailProvider {
  readonly #http: GmailHTTPClient;

  /** Bind the shared authenticated transport. */
  constructor(http: GmailHTTPClient) {
    if (!http) throw new TypeError('Gmail HTTP client is required');
    this.#http = http;
  }

  /** Read one full or metadata message, returning the raw response on refusal or malformed JSON. */
  async readMessage(call: Call, credentials: Credentials, messageId: string, format: 'full' | 'metadata' | 'minimal', signal?: AbortSignal): Promise<GmailReadResult> {
    const query = new URLSearchParams({ format });
    if (format === 'metadata') {
      for (const header of ['Message-ID', 'In-Reply-To', 'References', 'From', 'Reply-To', 'To', 'Subject']) query.append('metadataHeaders', header);
    }
    const path = `/users/me/messages/${encodeURIComponent(messageId)}?${query.toString()}`;
    const result = await this.#http.request(call, credentials, 'GET', path, undefined, signal);
    const { response } = result;
    if (response.status < 200 || response.status >= 300) return result;
    try {
      const document: unknown = JSON.parse(Buffer.from(response.body).toString('utf8'));
      return { ...result, resource: GmailMessageResourceSchema.parse(document) };
    } catch { return result; }
  }

  /**
   * List the newest inbox page that matches searchQuery, as the Go Trigger poller does. It rejects with
   * an error that names no query value when Gmail does not answer 2xx with a usable listing.
   */
  async listInboxMessages(call: Call, credentials: Credentials, searchQuery: string, pageSize: number, signal?: AbortSignal): Promise<{ messages: GmailMessageReference[]; credentials: Credentials }> {
    // Keys in Go's url.Values.Encode order.
    const query = new URLSearchParams({ labelIds: 'INBOX', maxResults: String(pageSize) });
    if (searchQuery.trim()) query.set('q', searchQuery);
    const result = await this.#http.request(call, credentials, 'GET', `/users/me/messages?${query.toString()}`, undefined, signal);
    if (result.response.status < 200 || result.response.status >= 300) throw new Error('Gmail message list is unavailable');
    let parsed: z.infer<typeof MessageListSchema>;
    try { parsed = MessageListSchema.parse(JSON.parse(Buffer.from(result.response.body).toString('utf8'))); }
    catch { throw new Error('Gmail message list is invalid'); }
    return { messages: (parsed.messages ?? []).map(message => ({ id: message.id ?? '', threadId: message.threadId ?? '' })), credentials: result.credentials };
  }

  /**
   * Search recent Sent Mail once for the message whose X-Dex-Message-ID or Message-ID header carries the
   * run-specific Message-ID address, reading the headers of up to 25 candidates. Resolves undefined on a
   * clean miss; rejects when the search or a candidate read is inconclusive. Pass `recipients` to narrow
   * the search to the first recipient.
   */
  async findSentMessage(call: Call, credentials: Credentials, messageIdAddress: string, signal?: AbortSignal, recipients: readonly string[] = []): Promise<SentMessageIdentity | undefined> {
    return new SentMailReadBack(this.#http, call, credentials, messageIdAddress, recipients).find(signal);
  }

  /** Post one encoded Gmail messages.send request without redirects or transport replay. */
  async sendMessage(call: Call, credentials: Credentials, rawMessage: Uint8Array, threadId = '', signal?: AbortSignal): Promise<{ response: ProviderResponse; credentials: Credentials }> {
    const request = Buffer.from(JSON.stringify({ raw: Buffer.from(rawMessage).toString('base64url'), ...(threadId ? { threadId } : {}) }), 'utf8');
    return this.#http.request(call, credentials, 'POST', '/users/me/messages/send', request, signal);
  }
}

/**
 * Parse one Gmail send response: a JSON object whose `id` is a non-empty string names the message,
 * whatever its other fields are. A `threadId` that is not a string becomes empty and is read back later.
 */
export function parseSentResponse(response: ProviderResponse): SentMessageIdentity | undefined {
  try {
    const document: unknown = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown;
    if (!document || typeof document !== 'object' || Array.isArray(document)) return undefined;
    const { id, threadId } = document as { id?: unknown; threadId?: unknown };
    return typeof id === 'string' && id ? { id, threadId: typeof threadId === 'string' ? threadId : '' } : undefined;
  } catch { return undefined; }
}
