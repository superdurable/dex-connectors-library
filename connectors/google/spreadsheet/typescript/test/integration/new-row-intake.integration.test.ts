// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore, parseTriggerPayloadJSON, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { SpreadsheetClient } from '../../src/client.js';
import { RowAddedEventSchema, newConnection, newRowAddedTriggerRunner, type RowAddedEvent } from '../../src/index.js';
import { newAddedRowTarget } from '../../examples/new-row-intake/app.js';
import { ConnectionName, FlowIDPrefix, NewRowIntakeFlow, NewRowTriggerBinding, type Outcome } from '../../examples/new-row-intake/flow/workflow.js';
import { SheetsFake } from '../sheets_fake.js';

const key: TriggerInboxKey = { connectorId: 'google-sheets', connectionName: ConnectionName, triggerName: 'rowAdded', bindingName: NewRowTriggerBinding };
const credential = { accessToken: new SecretString('SENTINEL-ACCESS-TOKEN'), oauthClientId: 'client', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };

async function eventually(condition: () => boolean, message: string): Promise<void> {
  const deadline = Date.now() + 30_000;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error(`timed out: ${message}`);
    await new Promise(resolve => setTimeout(resolve, 50));
  }
}

it('starts one Flow per added row, which reads it back, across restarts with real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const runId = String(Date.now());
  const sheet = await SheetsFake.start('Leads', ['email', 'name', 'run'], ['existing@example.test', 'Existing', runId]);
  const records: string[] = [];
  const logger: TriggerLogger = { isEnabled: () => true, log: (level, message, attributes) => { records.push(`${level} ${message} ${JSON.stringify(attributes)}`); } };
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-sheets-row-added-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  // The Flow reads each row back through its own connection to the same fake tab.
  const readBack = new SpreadsheetClient({ endpoint: sheet.url }, { resolve: async () => credential, resolveWithRefresh: async () => credential });
  const flow = new NewRowIntakeFlow(newConnection(readBack, { provider: 'google', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  const objects = new InMemoryObjectStore();
  const scope = { projectId: 'sheets', kind: 'preview' } as const;
  const inbox = new TriggerInbox(objects, scope, key);
  const deliveries = new Map<string, number>();
  const flowTarget = newAddedRowTarget(client, flow, logger);
  const countingTarget: TriggerTarget<RowAddedEvent> = {
    async handleTrigger(context: TriggerContext, event: TriggerEvent<RowAddedEvent>) {
      await flowTarget.handleTrigger(context, event);
      // Counted only after Dex accepted the start, so the Flow exists once the test sees the event.
      deliveries.set(event.id, (deliveries.get(event.id) ?? 0) + 1);
    },
  };
  const claimed = new Set<string>();
  const startRunner = () => {
    const spreadsheet = new SpreadsheetClient({ endpoint: sheet.url, pollIntervalMs: 1000 }, { resolve: async () => credential, resolveWithRefresh: async () => credential }, { triggerLogger: logger });
    const durable = newDurableTriggerTarget(inbox, key, countingTarget, { logger, decodePayload: (_json, bytes) => RowAddedEventSchema.parse(parseTriggerPayloadJSON(bytes)) });
    const runner = newRowAddedTriggerRunner({
      connection: newConnection(spreadsheet, { provider: 'google', name: ConnectionName }),
      routes: [{ bindingName: NewRowTriggerBinding, configuration: { spreadsheetId: 'sheet-1', sheetName: 'Leads' }, cursor: newPollCursorStore(objects, scope, key), target: durable }],
    });
    const controller = new AbortController();
    const running = runner.run(newTriggerContext(controller.signal)).then(() => undefined, (error: unknown) => error);
    return async () => { controller.abort(); expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError); };
  };
  const completedRow = async (rowNumber: number): Promise<Outcome> => {
    const prefix = `row-${rowNumber}-`;
    let eventId = '';
    await eventually(() => { eventId = [...deliveries.keys()].find(id => id.startsWith(prefix) && !claimed.has(id)) ?? ''; return eventId !== ''; }, `row ${rowNumber} is delivered`);
    claimed.add(eventId);
    const result = await client.waitForFlow(FlowIDPrefix + eventId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<Outcome>();
    expect(outcome.row.eventId).toBe(eventId);
    return outcome;
  };
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const stopFirst = startRunner();
    await eventually(() => sheet.completedRequests() >= 2, 'the seed and one poll after it');
    sheet.appendRows(['ada@example.test', 'Ada', runId], [], ['grace@example.test', 'Grace', runId]);
    const ada = await completedRow(3);
    expect(ada.row).toMatchObject({ spreadsheetId: 'sheet-1', sheetName: 'Leads', rowNumber: 3, values: { email: 'ada@example.test', name: 'Ada', run: runId } });
    expect(ada.currentCells).toEqual(['ada@example.test', 'Ada', runId]);
    await completedRow(5);
    // Deleting a row above the cursor shifts every row up; the runner realigns and delivers only the new row.
    sheet.deleteRow(2);
    sheet.appendRows(['linus@example.test', 'Linus', runId]);
    const linus = await completedRow(5);
    expect(linus.row.values.email).toBe('linus@example.test');
    expect(linus.currentCells).toEqual(['linus@example.test', 'Linus', runId]);
    await stopFirst();

    sheet.appendRows(['while-stopped@example.test', 'Stopped', runId]);
    sheet.script({ status: 429, retryAfter: '1' });
    const stopSecond = startRunner();
    expect((await completedRow(6)).row.values.email).toBe('while-stopped@example.test');
    const requestsBefore = sheet.completedRequests();
    await eventually(() => sheet.completedRequests() >= requestsBefore + 2, 'two more polls');
    await stopSecond();

    expect(deliveries.size, 'existing rows start no Flow').toBe(4);
    expect([...deliveries.values()].every(count => count === 1), 'each row is delivered once and never replayed').toBe(true);
    expect(await inbox.pending()).toEqual([]);
    expect(sheet.readRanges()).toEqual(expect.arrayContaining(["'Leads'!3:3", "'Leads'!5:5"]));
    const text = records.join('\n');
    expect(text).toContain('poll cursor seeded');
    expect(text).toContain('sheet rows moved; cursor realigned');
    expect(text).toContain('poll rate limited; pausing connection');
    expect(text).not.toContain('SENTINEL');
    expect(text, 'records never contain cell values').not.toContain('@example.test');
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await sheet.close();
  }
}, 180_000);
