// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { BambooHRClient, newConnection, type BambooHRClientOptions, type Connection } from '../src/index.js';

/** The only key the fake accepts, split so secret scanners do not mistake the 40-hex fixture for a real key. */
export const APIKey = '0123456789abcdef' + '0123456789abcdef01234567';
/** The fake clock's default start, 2026-10-13T09:00:00Z. */
export const Start = new Date(Date.UTC(2026, 9, 13, 9));
/** The connection the Trigger tests use. */
export const TriggerTestConnection = { provider: 'bamboohr', name: 'bamboohr-trigger-test' };

interface StoredEmployee { fields: Record<string, string>; action: string; lastChangedMs: number; isDeleted: boolean }

/**
 * A credential-checking BambooHR fake, ported from the Go connector's internal/bamboohrfake. It serves Get
 * Changed Employee IDs and Get Employee, keeps one change per employee as BambooHR does, and stamps changes
 * with its own clock, which it also sends in the Date header. The clock advances one second per change.
 */
export class BambooHRFake {
  /** The query of every change-list request, in arrival order. */ readonly changeListQueries: URLSearchParams[] = [];
  private clockMs: number;
  private readonly employees = new Map<string, StoredEmployee>();
  private rateLimitedChangeLists = 0;
  private rejectedChangeLists = 0;

  /** Start the clock at start, truncated to the second. */
  constructor(start: Date = Start) { this.clockMs = Math.floor(start.getTime() / 1000) * 1000; }

  /** Add an employee one second after the previous change, as an Inserted change. */ insert(employeeId: string, fields: Record<string, string> = {}): void { this.change(employeeId, 'Inserted', fields); }
  /** Merge fields into an employee one second after the previous change, as an Updated change. */ update(employeeId: string, fields: Record<string, string> = {}): void { this.change(employeeId, 'Updated', fields); }
  /** Delete an employee one second after the previous change. */ delete(employeeId: string): void { this.change(employeeId, 'Deleted', {}); }
  /** Move the clock, and so the Date header, forward without changing any employee. */ advance(milliseconds: number): void { this.clockMs = Math.floor((this.clockMs + milliseconds) / 1000) * 1000; }
  /** Add an employee stamped lastChanged without moving the clock, as a late-visible change. */
  insertLate(employeeId: string, lastChanged: Date): void { this.employees.set(employeeId, { fields: {}, action: 'Inserted', lastChangedMs: Math.floor(lastChanged.getTime() / 1000) * 1000, isDeleted: false }); }
  /** Answer the next count change-list requests with 429 and Retry-After: 1. */ rateLimitChangeLists(count: number): void { this.rateLimitedChangeLists = count; }
  /** Answer the next count change-list requests with 400. */ rejectChangeLists(count: number): void { this.rejectedChangeLists = count; }
  /** The fake's current time, which the Date header also reports. */ clock(): Date { return new Date(this.clockMs); }

  /** Answer one provider request; pass it as BambooHRClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    const headers: Record<string, string> = { date: new Date(this.clockMs).toUTCString() };
    if (request.headers?.Authorization !== `Basic ${Buffer.from(`${APIKey}:x`).toString('base64')}`) return { status: 401, headers, body: new Uint8Array() };
    if (request.method === 'GET' && url.pathname === '/api/v1/employees/changed') return this.serveChangeList(headers, url.searchParams);
    if (request.method === 'GET' && url.pathname.startsWith('/api/v1/employees/')) return this.serveEmployee(headers, url.pathname.slice('/api/v1/employees/'.length), url.searchParams.get('fields') ?? '');
    return { status: 404, headers, body: new Uint8Array() };
  };

  private change(employeeId: string, action: string, fields: Record<string, string>): void {
    this.clockMs += 1000;
    const record = this.employees.get(employeeId) ?? { fields: {}, action, lastChangedMs: this.clockMs, isDeleted: false };
    Object.assign(record.fields, fields);
    Object.assign(record, { action, lastChangedMs: this.clockMs, isDeleted: action === 'Deleted' });
    this.employees.set(employeeId, record);
  }

  private serveChangeList(headers: Record<string, string>, query: URLSearchParams): ProviderResponse {
    this.changeListQueries.push(query);
    if (this.rateLimitedChangeLists > 0) { this.rateLimitedChangeLists--; return { status: 429, headers: { ...headers, 'retry-after': '1' }, body: new Uint8Array() }; }
    if (this.rejectedChangeLists > 0) { this.rejectedChangeLists--; return json(400, headers, { error: { code: 'BadRequest' } }); }
    const sinceMs = Date.parse(query.get('since') ?? ''), changeType = query.get('type') ?? '';
    if (!Number.isFinite(sinceMs) || !['', 'inserted', 'updated', 'deleted', 'all'].includes(changeType)) return { status: 400, headers, body: new Uint8Array() };
    const employees: Record<string, unknown> = {};
    let latest = '';
    for (const [employeeId, record] of this.employees) {
      if (record.lastChangedMs <= sinceMs || (changeType && changeType !== 'all' && record.action.toLowerCase() !== changeType)) continue;
      const stamp = new Date(record.lastChangedMs).toISOString().replace(/\.\d{3}Z$/, '+00:00');
      employees[employeeId] = { id: employeeId, action: record.action, lastChanged: stamp };
      latest = stamp > latest ? stamp : latest;
    }
    return json(200, headers, { latest, employees: Object.keys(employees).length ? employees : [] });
  }

  private serveEmployee(headers: Record<string, string>, employeeId: string, fields: string): ProviderResponse {
    const record = this.employees.get(employeeId);
    if (!/^\d+$/.test(employeeId) || !record || record.isDeleted) return { status: 404, headers, body: new Uint8Array() };
    const body: Record<string, string> = { id: employeeId };
    for (const name of fields.split(',')) if (name in record.fields) body[name] = record.fields[name]!;
    return json(200, headers, body);
  }
}

function json(status: number, headers: Record<string, string>, body: unknown): ProviderResponse {
  return { status, headers: { ...headers, 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(body)) };
}

/** A client for the fake company acme with a 10-second poll interval. */
export function newFakeClient(fake: BambooHRFake, options: BambooHRClientOptions & { maxResponseBytes?: number; pollIntervalMs?: number } = {}): BambooHRClient {
  const { maxResponseBytes, pollIntervalMs, ...clientOptions } = options;
  const credentials = { resolve: async () => ({ apiKey: new SecretString(APIKey) }) };
  return new BambooHRClient({ companyDomain: 'acme', pollIntervalMs: pollIntervalMs ?? 10_000, ...(maxResponseBytes ? { maxResponseBytes } : {}) }, credentials,
    { apiBaseURL: 'https://acme.example/api/v1', send: fake.send, ...clientOptions });
}

/** A connection whose requests the fake answers. */
export function newFakeConnection(fake: BambooHRFake, name = TriggerTestConnection.name): Connection {
  return newConnection(newFakeClient(fake), { provider: 'bamboohr', name });
}

/**
 * A loaded project with one BambooHR API-key connection and its employeeChanged bindings over in-memory
 * storage; pass the same objects again to model a restarted process that reopens the storage.
 */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>, restartedObjects?: InMemoryObjectStore): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'bamboohr-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'bamboohr', connectionName }, 0, new CredentialMaterial({ api_key: APIKey }, undefined, 'apiKey'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    // Dex Web stores a duration as Go nanoseconds.
    connections: [{ connectorId: 'bamboohr', connectionName, modulePath: 'bamboohr', provider: 'bamboohr', configuration: { companyDomain: 'acme', pollInterval: 10_000_000_000 } }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'bamboohr', connectionName, triggerName: 'employeeChanged', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
