// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry } from '@superdurable/dex';
import { SecretString, type TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { NotionClient, newConnection, type DatabaseRowChangedEvent } from '../src/index.js';
import {
  ConnectionName, DatabaseRowChangeFlow, DatabaseRowChangedTriggerBinding, mapToFlowInput, mapToGetPageInput, resolveFlowId, validateRowChangeInput,
} from '../examples/row-changed/flow/workflow.js';

const change: DatabaseRowChangedEvent = {
  dataSourceId: '2a4d6f80-1b3c-4d5e-8f70-112233445566', changeKind: 'created',
  row: { id: '2a4d6f80-1b3c-4d5e-8f70-0000000000a1', title: 'Standup', parent: { type: 'data_source_id' }, inTrash: false, archived: false, properties: {} },
};
const event: TriggerEvent<DatabaseRowChangedEvent> = { id: 'notion-row-e0e716fa48c2158e8ccee9565192273e', occurredAt: new Date('2026-11-02T09:31:00Z'), payload: change };

function newFlow(): DatabaseRowChangeFlow {
  const client = new NotionClient({}, { resolve: async () => ({ apiToken: new SecretString('ntn_fake_token') }) }, { send: async () => { throw new Error('no request'); } });
  return new DatabaseRowChangeFlow(newConnection(client, { provider: 'notion', name: ConnectionName }));
}

it('names one Flow per row version and reads its row', () => {
  expect(resolveFlowId(event)).toBe('notion-row-change-notion-row-e0e716fa48c2158e8ccee9565192273e');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, change });
  expect(mapToGetPageInput(input)).toEqual({ pageId: change.row.id, skipContent: true });
  expect(validateRowChangeInput(input)).toBe('');
});

it('refuses invalid deliveries', () => {
  expect(validateRowChangeInput({ triggerEventId: ' ', change })).not.toBe('');
  expect(validateRowChangeInput({ triggerEventId: 'e', change: { ...change, changeKind: 'moved' } })).toContain('created or updated');
  expect(validateRowChangeInput({ triggerEventId: 'e', change: { ...change, row: { ...change.row, id: '' } } })).toContain('row.id');
});

it('registers and declares the databaseRowChanged binding with its data source picker', () => {
  const flow = newFlow();
  expect(() => new Registry([flow])).not.toThrow();
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding!.connectionName, binding!.bindingName, binding!.definition.trigger.triggerName]).toEqual([ConnectionName, DatabaseRowChangedTriggerBinding, 'databaseRowChanged']);
  const [unit] = binding!.configurationUI!.units;
  expect([unit!.unitId, unit!.required]).toEqual(['dataSourcePicker', true]);
  expect(unit!.bindings.map(item => [item.port, item.jsonPointer])).toEqual([['dataSourceId', '/dataSourceId'], ['dataSourceName', '/dataSourceName']]);
  expect(unit!.description).toContain('https://app.notion.com');
  expect(unit!.description).toContain('baseline');
});
