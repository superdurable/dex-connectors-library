// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { PostmarkClient } from './client.js';
import { GetMessageDetailsDefinition, ListOutboundMessagesDefinition } from './generated/definitions.js';
import type { GetMessageDetailsInput, ListOutboundMessagesInput, MessageDetails, MessagePage } from './generated/models.js';

const emptyPage: MessagePage = { TotalCount: 0, Messages: [] };
const emptyMessage: MessageDetails = { MessageID: '', Subject: '', Status: '' };

/** Read one bounded page of outbound Postmark messages. */
export class ListOutboundMessagesOperation implements Query<ListOutboundMessagesInput, MessagePage> {
  /** Bind an authenticated Postmark client. */ constructor(private readonly client: PostmarkClient) {}
  /** Return the generated query definition. */ definition() { return ListOutboundMessagesDefinition; }
  /** Validate the search window and invoke one provider GET. */
  async invoke(call: Call, input: ListOutboundMessagesInput): Promise<QueryAttempt<MessagePage>> {
    const count = input.count === 0 ? 50 : input.count;
    if (!Number.isInteger(count) || !Number.isInteger(input.offset) || count < 1 || count > 500 || input.offset < 0 || count + input.offset > 10_000) {
      return newQueryBranch('defect', emptyPage, {}, { kind: 'VALIDATION', provider: 'postmark', operation: 'listOutboundMessages', message: "count and offset must fit Postmark's 10,000-message search window" });
    }
    const received = await this.client.request(call, `/messages/outbound?count=${count}&offset=${input.offset}`);
    return this.client.classify('listOutboundMessages', received, emptyPage, 'listed', '', decodePage);
  }
}

/** Read details for one outbound Postmark message. */
export class GetMessageDetailsOperation implements Query<GetMessageDetailsInput, MessageDetails> {
  /** Bind an authenticated Postmark client. */ constructor(private readonly client: PostmarkClient) {}
  /** Return the generated query definition. */ definition() { return GetMessageDetailsDefinition; }
  /** Validate the ID and invoke one provider GET. */
  async invoke(call: Call, input: GetMessageDetailsInput): Promise<QueryAttempt<MessageDetails>> {
    if (!input.messageId) return newQueryBranch('defect', emptyMessage, {}, { kind: 'VALIDATION', provider: 'postmark', operation: 'getMessageDetails', message: 'message ID is required' });
    const received = await this.client.request(call, `/messages/outbound/${goPathEscape(input.messageId)}/details`);
    return this.client.classify('getMessageDetails', received, emptyMessage, 'found', 'notFound', decodeMessage);
  }
}

function goPathEscape(value: string): string {
  return encodeURIComponent(value)
    .replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%(24|26|2B|3A|3D|40)/gi, encoded => String.fromCharCode(Number.parseInt(encoded.slice(1), 16)));
}

function decodeObject(body: Uint8Array): Record<string, unknown> {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!document || typeof document !== 'object' || Array.isArray(document)) throw new TypeError('Postmark response must be an object');
  return document as Record<string, unknown>;
}

function goString(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('Postmark string field has a wrong type');
  return value;
}

function goInteger(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Postmark integer field has a wrong type');
  return value;
}

function decodeMessageValue(document: Record<string, unknown>): MessageDetails {
  return { MessageID: goString(document.MessageID), Subject: goString(document.Subject), Status: goString(document.Status) };
}

function decodeMessage(body: Uint8Array): MessageDetails {
  const message = decodeMessageValue(decodeObject(body));
  if (!message.MessageID) throw new TypeError('Postmark message ID is missing');
  return message;
}

function decodePage(body: Uint8Array): MessagePage {
  const document = decodeObject(body);
  if (!Array.isArray(document.Messages)) throw new TypeError('Postmark messages are missing');
  return { TotalCount: goInteger(document.TotalCount), Messages: document.Messages.map(value => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Postmark message is invalid');
    return decodeMessageValue(value as Record<string, unknown>);
  }) };
}
