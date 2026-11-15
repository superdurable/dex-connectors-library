// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { z } from 'zod';
import { ConversationSchema, type Conversation } from './generated/models.js';
import { channelPattern } from './operations.js';

/**
 * How far before the newest creation time a poll still accepts a conversation it has not seen, because
 * Re:amaze lists a conversation some time after creating it.
 */
export const ConversationCreatedLateIndexWindowMs = 600_000;
/** Bounds the GET /conversations pages one poll reads, 480 conversations, below the kit's page bound. */
export const MaxConversationCreatedPagesPerPoll = 16;

/** The conversationCreated binding configuration that the channelSlugEntry unit saves in Dex Web. */
export interface ConversationCreatedTriggerConfiguration {
  /** The watched channel slug, Re:amaze's category, such as support; blank or omitted watches every channel of the brand. */ channel?: string | undefined;
}
/** Runtime validator for ConversationCreatedTriggerConfiguration: blank, or a Re:amaze channel slug. */
export const ConversationCreatedTriggerConfigurationSchema = z.strictObject({
  channel: z.string().refine(channel => channel === '' || channelPattern.test(channel), 'channel must be a Re:amaze channel slug of letters, digits, hyphens, and underscores, such as support').optional(),
}) satisfies z.ZodType<ConversationCreatedTriggerConfiguration>;

/** One conversation created in the watched brand or channel, as Go's ConversationCreatedEvent encodes it. */
export interface ConversationCreatedEvent {
  /** The connection's Re:amaze brand subdomain, such as acme for https://acme.reamaze.io. */ brand: string;
  /** The watched channel slug, omitted when the binding watches every channel. */ channel?: string | undefined;
  /** The list entry Re:amaze returned when the poll first saw it, with its first message cut at MaxTextBytes; filter on status. */ conversation: Conversation;
}
/** Runtime validator for ConversationCreatedEvent. */
export const ConversationCreatedEventSchema = z.strictObject({
  brand: z.string(), channel: z.string().optional(), conversation: ConversationSchema,
}) satisfies z.ZodType<ConversationCreatedEvent>;

/** A slug digest of a seen conversation, which bounds the entry size for any slug length. */
export interface SeenConversation { digest: string; createdAtMs: number }
/** The committed cursor: the newest creation time, a floor below which nothing arrives, and the seen set. */
export interface ConversationCreatedCursor { brand: string; channel: string; watermarkMs: number; floorMs: number; seen: SeenConversation[] }
/** The newest end of the listing one poll read. */
export interface ConversationWalk { conversations: Conversation[]; reachesFloor: boolean; pagesRead: number }

/** Thrown for a token that Go's json.Unmarshal would refuse into the cursor struct. */
export class ConversationCreatedCursorInvalidError extends Error {
  constructor() { super('Re:amaze conversationCreated cursor is not decodable'); this.name = 'ConversationCreatedCursorInvalidError'; }
}

/** The Trigger event ID: a hash of the brand and slug, stable across refetches for any slug length. */
export function conversationCreatedEventId(brand: string, slug: string): string {
  return 'reamaze-conversation-' + createHash('sha256').update(`${brand}\u0000${slug}`).digest('hex').slice(0, 32);
}
/** The slug digest that the cursor's seen set stores. */
export function conversationDigest(slug: string): string { return createHash('sha256').update(slug).digest('hex').slice(0, 16); }

/** Nanoseconds since the Unix epoch of a canonical UTC timestamp, such as 2026-03-02T09:00:00.123Z. */
export function instantNanoseconds(timestamp: string): bigint {
  const [whole = '', fraction = ''] = timestamp.replace(/Z$/, '').split('.');
  return BigInt(Date.parse(`${whole}Z`)) * 1_000_000n + BigInt(fraction.padEnd(9, '0').slice(0, 9) || '0');
}
/** Unix milliseconds rounded down, as Go's Time.UnixMilli. */
export function unixMilliseconds(timestamp: string): number {
  const nanoseconds = instantNanoseconds(timestamp);
  return Number(nanoseconds / 1_000_000n - (nanoseconds % 1_000_000n < 0n ? 1n : 0n));
}

/** Encode a string as Go's json.Marshal does: HTML-safe, with U+2028 and U+2029 escaped. */
function goString(value: string): string {
  return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Encode the cursor as Go's json.Marshal does: struct field order, channel omitempty, HTML-safe strings. */
export function encodeConversationCreatedCursor(position: ConversationCreatedCursor): string {
  const members = [`"brand":${goString(position.brand)}`];
  if (position.channel) members.push(`"channel":${goString(position.channel)}`);
  members.push(`"watermark":${position.watermarkMs}`, `"floor":${position.floorMs}`);
  members.push(`"seen":[${position.seen.map(entry => `{"d":${goString(entry.digest)},"t":${entry.createdAtMs}}`).join(',')}]`);
  return `{${members.join(',')}}`;
}

const int64Minimum = -(2n ** 63n), int64Maximum = 2n ** 63n - 1n;
/** A JSON number with its source text, distinct from any JSON object. */
class SourcedNumber { constructor(readonly source: string) {} }

/**
 * Decode the token as Go's encoding/json fills the cursor struct: keys match case-insensitively, unknown
 * keys are ignored, a null leaves a scalar unchanged and clears the seen list, and a value of the wrong JSON
 * type, a fraction, an exponent, or a number outside int64 rejects the token.
 */
export function decodeConversationCreatedCursor(token: string): ConversationCreatedCursor {
  let document: unknown;
  try {
    document = JSON.parse(token, (_key: string, value: unknown, context?: { source: string }) => typeof value === 'number' ? new SourcedNumber(context?.source ?? '') : value);
  } catch { throw new ConversationCreatedCursorInvalidError(); }
  const position: ConversationCreatedCursor = { brand: '', channel: '', watermarkMs: 0, floorMs: 0, seen: [] };
  if (document === null) return position;
  if (!isObject(document)) throw new ConversationCreatedCursorInvalidError();
  for (const [name, value] of Object.entries(document)) {
    const folded = name.toLowerCase();
    if (folded === 'seen') {
      if (value === null) position.seen = [];
      else if (Array.isArray(value)) position.seen = value.map(decodeSeenConversation);
      else throw new ConversationCreatedCursorInvalidError();
    } else if (value === null) continue;
    else if (folded === 'brand') position.brand = goStringValue(value);
    else if (folded === 'channel') position.channel = goStringValue(value);
    else if (folded === 'watermark') position.watermarkMs = goInt64(value);
    else if (folded === 'floor') position.floorMs = goInt64(value);
  }
  return position;
}

function decodeSeenConversation(value: unknown): SeenConversation {
  // Go leaves a null array element as the zero entry.
  if (value === null) return { digest: '', createdAtMs: 0 };
  if (!isObject(value)) throw new ConversationCreatedCursorInvalidError();
  const entry: SeenConversation = { digest: '', createdAtMs: 0 };
  for (const [name, member] of Object.entries(value)) {
    if (member === null) continue;
    if (name.toLowerCase() === 'd') entry.digest = goStringValue(member);
    else if (name.toLowerCase() === 't') entry.createdAtMs = goInt64(member);
  }
  return entry;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) && !(value instanceof SourcedNumber);
}
function goStringValue(value: unknown): string { if (typeof value !== 'string') throw new ConversationCreatedCursorInvalidError(); return value; }
function goInt64(value: unknown): number {
  if (!(value instanceof SourcedNumber) || !/^-?(0|[1-9]\d*)$/.test(value.source)) throw new ConversationCreatedCursorInvalidError();
  const exact = BigInt(value.source);
  if (exact < int64Minimum || exact > int64Maximum) throw new ConversationCreatedCursorInvalidError();
  return Number(exact);
}
