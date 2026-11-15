// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { newProjectRecordTriggerRunner, type RecordModifiedEvent } from '../src/index.js';
import { newConnectionWithOperations, type Operations } from '../src/generated/connection.js';
import {
  ConnectionName, RecordChangeFlow, RecordModifiedTriggerBinding, acceptEveryRecordChange, mapToFlowInput, mapToGetRecordInput, resolveFlowId,
} from '../examples/record-changes/flow/workflow.js';

const recordId = '006RM0000000002AAA';
const change: TriggerEvent<RecordModifiedEvent> = {
  id: 'sfdc-0c1a7c970b998bb6d0b3ed38dc0f74e7', occurredAt: new Date('2026-03-02T09:00:02Z'),
  payload: {
    sObjectType: 'Opportunity', recordId, changeKind: 'created', systemModstamp: '2026-03-02T09:00:02Z',
    record: { type: 'Opportunity', id: recordId, fields: { StageName: 'Prospecting', Id: recordId, Name: 'New' } },
  },
};

it('maps each change to one Flow that reads the record back', () => {
  expect(acceptEveryRecordChange()).toBe(true);
  expect(resolveFlowId(change)).toBe('salesforce-record-change-sfdc-0c1a7c970b998bb6d0b3ed38dc0f74e7');
  const input = mapToFlowInput(change);
  expect(input).toEqual({ triggerEventId: change.id, change: change.payload });
  expect(mapToGetRecordInput(input)).toEqual({ sObjectType: 'Opportunity', recordId, fields: ['Id', 'Name', 'StageName'] });
});

it('declares the record-changes binding with its object picker and field list', () => {
  const flow = new RecordChangeFlow(newConnectionWithOperations({} as Operations, { provider: 'salesforce', name: ConnectionName }));
  const [binding] = flow.getConnectorTriggerBindings();
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: RecordModifiedTriggerBinding, definition: { trigger: { connectorId: 'salesforce', triggerName: 'recordModifiedPolled' } } });
  const [picker, fields] = binding!.configurationUI!.units;
  expect(picker).toMatchObject({ unitId: 'sObjectPicker', required: true, bindings: [{ port: 'sObjectType', jsonPointer: '/sObjectType' }] });
  expect(picker!.description).toMatch(/https:\/\/login\.salesforce\.com.*baseline/);
  expect(fields).toMatchObject({ unitId: 'fieldNamesInput', required: false, bindings: [{ port: 'fieldNames', jsonPointer: '/fieldNames' }] });
  expect(fields!.description).toMatch(/Fields & Relationships.*Blank delivers only/);
});

it('refuses invalid saved bindings and poll settings', () => {
  const project = (bindings: Array<Record<string, unknown>>, settings: Record<string, unknown> = {}) => new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: { projectId: 'salesforce-unit', kind: 'live' }, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'salesforce', connectionName: ConnectionName, modulePath: 'salesforce', provider: 'salesforce', configuration: settings }],
    triggerBindings: bindings.map(configuration => ({ connectorId: 'salesforce', connectionName: ConnectionName, triggerName: 'recordModifiedPolled', bindingName: RecordModifiedTriggerBinding, configuration })),
  }, undefined, new InMemoryObjectStore(), undefined, undefined);
  const routes = { recordModifiedPolledRoutes: [{ bindingName: RecordModifiedTriggerBinding, target: { handleTrigger: async () => undefined } }] };
  expect(() => newProjectRecordTriggerRunner(project([], { pollInterval: 4_999_999_999 }), ConnectionName, routes)).toThrow('at least 5s');
  expect(() => newProjectRecordTriggerRunner(project([], { pollPageSize: 0 }), ConnectionName, routes)).toThrow('pollPageSize must be from 1 to 500');
  expect(() => newProjectRecordTriggerRunner(project([]), ConnectionName, routes)).toThrow('has no saved object; choose one in Dex Web Connections');
  expect(() => newProjectRecordTriggerRunner(project([{ sObjectType: 'Bad Name' }]), ConnectionName, routes)).toThrow('sObjectType must be an object API name');
  expect(() => newProjectRecordTriggerRunner(project([{ sObjectType: 'Contact', fieldNames: ['Account.Name'] }]), ConnectionName, routes)).toThrow('without relationship dots');
  expect(() => newProjectRecordTriggerRunner(project([{ sObjectType: 'Contact', fieldNames: 'Name' }]), ConnectionName, routes)).toThrow('configuration');
  expect(() => newProjectRecordTriggerRunner(project([{ sObjectType: 'Contact', unknown: true }]), ConnectionName, routes)).toThrow('configuration');
  expect(newProjectRecordTriggerRunner(project([{ sObjectType: 'Opportunity', fieldNames: ['StageName'] }], { pollInterval: 5_000_000_000, pollPageSize: 2 }), ConnectionName, routes)).toBeDefined();
});
