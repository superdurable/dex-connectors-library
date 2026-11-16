// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry } from '@superdurable/dex';
import { SecretString, type TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { AirtableClient, UIUnitBasePicker, UIUnitLastModifiedTimeFieldPicker, UIUnitTablePicker, newConnection, type Connection, type RecordChangedEvent } from '../src/index.js';
import {
  ConnectionName, FlowType, RecordChangedIntakeFlow, RecordChangedTriggerBinding, acceptChangedRecord, completeWithCurrentRecord, mapToFlowInput,
  mapToReadBackInput, resolveFlowId,
} from '../examples/record-changed-intake/flow/workflow.js';

/** A connection whose placeholder token is never sent: these tests build the Flow without calling Airtable. */
function intakeConnection(name = ConnectionName): Connection {
  const client = new AirtableClient({}, { resolve: async () => ({ personalAccessToken: new SecretString('placeholder') }) });
  return newConnection(client, { provider: 'airtable', name });
}

const change: RecordChangedEvent = {
  baseId: 'appIntakeBase0001', tableId: 'tblIntakeLeads001', lastModifiedTime: '2026-11-15T09:00:00.123Z', isCreatedRecord: true,
  record: { id: 'recNewLead0000001', createdTime: '2026-11-15T09:00:00.123Z', fields: { Email: 'lead@example.test' } },
};
const event: TriggerEvent<RecordChangedEvent> = { id: 'recNewLead0000001-20261115T090000123000000Z', occurredAt: new Date('2026-11-15T09:00:00.123Z'), payload: change };

it('names one Flow per change and carries the record', () => {
  expect(acceptChangedRecord()).toBe(true);
  expect(resolveFlowId(event)).toBe('airtable-record-change-recNewLead0000001-20261115T090000123000000Z');
  expect(mapToFlowInput(event)).toEqual({
    eventId: event.id, baseId: 'appIntakeBase0001', tableId: 'tblIntakeLeads001', recordId: 'recNewLead0000001',
    isCreatedRecord: true, lastModifiedTime: '2026-11-15T09:00:00.123Z', fields: { Email: 'lead@example.test' },
  });
});

it('registers and declares the binding with its three pickers', () => {
  const flow = new RecordChangedIntakeFlow(intakeConnection());
  expect(flow.getFlowType()).toBe(FlowType);
  expect(() => new Registry([flow])).not.toThrow();
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding!.connectionName, binding!.bindingName, binding!.definition.trigger.triggerName]).toEqual([ConnectionName, RecordChangedTriggerBinding, 'recordChanged']);
  const units = binding!.configurationUI!.units;
  expect(units.map(unit => [unit.unitId, unit.required])).toEqual([[UIUnitBasePicker, true], [UIUnitTablePicker, true], [UIUnitLastModifiedTimeFieldPicker, true]]);
  expect(units[2]!.bindings.map(item => [item.port, item.jsonPointer])).toEqual([['baseId', '/baseId'], ['tableId', '/tableId'], ['fieldId', '/fieldId'], ['fieldName', '/fieldName']]);
  expect(units[0]!.description).toContain('https://airtable.com/');
  expect(units[1]!.description).toContain('never start Flows');
  expect(units[2]!.description).toContain('Include time');
});

it('reads the changed record back and keeps its current fields', () => {
  const change = mapToFlowInput(event);
  expect(mapToReadBackInput(change)).toEqual({ baseId: 'appIntakeBase0001', tableIdOrName: 'tblIntakeLeads001', recordId: 'recNewLead0000001' });
  const current = { id: 'recNewLead0000001', createdTime: change.lastModifiedTime, fields: { Email: 'lead@example.test', Status: 'New' } };
  expect(completeWithCurrentRecord({ change, isRemoved: false, isCompleted: false }, current)).toEqual({
    change, currentFields: { Email: 'lead@example.test', Status: 'New' }, isRemoved: false, isCompleted: true,
  });
  expect(() => completeWithCurrentRecord({ change, isRemoved: false, isCompleted: false }, { ...current, id: 'recOtherLead00001' })).toThrow('instead of the changed record');
});

it('requires the polled connection', () => {
  expect(() => new RecordChangedIntakeFlow(intakeConnection('airtable-other'))).toThrow('airtable-intake');
});
