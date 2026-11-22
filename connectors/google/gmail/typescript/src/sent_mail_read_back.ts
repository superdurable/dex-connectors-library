// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import type { GmailHTTPClient } from './http.js';
import { DexMessageIDHeader } from './dex_message_id_header.js';
import type { Credentials } from './models.js';
import type { SentMessageIdentity } from './provider.js';

// Covers the Execute retry window of a send with a wide margin.
const SentMailCandidateWindow = 'newer_than:2d';
const SentMailCandidateLimit = 25;
// Admits only characters that Gmail search reads literally in a to: term.
const SearchableRecipientAddress = /^[A-Za-z0-9._+-]+@[A-Za-z0-9.-]+$/;
// The formatted mailbox ends with its addr-spec; a Q-encoded display name may not parse again.
const FormattedMailboxAddrSpec = /<([^<>]+)>$/;
const QuotedLocalPart = /^"((?:[^"\\]|\\.)*)"(@.*)$/;
// Go decodes a null document, list, entry, ID, or thread ID as empty, and rejects other types.
const SentMailCandidateListSchema = z.object({ messages: z.array(z.object({ id: z.string().nullish(), threadId: z.string().nullish() }).nullish()).nullish() }).nullable();

interface SentMailCandidatePart {
  mimeType?: string | null | undefined;
  headers?: ({ name?: string | null | undefined; value?: string | null | undefined } | null | undefined)[] | null | undefined;
  body?: { data?: string | null | undefined } | null | undefined;
  parts?: (SentMailCandidatePart | null | undefined)[] | null | undefined;
}

// Go's gmailMessageResource at every depth: null decodes as empty, any other mistyped field is unreadable.
const SentMailCandidatePartSchema: z.ZodType<SentMailCandidatePart> = z.lazy(() => z.object({
  mimeType: z.string().nullish(),
  headers: z.array(z.object({ name: z.string().nullish(), value: z.string().nullish() }).nullish()).nullish(),
  body: z.object({ data: z.string().nullish() }).nullish(), parts: z.array(SentMailCandidatePartSchema.nullish()).nullish(),
}));
const SentMailCandidateSchema = z.object({
  id: z.string().nullish(), threadId: z.string().nullish(), labelIds: z.array(z.string().nullish()).nullish(),
  snippet: z.string().nullish(), internalDate: z.string().nullish(), payload: SentMailCandidatePartSchema.nullish(),
});

// Gmail replaces Message-ID on send and cannot search custom headers, so candidates are read.
export class SentMailReadBack {
  readonly #http: GmailHTTPClient;
  readonly #call: Call;
  // The latest credential, which a refresh after a 401 replaces.
  #credentials: Credentials;
  readonly #searchQuery: string;
  readonly #dexMessageId: string;
  // A message's headers never change, so a candidate read once is never read again.
  readonly #mismatchedCandidateIds = new Set<string>();

  /** Bind one call's run-specific Message-ID address and its recipients, if known, to a read-back. */
  constructor(http: GmailHTTPClient, call: Call, credentials: Credentials, messageIdAddress: string, recipients: readonly string[]) {
    if (!http || !call || !credentials || !messageIdAddress) throw new TypeError('Gmail Sent Mail read-back dependencies are required');
    this.#http = http; this.#call = call; this.#credentials = credentials;
    this.#searchQuery = sentMailCandidateQuery(recipients);
    this.#dexMessageId = `<${messageIdAddress}>`;
  }

  /** The latest credential, which a refresh after a 401 during read-back replaces. */
  get credentials(): Credentials { return this.#credentials; }

  /** Resolve the match or undefined; reject when the search fails or an unread candidate leaves a miss. */
  async find(signal?: AbortSignal): Promise<SentMessageIdentity | undefined> {
    const candidates = await this.#listCandidates(signal);
    let candidateError: Error | undefined;
    for (const candidate of candidates) {
      if (!candidate?.id || this.#mismatchedCandidateIds.has(candidate.id)) continue;
      let sent: SentMessageIdentity | undefined;
      try { sent = await this.#readCandidate(candidate.id, signal); }
      catch (error) {
        candidateError = error instanceof Error ? error : new Error('Gmail sent-message header read failed');
        if (signal?.aborted) break;
        continue;
      }
      if (sent) return sent;
      this.#mismatchedCandidateIds.add(candidate.id);
    }
    if (candidateError) throw candidateError;
    return undefined;
  }

  async #listCandidates(signal?: AbortSignal): Promise<({ id?: string | null | undefined } | null | undefined)[]> {
    // Keys in Go's url.Values.Encode order.
    const query = new URLSearchParams({ includeSpamTrash: 'true', maxResults: String(SentMailCandidateLimit), q: this.#searchQuery });
    const { response, credentials } = await this.#http.request(this.#call, this.#credentials, 'GET', `/users/me/messages?${query.toString()}`, undefined, signal);
    this.#credentials = credentials;
    if (response.status !== 200) throw new Error('Gmail sent-message lookup did not return a usable response');
    let document: unknown;
    try { document = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { throw new Error('Gmail sent-message lookup returned an invalid response'); }
    const parsed = SentMailCandidateListSchema.safeParse(document);
    if (!parsed.success) throw new Error('Gmail sent-message lookup returned an invalid response');
    return parsed.data?.messages ?? [];
  }

  /** Match the preserved X-Dex-Message-ID header or, where a gateway keeps it, Message-ID itself. */
  async #readCandidate(messageId: string, signal?: AbortSignal): Promise<SentMessageIdentity | undefined> {
    const query = new URLSearchParams({ format: 'metadata' });
    for (const header of ['Message-ID', DexMessageIDHeader]) query.append('metadataHeaders', header);
    const { response, credentials } = await this.#http.request(this.#call, this.#credentials, 'GET', `/users/me/messages/${encodeURIComponent(messageId)}?${query.toString()}`, undefined, signal);
    this.#credentials = credentials;
    // The candidate was deleted after the search; it cannot be this call's copy any more.
    if (response.status === 404) return undefined;
    if (response.status !== 200) throw new Error('Gmail sent-message header read did not return a usable response');
    let resource;
    try { resource = SentMailCandidateSchema.parse(JSON.parse(Buffer.from(response.body).toString('utf8'))); }
    catch { throw new Error('Gmail sent-message header read returned an invalid response'); }
    if (resource.id !== messageId) throw new Error('Gmail sent-message header read returned an invalid response');
    const isMatch = (resource.payload?.headers ?? []).some(header =>
      ['message-id', DexMessageIDHeader.toLowerCase()].includes((header?.name ?? '').toLowerCase()) && (header?.value ?? '').trim() === this.#dexMessageId);
    return isMatch ? { id: messageId, threadId: resource.threadId ?? '' } : undefined;
  }
}

/** Narrow Sent Mail to recent messages, and to the first formatted recipient whose address is searchable. */
export function sentMailCandidateQuery(recipients: readonly string[]): string {
  const query = `in:sent ${SentMailCandidateWindow}`;
  const address = recipients[0] === undefined ? undefined : formattedMailboxAddress(recipients[0]);
  return address && SearchableRecipientAddress.test(address) ? `${query} to:${address}` : query;
}

// Read the addr-spec as Go's and Python's address parsers do, unquoting a quoted local part.
function formattedMailboxAddress(mailbox: string): string | undefined {
  const addrSpec = FormattedMailboxAddrSpec.exec(mailbox)?.[1];
  const quoted = addrSpec === undefined ? null : QuotedLocalPart.exec(addrSpec);
  return quoted ? `${quoted[1]!.replace(/\\(.)/g, '$1')}${quoted[2]!}` : addrSpec;
}
