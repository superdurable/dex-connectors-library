// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, timingSafeEqual } from 'node:crypto';
import type { IncomingHttpHeaders } from 'node:http';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { isGuid } from './client.js';
import type { Envelope, EnvelopeCustomField } from './generated/models.js';
import { parseDocuSignTimestamp } from './operations.js';

/** The Connect event sent when every recipient finished. */
export const ConnectEventEnvelopeCompleted = 'envelope-completed';
/** The Connect event sent when a recipient declined. */
export const ConnectEventEnvelopeDeclined = 'envelope-declined';
/** The Connect event sent when the sender voided the envelope or it expired. */
export const ConnectEventEnvelopeVoided = 'envelope-voided';
/** The Connect events the envelopeEventReceived Trigger records. */
export const ConnectEnvelopeEvents: readonly string[] = [ConnectEventEnvelopeCompleted, ConnectEventEnvelopeDeclined, ConnectEventEnvelopeVoided];

/** One envelope Connect event name. */
export type ConnectEnvelopeEvent = typeof ConnectEventEnvelopeCompleted | typeof ConnectEventEnvelopeDeclined | typeof ConnectEventEnvelopeVoided;

/**
 * Filters the Connect events one binding records. An empty or omitted events list records every
 * envelope-completed, envelope-declined, and envelope-voided event. It reduces what a binding stores; the
 * application's TriggerFilter remains its admission rule.
 */
export const EnvelopeEventReceivedTriggerConfigurationSchema = z.strictObject({
  /** envelope-completed, envelope-declined, and envelope-voided; empty accepts all three. */ events: z.array(z.string()).nullish(),
}).superRefine((configuration, context) => {
  const events = configuration.events ?? [];
  if (events.some(event => !ConnectEnvelopeEvents.includes(event)) || new Set(events).size !== events.length) {
    context.addIssue({ code: 'custom', message: `envelopeEventReceived events must be distinct values from ${ConnectEnvelopeEvents.join(', ')}` });
  }
});
/** Event filter of one envelopeEventReceived binding. */
export type EnvelopeEventReceivedTriggerConfiguration = z.infer<typeof EnvelopeEventReceivedTriggerConfigurationSchema>;

/**
 * One HMAC-verified Connect envelope event in the JSON SIM format; field names and order match the Go
 * connector. Its Trigger event ID is the envelope ID and the event name, such as
 * 93be49ab-0000-0000-0000-f752070d71ec:envelope-completed, so a Connect retry or a second Connect
 * configuration delivering the same outcome keeps one ID.
 */
export interface EnvelopeEvent {
  /** envelope-completed, envelope-declined, or envelope-voided. */ event: string;
  /** The envelope's lowercase GUID. */ envelopeId: string;
  /** The sender's lowercase DocuSign API account GUID. */ accountId: string;
  /** When Connect generated the message, or when the endpoint received it if Connect left it blank, as RFC 3339 UTC. */ generatedAt: string;
  /** The envelope summary's lowercase status; absent unless the Connect configuration includes envelope data. */ status?: Envelope['status'];
  /** The envelope summary's void reason for an envelope-voided event, when included. */ voidedReason?: string;
  /** The envelope's text custom fields; absent unless the Connect configuration's Include Data lists Custom Fields. */ customFields?: EnvelopeCustomField[];
}

/** Runtime validator of a replayed EnvelopeEvent payload. */
export const EnvelopeEventSchema = z.object({
  event: z.string(), envelopeId: z.string(), accountId: z.string(), generatedAt: z.string(),
  status: z.string().optional(), voidedReason: z.string().optional(),
  customFields: z.array(z.object({ name: z.string(), value: z.string() })).optional(),
}).transform(value => value as EnvelopeEvent);

/** Return the value of the event's named text custom field, or undefined when it has none. */
export function envelopeEventCustomFieldValue(event: EnvelopeEvent, name: string): string | undefined {
  return event.customFields?.find(field => field.name === name)?.value;
}

/** A Connect message that is not a JSON SIM envelope event with GUID identities. */
export class ConnectMessageMalformedError extends Error {
  constructor() { super('DocuSign returned a malformed response'); this.name = 'ConnectMessageMalformedError'; }
}

/** No X-DocuSign-Signature-N header matches the HMAC-SHA256 of the body. */
export class ConnectSignatureInvalidError extends Error {
  constructor() { super('no X-DocuSign-Signature header matches the HMAC-SHA256 of the body'); this.name = 'ConnectSignatureInvalidError'; }
}

/** Header names DocuSign signs with: x-docusign-signature- and a 16-bit unsigned decimal, one per account HMAC key. */
const signatureHeaderPattern = /^x-docusign-signature-([0-9]+)$/;
const base64Pattern = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

/** Accept any X-DocuSign-Signature-N whose standard base64 equals the body's HMAC-SHA256 under hmacKey. */
export function verifyConnectSignature(body: Uint8Array, headers: IncomingHttpHeaders, hmacKey: string): void {
  const expected = createHmac('sha256', hmacKey).update(body).digest();
  for (const [name, raw] of Object.entries(headers)) {
    const suffix = signatureHeaderPattern.exec(name.toLowerCase())?.[1];
    if (suffix === undefined || Number(suffix) > 65_535 || raw === undefined) continue;
    // Node joins repeated custom headers with ", "; base64 never contains a comma.
    for (const value of (Array.isArray(raw) ? raw : [raw]).flatMap(entry => entry.split(','))) {
      const encoded = value.trim();
      if (!base64Pattern.test(encoded)) continue;
      const signature = Buffer.from(encoded, 'base64');
      if (signature.length === expected.length && timingSafeEqual(signature, expected)) return;
    }
  }
  throw new ConnectSignatureInvalidError();
}

/**
 * Decode a JSON SIM envelope outcome received at receivedAt. Another valid Connect event returns
 * undefined, so the endpoint acknowledges it unrecorded; anything else throws ConnectMessageMalformedError.
 */
export function decodeConnectMessage(body: Uint8Array, receivedAt: Date): TriggerEvent<EnvelopeEvent> | undefined {
  let message: unknown;
  try { message = JSON.parse(Buffer.from(body).toString('utf8')); }
  catch { throw new ConnectMessageMalformedError(); }
  const root = objectOrNull(message);
  const eventName = stringField(root, 'event');
  const data = objectOrNull(field(root, 'data'));
  const generatedDateTime = stringField(root, 'generatedDateTime');
  if (eventName === '' || data === null) throw new ConnectMessageMalformedError();
  const accountId = stringField(data, 'accountId');
  const envelopeId = stringField(data, 'envelopeId');
  const summary = field(data, 'envelopeSummary');
  if (!ConnectEnvelopeEvents.includes(eventName)) return undefined;
  if (!isGuid(envelopeId) || !isGuid(accountId)) throw new ConnectMessageMalformedError();
  const event: EnvelopeEvent = { event: eventName, envelopeId: envelopeId.toLowerCase(), accountId: accountId.toLowerCase(), generatedAt: goTimestamp(receivedAt) };
  // Go parity: an unparsable generation time falls back to the receipt time instead of refusing the event.
  if (generatedDateTime !== '') {
    try { event.generatedAt = parseDocuSignTimestamp(generatedDateTime); } catch { /* keep the receipt time */ }
  }
  // The summary is an object only when the Connect configuration includes envelope data.
  if (summary !== null && typeof summary === 'object' && !Array.isArray(summary)) {
    const status = stringField(summary as Record<string, unknown>, 'status').toLowerCase();
    const voidedReason = stringField(summary as Record<string, unknown>, 'voidedReason');
    const customFields = textCustomFields(objectOrNull(field(summary as Record<string, unknown>, 'customFields')));
    if (status !== '') event.status = status;
    if (voidedReason !== '') event.voidedReason = voidedReason;
    if (customFields.length > 0) event.customFields = customFields;
  }
  return { id: `${event.envelopeId}:${event.event}`, occurredAt: new Date(event.generatedAt), payload: event };
}

/** Return the named text custom fields in DocuSign's order; list custom fields are not read. */
function textCustomFields(fields: Record<string, unknown> | null): EnvelopeCustomField[] {
  const raw = field(fields, 'textCustomFields');
  if (raw === null || raw === undefined) return [];
  if (!Array.isArray(raw)) throw new ConnectMessageMalformedError();
  const converted: EnvelopeCustomField[] = [];
  for (const entry of raw) {
    const customField = objectOrNull(entry);
    const name = stringField(customField, 'name');
    const value = stringField(customField, 'value');
    if (name !== '') converted.push({ name, value });
  }
  return converted;
}

/** Read a JSON object field with Go encoding/json's case-insensitive key match; the last matching key wins. */
function field(object: Record<string, unknown> | null, name: string): unknown {
  if (object === null) return undefined;
  let found: unknown;
  for (const [key, value] of Object.entries(object)) if (key === name || key.toLowerCase() === name.toLowerCase()) found = value;
  return found;
}

/** Decode an optional string field as Go does: absent or null is blank, and another JSON type is malformed. */
function stringField(object: Record<string, unknown> | null, name: string): string {
  const value = field(object, name);
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new ConnectMessageMalformedError();
  return value;
}

/** Decode an optional JSON object as Go does into a pointer: null stays null, and another type is malformed. */
function objectOrNull(value: unknown): Record<string, unknown> | null {
  if (value === undefined || value === null) return null;
  if (typeof value !== 'object' || Array.isArray(value)) throw new ConnectMessageMalformedError();
  return value as Record<string, unknown>;
}

/** Format a time as Go's time.Time JSON in UTC, without trailing fractional zeros. */
function goTimestamp(value: Date): string { return value.toISOString().replace(/\.([0-9]*?)0*Z$/, (_match, fraction: string) => fraction ? `.${fraction}Z` : 'Z'); }
