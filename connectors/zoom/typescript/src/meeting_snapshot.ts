// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { KeyedSnapshotEntry } from '@superdurable/connectors-sdk/projectconfig';
import type { KeyedItem } from '@superdurable/connectors-sdk/pollingtrigger';
import { z } from 'zod';
import { MeetingSummarySchema, type MeetingSummary } from './generated/models.js';

/** A meeting the previous poll did not list, such as a newly scheduled meeting. */
export const MeetingChangeScheduled = 'scheduled';
/**
 * A listed meeting whose topic, type, start, duration, time zone, or listed agenda changed, such as a
 * reschedule. A recurring meeting whose listed start moves to its next occurrence also arrives as updated.
 */
export const MeetingChangeUpdated = 'updated';
/** A meeting the list no longer contains: it was deleted, or it expired after its scheduled time. */
export const MeetingChangeRemoved = 'removed';

/**
 * The kind of one meetingChanged delivery. Zoom's meeting list reports no change type or modification time,
 * so the kind comes from comparing two polls of the list.
 */
export const MeetingChangeKindSchema = z.enum([MeetingChangeScheduled, MeetingChangeUpdated, MeetingChangeRemoved]);
/** scheduled, updated, or removed. */
export type MeetingChangeKind = z.infer<typeof MeetingChangeKindSchema>;

/**
 * The meetingChanged binding configuration. The binding always watches the authorized user's own scheduled
 * meetings, so Dex Web shows no field for it; an application may narrow the delivered kinds in code.
 */
export const MeetingChangedTriggerConfigurationSchema = z.strictObject({
  /**
   * Limits deliveries to these kinds; empty delivers scheduled, updated, and removed. Skipped kinds still
   * advance the stored snapshot, so they are never delivered later.
   */
  changeKinds: z.array(z.string()).nullable().optional(),
}).superRefine((configuration, context) => {
  const seen = new Set<string>();
  for (const kind of configuration.changeKinds ?? []) {
    if (!MeetingChangeKindSchema.safeParse(kind).success) context.addIssue({ code: 'custom', message: `meetingChanged changeKinds entry ${JSON.stringify(kind)} must be scheduled, updated, or removed` });
    else if (seen.has(kind)) context.addIssue({ code: 'custom', message: `meetingChanged changeKinds repeats ${JSON.stringify(kind)}` });
    seen.add(kind);
  }
});
/** The meetingChanged binding configuration; the empty object delivers every kind. */
export type MeetingChangedTriggerConfiguration = z.infer<typeof MeetingChangedTriggerConfigurationSchema>;

/**
 * One meeting scheduled, updated, or removed between two polls of the authorized user's scheduled meetings;
 * field names match the Go connector, so Go and TypeScript replicas share a binding's inbox.
 */
export const MeetingChangedEventSchema = z.object({
  /** The derived change kind. */ changeKind: MeetingChangeKindSchema,
  /**
   * The meeting as this poll listed it. For a removed meeting it is the last listed state, without uuid or
   * joinUrl, which the stored snapshot never keeps.
   */
  meeting: MeetingSummarySchema,
  /** The state the previous poll listed, without uuid or joinUrl; set only for updated. */ previousMeeting: MeetingSummarySchema.optional(),
});
/** One meetingChanged delivery. */
export type MeetingChangedEvent = z.infer<typeof MeetingChangedEventSchema>;

/** Report whether configuration delivers kind. */
export function deliversMeetingChange(configuration: MeetingChangedTriggerConfiguration, kind: string): boolean {
  const kinds = configuration.changeKinds ?? [];
  return kinds.length === 0 || kinds.includes(kind);
}

/** Encode a string or number as Go's encoding/json does: lone surrogates become U+FFFD and HTML characters are escaped. */
export function goJSON(value: string | number): string {
  const text = typeof value === 'string' ? JSON.stringify(wellFormed(value)) : JSON.stringify(value);
  return text.replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Encode the tracked fields as Go marshals trackedMeeting; it never keeps the UUID or join URL. */
function trackedMeetingJSON(summary: MeetingSummary): Uint8Array {
  const fields: Array<[string, string | number]> = [['id', summary.id]];
  if (summary.hostId) fields.push(['hostId', summary.hostId]);
  fields.push(['topic', summary.topic]);
  if (summary.type) fields.push(['type', summary.type]);
  if (summary.startTime) fields.push(['startTime', summary.startTime]);
  if (summary.durationMinutes) fields.push(['durationMinutes', summary.durationMinutes]);
  if (summary.timeZone) fields.push(['timeZone', summary.timeZone]);
  if (summary.agenda) fields.push(['agenda', summary.agenda]);
  if (summary.createdAt) fields.push(['createdAt', summary.createdAt]);
  return Buffer.from(`{${fields.map(([name, value]) => `${goJSON(name)}:${goJSON(value)}`).join(',')}}`, 'utf8');
}

/** Nanoseconds since the Unix epoch of a canonical UTC timestamp, such as 2036-02-24T03:30:00.12Z. */
function instantNanoseconds(timestamp: string): bigint {
  const [whole = '', fraction = ''] = timestamp.replace(/Z$/, '').split('.');
  return BigInt(Date.parse(`${whole}Z`)) * 1_000_000n + BigInt(fraction.padEnd(9, '0').slice(0, 9) || '0');
}

/** Replace lone surrogates with U+FFFD, as Go decodes them. */
function wellFormed(text: string): string {
  return text.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD');
}

/** One listed meeting adapted to the kit's keyed snapshot. */
export class ListedMeeting implements KeyedItem {
  /** The tracked JSON, so a removal or update carries the last listed state. */ readonly value: Uint8Array;
  readonly #hash: string;
  /** Keep the listed summary beside its tracked JSON. */
  constructor(readonly summary: MeetingSummary) {
    this.value = trackedMeetingJSON(summary);
    this.#hash = createHash('sha256').update(this.value).digest().subarray(0, 16).toString('hex');
  }
  /** The decimal meeting ID. */ snapshotKey(): string { return String(this.summary.id); }
  /** The first 16 bytes of SHA-256 over the tracked fields. */ snapshotHash(): string { return this.#hash; }
  /** The tracked JSON. */ snapshotValue(): Uint8Array { return this.value; }
  /** Order two listings of one meeting ID: the earlier start, then the smaller hash. */
  listsBefore(other: ListedMeeting): boolean {
    const start = this.summary.startTime, otherStart = other.summary.startTime;
    if (start && otherStart) {
      const startNanoseconds = instantNanoseconds(start), otherNanoseconds = instantNanoseconds(otherStart);
      if (startNanoseconds !== otherNanoseconds) return startNanoseconds < otherNanoseconds;
    } else if (!start !== !otherStart) return Boolean(start);
    return this.#hash < other.#hash;
  }
}

const trackedMeetingSchema = z.object({
  id: z.number().int().positive(),
  hostId: z.string().nullish(), topic: z.string().nullish(), type: z.number().int().nullish(),
  startTime: z.string().nullish(), durationMinutes: z.number().int().nullish(), timeZone: z.string().nullish(),
  agenda: z.string().nullish(), createdAt: z.string().nullish(),
});

/** Read a stored snapshot value back as a summary without uuid or joinUrl; throws for an invalid entry. */
export function decodeTrackedMeeting(entry: KeyedSnapshotEntry): MeetingSummary {
  let tracked: z.infer<typeof trackedMeetingSchema>;
  try { tracked = trackedMeetingSchema.parse(JSON.parse(Buffer.from(entry.value ?? new Uint8Array()).toString('utf8'))); }
  catch { throw new Error('stored Zoom meeting snapshot entry is invalid'); }
  if (String(tracked.id) !== entry.key) throw new Error('stored Zoom meeting snapshot entry is invalid');
  const summary: MeetingSummary = { id: tracked.id, topic: tracked.topic ?? '' };
  if (tracked.hostId) summary.hostId = tracked.hostId;
  if (tracked.type) summary.type = tracked.type;
  if (tracked.startTime) summary.startTime = tracked.startTime;
  if (tracked.durationMinutes) summary.durationMinutes = tracked.durationMinutes;
  if (tracked.timeZone) summary.timeZone = tracked.timeZone;
  if (tracked.agenda) summary.agenda = tracked.agenda;
  if (tracked.createdAt) summary.createdAt = tracked.createdAt;
  return summary;
}

/**
 * Hash the change so a refetch keeps its ID while a change back and forth or a recovery gets a new one. An
 * update or removal includes the previous entry's hash and commit; a scheduled change, which has no
 * previous entry, includes the cursor's count of commits with changes.
 */
export function meetingChangeId(kind: MeetingChangeKind, key: string, previous: KeyedSnapshotEntry | undefined, currentHash: string, sequence: number): string {
  const parts = [kind, key, currentHash];
  if (previous) parts.push(previous.hash, String(previous.seen));
  else if (sequence > 0) parts.push(String(sequence));
  const digest = createHash('sha256').update(wellFormed(parts.join('\u0000')), 'utf8').digest();
  return `zoom-meeting-${digest.subarray(0, 16).toString('hex')}`;
}

/** Build the Trigger event; a scheduled meeting occurs when created, any other change when observed. */
export function newMeetingChangedTriggerEvent(previous: KeyedSnapshotEntry | undefined, currentHash: string, sequence: number, payload: MeetingChangedEvent, observedAt: Date): TriggerEvent<MeetingChangedEvent> {
  const key = String(payload.meeting.id);
  const occurredAt = payload.changeKind === MeetingChangeScheduled && payload.meeting.createdAt ? new Date(payload.meeting.createdAt) : observedAt;
  return { id: meetingChangeId(payload.changeKind, key, previous, currentHash, sequence), occurredAt, payload };
}
