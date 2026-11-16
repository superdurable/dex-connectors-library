// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import type { KeyedItem } from '@superdurable/connectors-sdk/pollingtrigger';
import { z } from 'zod';
import { ConversationSchema, type Conversation } from './generated/models.js';

/** Versions the stored conversationReceived cursor token; another version records a new baseline. */
export const ConversationReceivedCursorVersion = 1;

/** One conversation that newly appeared in the polled shared inbox. */
export interface ConversationReceivedEvent {
  /** The polled shared inbox ID. */ inboxId: string;
  /**
   * The conversation as the inbox's conversation list reported it when the poll found it: its status,
   * assignee, tag IDs, Gmail thread ID, and private permalink. Hiver's list carries no subject, requester,
   * message, or timestamp; read the conversation with getConversation for its message IDs.
   */
  conversation: Conversation;
}

/** Runtime validator for ConversationReceivedEvent, which also decodes Go-written inbox payloads. */
export const ConversationReceivedEventSchema = z.strictObject({ inboxId: z.string(), conversation: ConversationSchema }) satisfies z.ZodType<ConversationReceivedEvent>;

/** Selects the shared inbox a conversationReceived binding polls; Dex Web saves it from the inboxPicker unit. */
export interface ConversationReceivedTriggerConfiguration {
  /** The Hiver shared inbox ID, such as 105902, stored by the inboxPicker unit. Required. */ inboxId: string;
}

/** Runtime validator for a saved ConversationReceivedTriggerConfiguration; unknown fields are rejected. */
export const ConversationReceivedTriggerConfigurationSchema = z.strictObject({ inboxId: z.string().nullish().transform(value => value ?? '') }) satisfies z.ZodType<ConversationReceivedTriggerConfiguration, unknown>;

/** The JSON position stored in the poll cursor; the keyed snapshot beside it holds tracked conversation IDs. */
export interface ConversationReceivedCursorToken {
  /** The polled shared inbox; a cursor of another inbox records a new baseline. */ inboxId: string;
  /** False until the first full walk of the list has recorded every existing conversation. */ isBaselineComplete: boolean;
  /** Hiver's next_page token where the current sweep continues; empty starts a sweep. */ sweepPageToken: string;
  /** The lowest snapshot seen counter a conversation refreshed by the current sweep can have. */ sweepStartSeen: number;
  /** sweepStartSeen of the last completed sweep; IDs below it at the next sweep end are dropped. */ previousSweepStartSeen: number;
}

const pageTokenPattern = /^[A-Za-z0-9+/=_.-]+$/;
const integerSource = /^-?(?:0|[1-9][0-9]*)$/;
type CursorField = 'v' | 'inboxId' | 'baselineComplete' | 'sweepPageToken' | 'sweepStartSeen' | 'previousSweepStartSeen';
const cursorFieldNames: Readonly<Record<string, CursorField>> = {
  v: 'v', inboxid: 'inboxId', baselinecomplete: 'baselineComplete', sweeppagetoken: 'sweepPageToken', sweepstartseen: 'sweepStartSeen', previoussweepstartseen: 'previousSweepStartSeen',
};

/** Report whether value is a Hiver base64 next_page token of at most 4096 bytes. */
export function isPageToken(value: string): boolean {
  return Buffer.byteLength(value) <= 4096 && pageTokenPattern.test(value);
}

/** Return Go's json.Marshal text of the token, leaving out the omitempty fields when empty or zero. */
export function encodeConversationReceivedCursorToken(token: ConversationReceivedCursorToken): string {
  const fields = [`"v":${ConversationReceivedCursorVersion}`, `"inboxId":${encodeGoJSONValue(token.inboxId)}`, `"baselineComplete":${token.isBaselineComplete}`];
  if (token.sweepPageToken) fields.push(`"sweepPageToken":${encodeGoJSONValue(token.sweepPageToken)}`);
  if (token.sweepStartSeen) fields.push(`"sweepStartSeen":${token.sweepStartSeen}`);
  if (token.previousSweepStartSeen) fields.push(`"previousSweepStartSeen":${token.previousSweepStartSeen}`);
  return `{${fields.join(',')}}`;
}

/**
 * Decode a stored token for inboxId as Go's json.Decoder with DisallowUnknownFields does: field names match
 * case-insensitively, a later duplicate wins, null leaves a field zero, and integers have no fraction or
 * exponent. Throws for a token of another version or another inbox, or one with an invalid sweep position,
 * so the poll kit records a new baseline.
 */
export function decodeConversationReceivedCursorToken(encoded: string, inboxId: string): ConversationReceivedCursorToken {
  const notVersionOne = () => new TypeError('Hiver conversationReceived cursor is not a version 1 cursor');
  let document: unknown;
  try {
    document = JSON.parse(encoded, (_key, value: unknown, context?: { source?: string }) => {
      if (typeof value === 'number' && (context?.source === undefined || !integerSource.test(context.source) || !Number.isSafeInteger(value))) throw new TypeError('cursor integers have no fraction or exponent');
      return value;
    });
  } catch { throw notVersionOne(); }
  if (document !== null && (typeof document !== 'object' || Array.isArray(document))) throw notVersionOne();
  const fields: Record<CursorField, unknown> = { v: 0, inboxId: '', baselineComplete: false, sweepPageToken: '', sweepStartSeen: 0, previousSweepStartSeen: 0 };
  for (const [key, value] of Object.entries(document ?? {})) {
    const name = cursorFieldNames[key.toLowerCase()];
    if (name === undefined) throw notVersionOne();
    if (value === null) continue;
    const expected = name === 'inboxId' || name === 'sweepPageToken' ? 'string' : name === 'baselineComplete' ? 'boolean' : 'number';
    if (typeof value !== expected) throw notVersionOne();
    fields[name] = value;
  }
  if (fields.v !== ConversationReceivedCursorVersion) throw notVersionOne();
  const token: ConversationReceivedCursorToken = {
    inboxId: fields.inboxId as string, isBaselineComplete: fields.baselineComplete as boolean, sweepPageToken: fields.sweepPageToken as string,
    sweepStartSeen: fields.sweepStartSeen as number, previousSweepStartSeen: fields.previousSweepStartSeen as number,
  };
  if (token.inboxId !== inboxId) throw new TypeError('Hiver conversationReceived cursor belongs to another shared inbox');
  if ((token.sweepPageToken && !isPageToken(token.sweepPageToken)) || token.sweepStartSeen < 0 || token.previousSweepStartSeen < 0 || token.previousSweepStartSeen > token.sweepStartSeen) {
    throw new TypeError('Hiver conversationReceived cursor holds an invalid sweep position');
  }
  return token;
}

/** Tracks one conversation ID in the keyed snapshot; its hash ignores status, assignee, and tags. */
export class ConversationSnapshotItem implements KeyedItem {
  /** Bind the Hiver conversation ID. */
  constructor(readonly conversationId: string) {}
  /** The Hiver conversation ID. */ snapshotKey(): string { return this.conversationId; }
  /** The first 8 bytes of SHA-256 over the ID as hex; only membership matters. */
  snapshotHash(): string { return createHash('sha256').update(this.conversationId, 'utf8').digest('hex').slice(0, 16); }
  /** Store nothing beside the key. */ snapshotValue(): Uint8Array | undefined { return undefined; }
}

/** Return `conversation-<inboxId>-<conversationId>`, a pure function of the conversation. */
export function conversationReceivedEventId(inboxId: string, conversationId: string): string {
  return `conversation-${inboxId}-${conversationId}`;
}
