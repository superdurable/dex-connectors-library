// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call } from '@superdurable/connectors-sdk';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { z } from 'zod';
import type { Credentials } from './models.js';
import { GmailHTTPClient } from './http.js';
import { GmailMessageResourceSchema, type GmailMessageResource } from './message.js';

const SentMessageSchema = z.object({ id: z.string().optional(), threadId: z.string().optional() });
const SentSearchSchema = z.object({ messages: z.array(SentMessageSchema).nullish() });

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

  /** Search Sent Mail once for the run-specific RFC Message-ID. */
  async findSentMessage(call: Call, credentials: Credentials, messageIdAddress: string, signal?: AbortSignal): Promise<SentMessageIdentity | undefined> {
    const query = new URLSearchParams();
    query.set('includeSpamTrash', 'true');
    query.set('maxResults', '1');
    query.set('q', `in:sent rfc822msgid:${messageIdAddress}`);
    const { response } = await this.#http.request(call, credentials, 'GET', `/users/me/messages?${query.toString()}`, undefined, signal);
    if (response.status !== 200) throw new Error('Gmail sent-message lookup did not return a usable response');
    let document: unknown;
    try { document = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { throw new Error('Gmail sent-message lookup returned an invalid response'); }
    const parsed = SentSearchSchema.safeParse(document);
    if (!parsed.success) throw new Error('Gmail sent-message lookup returned an invalid response');
    const message = parsed.data.messages?.find(candidate => candidate.id);
    return message?.id ? { id: message.id, threadId: message.threadId ?? '' } : undefined;
  }

  /** Post one encoded Gmail messages.send request without redirects or transport replay. */
  async sendMessage(call: Call, credentials: Credentials, rawMessage: Uint8Array, threadId = '', signal?: AbortSignal): Promise<{ response: ProviderResponse; credentials: Credentials }> {
    const request = Buffer.from(JSON.stringify({ raw: Buffer.from(rawMessage).toString('base64url'), ...(threadId ? { threadId } : {}) }), 'utf8');
    return this.#http.request(call, credentials, 'POST', '/users/me/messages/send', request, signal);
  }
}

/** Parse one accepted Gmail send response. */
export function parseSentResponse(response: ProviderResponse): SentMessageIdentity | undefined {
  try {
    const document: unknown = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown;
    const parsed = SentMessageSchema.safeParse(document);
    return parsed.success && parsed.data.id ? { id: parsed.data.id, threadId: parsed.data.threadId ?? '' } : undefined;
  } catch { return undefined; }
}
