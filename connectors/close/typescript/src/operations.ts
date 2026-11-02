// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { CloseClient } from './client.js';
import { emptyLead, emptyLeadPage } from './client.js';
import { GetLeadDefinition, ListLeadsDefinition } from './generated/definitions.js';
import type { GetLeadInput, Lead, LeadPage, ListLeadsInput } from './generated/models.js';

const safeLeadID = /^lead_[A-Za-z0-9_-]{0,123}$/;

/** Read one bounded Close lead page. */
export class ListLeadsOperation implements Query<ListLeadsInput, LeadPage> {
  /** Bind one authenticated client. */ constructor(private readonly client: CloseClient) {}
  /** Return the manifest definition. */ definition() { return ListLeadsDefinition; }
  /** Validate the page window and classify one request. */
  invoke(call: Call, input: ListLeadsInput): Promise<QueryAttempt<LeadPage>> {
    const skip = input.skip ?? 0;
    const limit = input.limit ?? 0;
    if (!Number.isSafeInteger(skip) || !Number.isSafeInteger(limit) || skip < 0 || limit < 0 || limit > 100) {
      return Promise.resolve(newQueryBranch('defect', emptyLeadPage, {}, { kind: 'VALIDATION', provider: 'close', operation: 'listLeads', message: 'skip or limit is invalid' }));
    }
    return this.client.get(call, 'listLeads', `/lead/?_skip=${skip}&_limit=${limit || 100}`, 'listed', 'providerRejected', decodeLeadPage, emptyLeadPage);
  }
}

/** Read one Close lead by stable provider ID. */
export class GetLeadOperation implements Query<GetLeadInput, Lead> {
  /** Bind one authenticated client. */ constructor(private readonly client: CloseClient) {}
  /** Return the manifest definition. */ definition() { return GetLeadDefinition; }
  /** Validate the ID and classify one request. */
  invoke(call: Call, input: GetLeadInput): Promise<QueryAttempt<Lead>> {
    if (!safeLeadID.test(input.leadId)) return Promise.resolve(newQueryBranch('defect', emptyLead, {}, { kind: 'VALIDATION', provider: 'close', operation: 'getLead', message: 'leadId must be a Close lead ID' }));
    return this.client.get(call, 'getLead', `/lead/${input.leadId}/`, 'found', 'notFound', decodeLead, emptyLead);
  }
}

function decodeLeadPage(body: Uint8Array): LeadPage {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!isObject(document) || !Array.isArray(document.data)) throw new TypeError('Close lead page is incomplete');
  if (document.has_more !== undefined && typeof document.has_more !== 'boolean') throw new TypeError('Close pagination flag is invalid');
  const leads = document.data.map(decodeLeadValue);
  return { leads, hasMore: document.has_more === true };
}

function decodeLead(body: Uint8Array): Lead {
  const lead = decodeLeadValue(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
  if (!lead.id || !lead.name) throw new TypeError('Close lead is incomplete');
  return lead;
}
function decodeLeadValue(value: unknown): Lead {
  if (!isObject(value)) throw new TypeError('Close lead is invalid');
  if ((value.id !== undefined && typeof value.id !== 'string') || (value.name !== undefined && typeof value.name !== 'string')) throw new TypeError('Close lead fields are invalid');
  return { id: typeof value.id === 'string' ? value.id : '', name: typeof value.name === 'string' ? value.name : '' };
}
function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
