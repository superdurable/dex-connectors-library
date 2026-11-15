// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { MailchimpClient } from '../src/client.js';

/** The only key the fake accepts, split so secret scanners do not read a fixture as a credential. */
export const APIKey = '0123456789abcdef' + '0123456789abcdef' + '-us6';
/** The base URL to pass as MailchimpClientOptions.apiBaseURL. */
export const APIBaseURL = 'https://mailchimp.example/3.0';

/** The audience the unit tests watch. */
export const audienceId = '57afe96172';

/** A client whose requests the fake answers. */
export function newFakeClient(audience: MailchimpAudienceFake, pollIntervalMs?: number): MailchimpClient {
  return new MailchimpClient(pollIntervalMs ? { pollIntervalMs } : {}, { resolve: async () => ({ apiKey: new SecretString(APIKey) }) }, { apiBaseURL: APIBaseURL, send: audience.send });
}

/** One injected response for the next list request. */
export interface Failure { status: number; retryAfter?: string; withoutDocument?: boolean }

interface StoredMember { wire: Record<string, unknown>; changedAt: number }

/** Return Mailchimp's member ID for emailAddress. */
export function subscriberHash(emailAddress: string): string {
  return createHash('md5').update(emailAddress.toLowerCase()).digest('hex');
}

/**
 * A fake of one Mailchimp audience's contact list and contact reads, as a MailchimpRequestSender. It models
 * the list semantics the memberChanged Trigger relies on, as the Go connector's mailchimpaudiencefake does:
 * since_last_changed keeps contacts changed strictly after the bound, sort_field=last_changed orders by change
 * second, offset skips matching contacts, and fields keeps only the listed members.* fields.
 */
export class MailchimpAudienceFake {
  /** Every authenticated request as method, path, and query. */ readonly requests: string[] = [];
  /** The time of the latest upsert, in milliseconds; it starts at 2026-11-01T09:00:00Z. */ clockMs = Date.UTC(2026, 10, 1, 9, 0, 0);
  private readonly members = new Map<string, StoredMember>();
  private readonly failures: Failure[] = [];

  /** Bind the audience ID. */
  constructor(readonly audienceId: string) {}

  /** Add or change the contact one second after the previous change and return its hash. */
  upsert(emailAddress: string, status: string, firstName: string): string {
    this.clockMs += 1000;
    return this.upsertAt(emailAddress, status, firstName, this.clockMs);
  }

  /** Add or change the contact with an explicit change time, which may lie before earlier changes. */
  upsertAt(emailAddress: string, status: string, firstName: string, changedAtMs: number): string {
    const changedAt = Math.floor(changedAtMs / 1000) * 1000;
    const id = subscriberHash(emailAddress);
    const lastChanged = `${new Date(changedAt).toISOString().slice(0, 19)}+00:00`;
    this.members.set(id, { wire: { id, email_address: emailAddress, status, merge_fields: { FNAME: firstName }, last_changed: lastChanged, list_id: this.audienceId }, changedAt });
    return id;
  }

  /** Answer the next list requests with failures, in order. */
  failNextList(...failures: Failure[]): void { this.failures.push(...failures); }

  /** Serve one request. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    if (request.headers?.Authorization !== `Bearer ${APIKey}`) return problem(401, 'API Key Invalid');
    const url = new URL(request.url);
    this.requests.push(`${request.method} ${url.pathname}?${url.search.slice(1)}`);
    const prefix = `/3.0/lists/${this.audienceId}/members`;
    if (request.method !== 'GET') return problem(405, 'Method Not Allowed');
    if (url.pathname === prefix) return this.serveList(url.searchParams);
    if (url.pathname.startsWith(`${prefix}/`)) {
      const found = this.members.get(url.pathname.slice(prefix.length + 1));
      return found ? json(200, found.wire) : problem(404, 'Resource Not Found');
    }
    return problem(404, 'Resource Not Found');
  };

  private serveList(params: URLSearchParams): ProviderResponse {
    const failure = this.failures.shift();
    if (failure) {
      const headers = failure.retryAfter ? { 'retry-after': failure.retryAfter } : {};
      return failure.withoutDocument ? { status: failure.status, headers, body: new Uint8Array() } : problem(failure.status, 'Injected failure', headers);
    }
    const count = Number(params.get('count')); const offset = Number(params.get('offset'));
    if (!Number.isInteger(count) || count < 1 || count > 1000 || !Number.isInteger(offset) || offset < 0 || params.get('sort_field') !== 'last_changed') return problem(400, 'Invalid Resource');
    const sinceText = params.get('since_last_changed');
    const since = sinceText ? Date.parse(sinceText) : undefined;
    const isDescending = params.get('sort_dir') === 'DESC';
    const matching = [...this.members.values()].filter(member => since === undefined || member.changedAt > since)
      .sort((left, right) => left.changedAt !== right.changedAt ? (left.changedAt - right.changedAt) * (isDescending ? -1 : 1) : String(left.wire.id) < String(right.wire.id) ? -1 : 1);
    const fields = (params.get('fields') ?? '').split(',').filter(Boolean).map(field => field.replace(/^members\./, ''));
    const members = matching.slice(offset, offset + count).map(member => Object.fromEntries(Object.entries(member.wire).filter(([name]) => !fields.length || fields.includes(name))));
    return json(200, { members, list_id: this.audienceId, total_items: matching.length });
  }
}

function json(status: number, value: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers: { 'content-type': 'application/json', ...headers }, body: Buffer.from(JSON.stringify(value), 'utf8') };
}

function problem(status: number, title: string, headers: Record<string, string> = {}): ProviderResponse {
  return json(status, { type: 'https://mailchimp.com/developer/marketing/docs/errors/', title }, headers);
}

/**
 * A loaded project with one Mailchimp connection and its memberChanged bindings over in-memory storage; pass
 * the same objects again to model a restarted process that reopens the storage the first one wrote.
 */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>,
  restartedObjects?: InMemoryObjectStore): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'mailchimp-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'mailchimp', connectionName }, 0, new CredentialMaterial({ api_key: APIKey }, undefined, 'apiKey'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    // Dex Web stores a duration as Go nanoseconds.
    connections: [{ connectorId: 'mailchimp', connectionName, modulePath: 'mailchimp', provider: 'mailchimp', configuration: { pollInterval: 5_000_000_000 } }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'mailchimp', connectionName, triggerName: 'memberChanged', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
