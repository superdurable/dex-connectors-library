// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newMemoryCursorStore } from '@superdurable/connectors-sdk/pollingtrigger';
import { InMemoryObjectStore, ObjectNotFoundError, TriggerInbox, loadFromEnvironment, newDurableTriggerTarget, newPollCursorStore, parseTriggerPayloadJSON, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { SpreadsheetClient } from '../src/client.js';
import { RowAddedEventSchema, newConnection, newProjectConnection, newProjectRowAddedTriggerRunner, newRowAddedTrigger, newRowAddedTriggerRunner, type Connection, type RowAddedEvent, type RowAddedTriggerRoute } from '../src/index.js';
import { SheetsFake } from './sheets_fake.js';

const leadsHeader = ['email', 'name', 'status'];
const leads = { spreadsheetId: 'sheet-1', sheetName: 'Leads' };
const credential = { accessToken: new SecretString('SENTINEL-ACCESS-TOKEN'), oauthClientId: 'client', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };
const cleanups: Array<() => Promise<void>> = [];

class RecordingRowTarget implements TriggerTarget<RowAddedEvent> {
  readonly rows: number[] = [];
  async handleTrigger(_context: TriggerContext, event: TriggerEvent<RowAddedEvent>): Promise<void> { this.rows.push(event.payload.rowNumber); }
}

async function newSheet(...rows: string[][]): Promise<SheetsFake> {
  const sheet = await SheetsFake.start('Leads', ...rows);
  cleanups.push(() => sheet.close());
  return sheet;
}

function newTestConnection(endpoint: string): Connection {
  const client = new SpreadsheetClient({ endpoint, pollIntervalMs: 1000 }, { resolve: async () => credential, resolveWithRefresh: async () => credential });
  return newConnection(client, { provider: 'google', name: 'sheets-leads' });
}

/** Run in the background and abort once condition holds, within 20 seconds. */
async function runUntil(run: (context: TriggerContext) => Promise<void>, condition: () => boolean): Promise<void> {
  const controller = new AbortController();
  let failure: unknown;
  const running = run(newTriggerContext(controller.signal)).catch((error: unknown) => { failure = error; });
  const deadline = Date.now() + 20_000;
  while (!condition() && failure === undefined && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 20));
  controller.abort();
  await running;
  expect(condition()).toBe(true);
  expect(failure).toBeInstanceOf(TriggerDeliveryAbortedError);
}

afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

describe('rowAdded runners', () => {
  it('seeds then delivers appended rows with the memory-cursor Trigger', async () => {
    const sheet = await newSheet(leadsHeader, ['existing']);
    const target = new RecordingRowTarget();
    const connection = newTestConnection(sheet.url);
    const trigger = newRowAddedTrigger({ connection, connectionName: 'sheets-leads', bindingName: 'new-leads', configuration: leads, target });
    sheet.appendRowsAfterRequests(1, ['added']);
    await runUntil(context => trigger.run(context), () => target.rows.length === 1);
    expect(target.rows).toEqual([3]);
    expect(() => newRowAddedTrigger({ connection, connectionName: 'sheets-leads', bindingName: 'new-leads', configuration: { spreadsheetId: '', sheetName: '' }, target })).toThrow('spreadsheetId and sheetName are required');
  });

  it('rejects incomplete routes', () => {
    const connection = newTestConnection('https://sheets.example.test');
    const valid: RowAddedTriggerRoute = { bindingName: 'new-leads', configuration: leads, cursor: newMemoryCursorStore(), target: new RecordingRowTarget() };
    newRowAddedTriggerRunner({ connection, routes: [valid] });
    const incomplete = [
      { ...valid, bindingName: '' },
      { ...valid, cursor: undefined },
      { ...valid, target: undefined },
      { ...valid, configuration: { spreadsheetId: '', sheetName: '' } },
    ] as unknown as RowAddedTriggerRoute[];
    for (const route of incomplete) expect(() => newRowAddedTriggerRunner({ connection, routes: [route] })).toThrow();
    expect(() => newRowAddedTriggerRunner({ connection, routes: [valid, { ...valid, cursor: newMemoryCursorStore() }] })).toThrow();
    expect(() => newRowAddedTriggerRunner({ connection, routes: [] })).toThrow();
    expect(() => newRowAddedTriggerRunner({ connection: undefined as unknown as Connection, routes: [valid] })).toThrow();
  });

  it('keeps a durable cursor across restarts', async () => {
    const sheet = await newSheet(leadsHeader, ['existing']);
    const objects = new InMemoryObjectStore();
    const scope = { projectId: 'sheets', kind: 'preview' } as const;
    const key: TriggerInboxKey = { connectorId: 'google-sheets', connectionName: 'sheets-leads', triggerName: 'rowAdded', bindingName: 'new-leads' };
    const inbox = new TriggerInbox(objects, scope, key);
    const target = new RecordingRowTarget();
    const newRun = () => {
      const durable = newDurableTriggerTarget(inbox, key, target, { decodePayload: (_json, bytes) => RowAddedEventSchema.parse(parseTriggerPayloadJSON(bytes)) });
      const runner = newRowAddedTriggerRunner({ connection: newTestConnection(sheet.url), routes: [{ bindingName: 'new-leads', configuration: leads, cursor: newPollCursorStore(objects, scope, key), target: durable }] });
      return (context: TriggerContext) => runner.run(context);
    };
    sheet.appendRowsAfterRequests(1, ['first']);
    await runUntil(newRun(), () => target.rows.length === 1);
    // Rows added while no runner polls arrive after the restart; nothing is seeded again or replayed.
    sheet.appendRows(['while-stopped-1'], ['while-stopped-2']);
    await runUntil(newRun(), () => target.rows.length === 3);
    expect(target.rows).toEqual([3, 4, 5]);
    expect(await inbox.pending()).toEqual([]);
  });

  it('decodes saved bindings of the project runner', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sheets-row-added-'));
    cleanups.push(() => rm(directory, { recursive: true, force: true }));
    const path = join(directory, 'connections.json');
    const binding = { connectorId: 'google-sheets', connectionName: 'sheets-leads', triggerName: 'rowAdded', bindingName: 'new-leads', configuration: { spreadsheetId: 'sheet-1', sheetName: 'Leads' } };
    await writeFile(path, JSON.stringify({
      schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
      connections: [{
        connectorId: 'google-sheets', connectionName: 'sheets-leads', modulePath: 'github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go', provider: 'google', authMethodId: 'oauth',
        configuration: { endpoint: 'https://sheets.example.test/v4', pollInterval: 2_000_000_000, pollPageSize: 50 },
        credentials: { oauth_client_id: 'client', oauth_client_secret: 's', access_token: 'a', refresh_token: 'r' },
      }],
      triggerBindings: [binding, { ...binding, bindingName: 'bad', configuration: { sheetName: 3 } }],
    }));
    await chmod(path, 0o600);
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
    newProjectRowAddedTriggerRunner(project, 'sheets-leads', [{ bindingName: 'new-leads', target: new RecordingRowTarget() }]);
    const client = newProjectConnection(project, 'sheets-leads').operations as SpreadsheetClient;
    expect([client.pollIntervalMs, client.pollPageSize], 'Dex Web stores pollInterval in Go nanoseconds').toEqual([2000, 50]);
    expect(() => newProjectRowAddedTriggerRunner(project, 'sheets-leads', [{ bindingName: 'unsaved-binding', target: new RecordingRowTarget() }])).toThrow(/unsaved-binding/);
    let error: unknown;
    try { newProjectRowAddedTriggerRunner(project, 'sheets-leads', [{ bindingName: 'unsaved-binding', target: new RecordingRowTarget() }]); } catch (caught) { error = caught; }
    expect((error as Error).cause).toBeInstanceOf(ObjectNotFoundError);
    expect(() => newProjectRowAddedTriggerRunner(project, 'sheets-leads', [{ bindingName: 'bad', target: new RecordingRowTarget() }])).toThrow(/"bad" configuration/);
  });
});
