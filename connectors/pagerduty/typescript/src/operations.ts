// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { PagerDutyClient, ExchangeFailure } from './client.js';
import { GetIncidentDefinition, ListIncidentsDefinition } from './generated/definitions.js';
import type { GetIncidentInput, Incident, IncidentPage, ListIncidentsInput } from './generated/models.js';

const emptyIncident: Incident = { id: '', title: '', status: '' };
const emptyPage: IncidentPage = { incidents: [], limit: 0, offset: 0, more: false };

/** List one bounded page of PagerDuty incidents. */
export class ListIncidentsOperation implements Query<ListIncidentsInput, IncidentPage> {
  /** Bind a provider client. */ constructor(private readonly client: PagerDutyClient) {}
  /** Return the manifest definition. */ definition() { return ListIncidentsDefinition; }
  /** Validate and classify one list attempt. */
  async invoke(call: Call, input: ListIncidentsInput): Promise<QueryAttempt<IncidentPage>> {
    const limit = input.limit || 25;
    const offset = input.offset ?? 0;
    if (!Number.isInteger(limit) || limit < 1 || limit > 100 || !Number.isInteger(offset) || offset < 0) return failed('listIncidents', 'defect', 'VALIDATION', 'limit must be 1–100 and offset nonnegative', emptyPage);
    const result = await this.client.exchange(call, 'listIncidents', `/incidents?limit=${limit}&offset=${offset}`, false);
    if ('outcome' in result) return classify(result.outcome, emptyPage);
    try {
      const document = JSON.parse(Buffer.from(result.response.body).toString('utf8')) as Record<string, unknown>;
      if (!document || typeof document !== 'object' || !Array.isArray(document.incidents)) throw new TypeError('incidents missing');
      const incidents = document.incidents.map(decodeIncident);
      return newQueryBranch('listed', { incidents, limit: goInteger(document.limit), offset: goInteger(document.offset), more: goBoolean(document.more) }, {});
    } catch { return failed('listIncidents', 'invalidResponse', 'PROTOCOL', 'provider response is incomplete or malformed', emptyPage); }
  }
}

/** Look up one PagerDuty incident. */
export class GetIncidentOperation implements Query<GetIncidentInput, Incident> {
  /** Bind a provider client. */ constructor(private readonly client: PagerDutyClient) {}
  /** Return the manifest definition. */ definition() { return GetIncidentDefinition; }
  /** Validate and classify one lookup attempt. */
  async invoke(call: Call, input: GetIncidentInput): Promise<QueryAttempt<Incident>> {
    if (!input.id) return failed('getIncident', 'defect', 'VALIDATION', 'incident ID is required', emptyIncident);
    const result = await this.client.exchange(call, 'getIncident', `/incidents/${goPathEscape(input.id)}`, true);
    if ('outcome' in result) return classify(result.outcome, emptyIncident);
    try {
      const document = JSON.parse(Buffer.from(result.response.body).toString('utf8')) as Record<string, unknown>;
      const incident = decodeIncident(document.incident);
      if (!incident.id) throw new TypeError('incident missing');
      return newQueryBranch('found', incident, {});
    } catch { return failed('getIncident', 'invalidResponse', 'PROTOCOL', 'provider response is incomplete or malformed', emptyIncident); }
  }
}

function classify<Output>(outcome: ExchangeFailure, empty: Output): QueryAttempt<Output> {
  if (outcome.disposition === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.disposition, empty, {}, outcome.failure);
}
function failed<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, { kind, provider: 'pagerduty', operation, message });
}
function decodeIncident(value: unknown): Incident {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('incident invalid');
  const entry = value as Record<string, unknown>;
  return { id: goString(entry.id), title: goString(entry.title), status: goString(entry.status) };
}
function goString(value: unknown): string { if (value == null) return ''; if (typeof value !== 'string') throw new TypeError('string expected'); return value; }
function goInteger(value: unknown): number { if (value == null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('integer expected'); return value; }
function goBoolean(value: unknown): boolean { if (value == null) return false; if (typeof value !== 'boolean') throw new TypeError('boolean expected'); return value; }
function goPathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replaceAll('%3A', ':').replaceAll('%40', '@').replaceAll('%26', '&').replaceAll('%3D', '=').replaceAll('%2B', '+').replaceAll('%24', '$'); }
