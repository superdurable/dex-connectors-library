// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { MixpanelClient, MixpanelExchange } from './client.js';
import { TrackEventDefinition, ExportEventsDefinition } from './generated/definitions.js';
import type { TrackEventInput, EventImportResult, ExportEventsInput, ExportedEvents, ExportedEvent } from './generated/models.js';

const emptyImport: EventImportResult = { imported: false };
const emptyExport: ExportedEvents = { events: [] };
const reservedProperties = new Set(['time', 'distinct_id', '$insert_id', 'ip']);

/** Stable, deduplicated event import mutation. */
export class TrackEventOperation implements Mutation<TrackEventInput, EventImportResult> {
  /** Bind the provider client. */ constructor(private readonly client: MixpanelClient) {}
  /** Return the manifest definition. */ definition() { return TrackEventDefinition; }
  /** Derive the Go SHA-256 UUID-shaped insert ID from the Dex Call ID. */
  idempotencyKey(callId: string): string {
    const hex = createHash('sha256').update(callId, 'utf8').digest('hex').slice(0, 32);
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
  }
  /** Validate, import, and classify one event. */
  async invoke(call: Call, input: TrackEventInput): Promise<MutationAttempt<EventImportResult>> {
    const operation = 'trackEvent';
    if (!input.event?.trim() || !input.distinctID?.trim() || !Number.isSafeInteger(input.time) || input.time <= 0 || call.idempotencyKey?.length !== 36) {
      return mutationFailure('defect', 'VALIDATION', 'event, distinctID, time, and stable call identity are required');
    }
    const properties: Record<string, string | number> = { time: input.time, distinct_id: input.distinctID, '$insert_id': call.idempotencyKey, ip: 0 };
    for (const [key, value] of Object.entries(input.properties ?? {})) {
      if (!key.trim() || reservedProperties.has(key)) return mutationFailure('defect', 'VALIDATION', 'event properties include a reserved or blank name');
      properties[key] = value;
    }
    let payload: Uint8Array;
    try { payload = Buffer.from(JSON.stringify([{ event: input.event, properties }]), 'utf8'); }
    catch { return mutationFailure('defect', 'VALIDATION', 'event payload is invalid or too large'); }
    if (payload.length >= 1 << 20) return mutationFailure('defect', 'VALIDATION', 'event payload is invalid or too large');
    const query = new URLSearchParams({ project_id: String(this.client.projectId), strict: '1' });
    const reply = await this.client.exchange(call, operation, 'POST', `${this.client.baseUrl}/import?${query}`, payload);
    if ('failure' in reply) return mutationExchangeFailure(reply);
    if (reply.status !== 200) return mutationFailure('providerRejected', rejectionKind(reply.status), `Mixpanel rejected event import with HTTP ${reply.status}`);
    try {
      const response: unknown = JSON.parse(Buffer.from(reply.body).toString('utf8'));
      if (typeof response === 'object' && response !== null && 'code' in response && response.code === 200 && 'num_records_imported' in response && response.num_records_imported === 1 && 'status' in response && response.status === 'OK') {
        return newMutationBranch('imported', { imported: true }, zeroReceipt());
      }
    } catch { /* malformed confirmation selects invalidResponse */ }
    return mutationFailure('invalidResponse', 'PROTOCOL', 'Mixpanel returned an invalid import confirmation');
  }
}

/** Bounded Mixpanel raw event export query. */
export class ExportEventsOperation implements Query<ExportEventsInput, ExportedEvents> {
  /** Bind the provider client. */ constructor(private readonly client: MixpanelClient) {}
  /** Return the manifest definition. */ definition() { return ExportEventsDefinition; }
  /** Validate, export, and classify one bounded day range. */
  async invoke(call: Call, input: ExportEventsInput): Promise<QueryAttempt<ExportedEvents>> {
    if (!validDate(input.fromDate) || !validDate(input.toDate) || !Number.isSafeInteger(input.limit) || input.limit < 1 || input.limit > 100) return queryFailure('defect', 'VALIDATION', 'date range and limit must be valid');
    const from = Date.parse(`${input.fromDate}T00:00:00Z`);
    const to = Date.parse(`${input.toDate}T00:00:00Z`);
    if (from > to || to - from > 30 * 86_400_000) return queryFailure('defect', 'VALIDATION', 'date range and limit must be valid');
    const query = new URLSearchParams({ from_date: input.fromDate, limit: String(input.limit), project_id: String(this.client.projectId), to_date: input.toDate });
    const reply = await this.client.exchange(call, 'exportEvents', 'GET', `${this.client.exportBaseUrl}/export?${query}`);
    if ('failure' in reply) return queryExchangeFailure(reply);
    if (reply.status !== 200) return queryFailure('providerRejected', rejectionKind(reply.status), `Mixpanel rejected raw export with HTTP ${reply.status}`);
    const events: ExportedEvent[] = [];
    try {
      for (const line of Buffer.from(reply.body).toString('utf8').split('\n')) {
        if (!line.trim()) continue;
        const value: unknown = JSON.parse(line);
        if (!value || typeof value !== 'object' || !('event' in value) || typeof value.event !== 'string' || !value.event) throw new TypeError('invalid event');
        const rawProperties = 'properties' in value ? value.properties : undefined;
        const properties = rawProperties && typeof rawProperties === 'object' ? rawProperties as Record<string, unknown> : {};
        events.push({ event: value.event, distinctID: typeof properties.distinct_id === 'string' ? properties.distinct_id : '', time: typeof properties.time === 'number' ? properties.time : 0, insertID: typeof properties.$insert_id === 'string' ? properties.$insert_id : '' });
        if (events.length > input.limit) return queryFailure('invalidResponse', 'PROTOCOL', 'Mixpanel returned more events than requested');
      }
    } catch { return queryFailure('invalidResponse', 'PROTOCOL', 'Mixpanel returned invalid JSONL'); }
    return newQueryBranch('exported', { events }, zeroReceipt());
  }
}

function validDate(value: string): boolean { return /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(`${value}T00:00:00Z`)) && new Date(`${value}T00:00:00Z`).toISOString().startsWith(value); }
function rejectionKind(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'mixpanel', operation, message }; }
function mutationFailure(branch: string, kind: Failure['kind'], message: string): MutationAttempt<EventImportResult> { return newMutationBranch(branch, emptyImport, zeroReceipt(), failure('trackEvent', kind, message)); }
function queryFailure(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ExportedEvents> { return newQueryBranch(branch, emptyExport, zeroReceipt(), failure('exportEvents', kind, message)); }
function mutationExchangeFailure(reply: Extract<MixpanelExchange, { failure: Failure }>): MutationAttempt<EventImportResult> { return reply.disposition === 'retry' ? newMutationRetry(reply.failure, reply.retryAfterMs) : newMutationBranch(reply.disposition === 'defect' ? 'defect' : 'invalidResponse', emptyImport, zeroReceipt(), reply.failure); }
function queryExchangeFailure(reply: Extract<MixpanelExchange, { failure: Failure }>): QueryAttempt<ExportedEvents> { return reply.disposition === 'retry' ? newQueryRetry(reply.failure, reply.retryAfterMs) : newQueryBranch(reply.disposition === 'defect' ? 'defect' : 'invalidResponse', emptyExport, zeroReceipt(), reply.failure); }
