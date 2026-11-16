// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { SecretString } from '@superdurable/connectors-sdk';
import { SpreadsheetClient } from '../src/client.js';
import { RowAddedTriggerDefinition, newConnection, type RowAddedEvent } from '../src/index.js';
import { ConnectionName, NewRowIntakeFlow, NewRowTriggerBinding, acceptAddedRow, mapToFlowInput, mapToGetValuesInput, resolveFlowId } from '../examples/new-row-intake/flow/workflow.js';

const detectedAt = new Date('2026-11-15T09:00:00Z');
const event: TriggerEvent<RowAddedEvent> = {
  id: 'row-12-0f3a9c1d2b4e5f60', occurredAt: detectedAt,
  payload: { spreadsheetId: 'sheet-1', sheetName: 'Leads', rowNumber: 12, values: { email: 'ada@example.test' }, cells: ['ada@example.test'] },
};

const credential = { accessToken: new SecretString('token'), oauthClientId: 'client', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };
// A connection that is never called: these tests only build the Flow.
const connection = newConnection(new SpreadsheetClient({}, { resolve: async () => credential, resolveWithRefresh: async () => credential }), { provider: 'google', name: ConnectionName });

describe('new-row-intake example', () => {
  it('declares one static binding with picker units and full guidance', () => {
    const [binding, ...others] = new NewRowIntakeFlow(connection).getConnectorTriggerBindings();
    expect(others).toEqual([]);
    expect([binding!.connectionName, binding!.bindingName, binding!.definition.trigger]).toEqual([ConnectionName, NewRowTriggerBinding, RowAddedTriggerDefinition.trigger]);
    const [spreadsheet, tab] = binding!.configurationUI!.units;
    expect([spreadsheet!.id, spreadsheet!.unitId, spreadsheet!.label, spreadsheet!.required]).toEqual(['spreadsheet', 'spreadsheetPicker', 'Spreadsheet', true]);
    expect(spreadsheet!.bindings).toEqual([{ port: 'spreadsheetId', jsonPointer: '/spreadsheetId' }]);
    expect(spreadsheet!.description).toContain('https://docs.google.com/spreadsheets/d/');
    expect(spreadsheet!.description).toContain('Google Picker');
    expect(spreadsheet!.description).toContain('cannot be blank');
    expect([tab!.id, tab!.unitId, tab!.label, tab!.required]).toEqual(['tab', 'sheetTabPicker', 'Tab', true]);
    expect(tab!.bindings).toEqual([{ port: 'spreadsheetId', jsonPointer: '/spreadsheetId' }, { port: 'tab', jsonPointer: '/sheetName' }]);
    expect(tab!.description).toContain('Row 1 must hold the column headers');
    expect(tab!.description).toContain('cannot be blank');
  });

  it('maps every row to one Flow per event ID', () => {
    expect(acceptAddedRow(event)).toBe(true);
    expect(resolveFlowId(event)).toBe('google-sheets-row-row-12-0f3a9c1d2b4e5f60');
    expect(mapToFlowInput(event)).toEqual({ eventId: event.id, spreadsheetId: 'sheet-1', sheetName: 'Leads', rowNumber: 12, values: { email: 'ada@example.test' }, detectedAt: '2026-11-15T09:00:00.000Z' });
  });

  it('reads the added row back through getValues', () => {
    expect(() => new NewRowIntakeFlow(connection).getSteps()).not.toThrow();
    expect(mapToGetValuesInput(mapToFlowInput(event))).toEqual({ spreadsheetId: 'sheet-1', range: "'Leads'!12:12" });
    expect(mapToGetValuesInput({ ...mapToFlowInput(event), sheetName: "Bob's leads", rowNumber: 7 }).range).toBe("'Bob''s leads'!7:7");
  });
});
