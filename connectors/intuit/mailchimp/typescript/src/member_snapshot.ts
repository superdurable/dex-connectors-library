// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { KeyedItem } from '@superdurable/connectors-sdk/pollingtrigger';
import { z } from 'zod';
import { MemberSchema, type Member } from './generated/models.js';

/** Every status the memberStatusPicker offers, in its display order. */
export const MemberStatuses = ['subscribed', 'unsubscribed', 'cleaned', 'pending', 'transactional', 'archived'] as const;

/**
 * The memberChanged binding configuration that the audienceIdEntry and memberStatusPicker units save in Dex
 * Web. Changing the audience of a binding that already polled makes its next poll record a new baseline.
 */
export const MemberChangedTriggerConfigurationSchema = z.strictObject({
  /** The watched audience, Mailchimp's list_id, such as 57afe96172. It is required. */
  audienceId: z.string(),
  /**
   * Keeps only contacts whose status after the change is one of these, such as subscribed; empty delivers
   * changed contacts in every status. A filtered-out change still moves the cursor.
   */
  statuses: z.array(z.string()).nullable().optional(),
}).superRefine((configuration, context) => {
  if (!/^[A-Za-z0-9]{1,64}$/.test(configuration.audienceId)) context.addIssue({ code: 'custom', message: 'audienceId must be a Mailchimp ID of 1 to 64 letters and digits' });
  const statuses = configuration.statuses ?? [];
  statuses.forEach((status, index) => {
    if (!(MemberStatuses as readonly string[]).includes(status)) context.addIssue({ code: 'custom', message: `statuses[${index}] ${goJSON(status)} is not a Mailchimp contact status` });
    else if (statuses.slice(0, index).includes(status)) context.addIssue({ code: 'custom', message: `statuses lists ${goJSON(status)} twice` });
  });
});
/** The memberChanged binding configuration. */
export type MemberChangedTriggerConfiguration = z.infer<typeof MemberChangedTriggerConfigurationSchema>;

/**
 * One contact added to or changed in the watched audience; field names match the Go connector, so Go and
 * TypeScript replicas share a binding's inbox. Mailchimp reports no change type, so a new contact and an
 * edited one look alike; member.signedUpAt, member.optedInAt, and member.status tell them apart. A contact
 * that changed several times between two polls is delivered once, in its latest state.
 */
export const MemberChangedEventSchema = z.object({
  /** The watched audience. */ audienceId: z.string().min(1),
  /** The contact as Mailchimp listed it after the change; member.lastChangedAt is the change time. */ member: MemberSchema,
});
/** One memberChanged delivery. */
export type MemberChangedEvent = z.infer<typeof MemberChangedEventSchema>;

/** Report whether a contact in status passes the binding's status filter. */
export function acceptsMemberStatus(configuration: MemberChangedTriggerConfiguration, status: string): boolean {
  const statuses = configuration.statuses ?? [];
  return statuses.length === 0 || statuses.includes(status);
}

/** Encode a decoded JSON value as Go marshals the same value decoded into any. */
export function goJSON(value: unknown): string {
  if (value === null || value === undefined) return 'null';
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  if (typeof value === 'string') return goStringJSON(value);
  if (typeof value === 'number') return goFloatJSON(value);
  if (Array.isArray(value)) return `[${value.map(goJSON).join(',')}]`;
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>).map(([key, item]) => [wellFormed(key), item] as const);
    entries.sort(([left], [right]) => Buffer.compare(Buffer.from(left, 'utf8'), Buffer.from(right, 'utf8')));
    return `{${entries.map(([key, item]) => `${goStringJSON(key)}:${goJSON(item)}`).join(',')}}`;
  }
  throw new TypeError('a merge field value is not JSON');
}

function goStringJSON(value: string): string {
  return JSON.stringify(wellFormed(value)).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Go writes float64 with the shortest digits and an exponent outside [1e-6, 1e21), as JavaScript does. */
function goFloatJSON(value: number): string {
  if (!Number.isFinite(value)) throw new TypeError('a merge field number is not finite');
  if (Object.is(value, -0)) return '-0';
  return String(value);
}

function wellFormed(value: string): string {
  return value.replace(/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/g, '\ufffd');
}

/** Encode the contact as Go marshals Member: declaration order, omitempty, and sorted tags. */
function memberJSON(member: Member): string {
  const fields: Array<[string, string]> = [['subscriberHash', goStringJSON(member.subscriberHash)], ['emailAddress', goStringJSON(member.emailAddress)]];
  if (member.contactId) fields.push(['contactId', goStringJSON(member.contactId)]);
  if (member.fullName) fields.push(['fullName', goStringJSON(member.fullName)]);
  fields.push(['status', goStringJSON(member.status)]);
  if (member.emailType) fields.push(['emailType', goStringJSON(member.emailType)]);
  if (member.mergeFields && Object.keys(member.mergeFields).length) fields.push(['mergeFields', goJSON(member.mergeFields)]);
  if (member.tags?.length) {
    // Tag order is not part of the version; Go sorts by bytes.
    const tags = member.tags.map(wellFormed).sort((left, right) => Buffer.compare(Buffer.from(left, 'utf8'), Buffer.from(right, 'utf8')));
    fields.push(['tags', goJSON(tags)]);
  }
  fields.push(['tagCount', String(member.tagCount)]);
  if (member.isVip) fields.push(['isVip', 'true']);
  if (member.language) fields.push(['language', goStringJSON(member.language)]);
  fields.push(['listId', goStringJSON(member.listId)]);
  if (member.webId) fields.push(['webId', String(member.webId)]);
  for (const [name, moment] of [['signedUpAt', member.signedUpAt], ['optedInAt', member.optedInAt], ['lastChangedAt', member.lastChangedAt]] as const) {
    if (moment) fields.push([name, `"${moment}"`]);
  }
  return `{${fields.map(([name, encoded]) => `"${name}":${encoded}`).join(',')}}`;
}

/** Hash the audience and every listed field of the contact version, as Go's memberVersionDigest. */
export function memberVersionDigest(audienceId: string, member: Member, bytes: number): string {
  const digest = createHash('sha256').update(Buffer.from(wellFormed(audienceId), 'utf8')).update(Buffer.from([0])).update(Buffer.from(memberJSON(member), 'utf8')).digest();
  return digest.subarray(0, bytes).toString('hex');
}

/** The Trigger event ID of one contact version; two versions in one second get different IDs. */
export function memberChangeID(audienceId: string, member: Member): string {
  return `mailchimp-${memberVersionDigest(audienceId, member, 16)}`;
}

/** Keys a contact's delivered version, so rereading the lookback window delivers nothing twice. */
export class MemberSnapshotItem implements KeyedItem {
  /** Bind the audience and the listed contact. */
  constructor(readonly audienceId: string, readonly member: Member) {}
  /** The subscriber hash. */ snapshotKey(): string { return this.member.subscriberHash; }
  /** An 8-byte SHA-256 prefix of the contact version. */ snapshotHash(): string { return memberVersionDigest(this.audienceId, this.member, 8); }
  /** No value is kept. */ snapshotValue(): Uint8Array | undefined { return undefined; }
}

/** Convert one listed contact; its ID is a pure function of the contact version. */
export function newMemberChangedTriggerEvent(audienceId: string, member: Member): TriggerEvent<MemberChangedEvent> {
  return { id: memberChangeID(audienceId, member), occurredAt: new Date(member.lastChangedAt!), payload: { audienceId, member } };
}
