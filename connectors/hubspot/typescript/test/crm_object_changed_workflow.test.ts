// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { newConnectionWithOperations, newProjectCRMObjectTriggerRunner, type CRMObjectChangedEvent } from '../src/index.js';
import type { Operations } from '../src/generated/connection.js';
import {
  CRMObjectChangeFlow, CRMObjectChangedTriggerBinding, ConnectionName, DeliveredProperties, acceptEveryCRMObjectChange, mapToFlowInput, mapToGetObjectInput, resolveFlowId,
} from '../examples/crm-object-changed/flow/workflow.js';

const change: TriggerEvent<CRMObjectChangedEvent> = {
  id: 'hubspot-38f57040bd071dc31d72af8e1d80ac42', occurredAt: new Date('2026-10-25T10:00:02Z'),
  payload: { objectType: 'deals', changeKind: 'created', modifiedAt: '2026-10-25T10:00:02Z', object: { objectType: 'deals', id: '1002', properties: { dealname: 'New' }, archived: false } },
};

it('maps each change to one Flow that reads the record back', () => {
  expect(acceptEveryCRMObjectChange()).toBe(true);
  expect(resolveFlowId(change)).toBe('hubspot-crm-object-change-hubspot-38f57040bd071dc31d72af8e1d80ac42');
  const input = mapToFlowInput(change);
  expect(input).toEqual({ triggerEventId: change.id, change: change.payload });
  expect(mapToGetObjectInput(input)).toEqual({ objectType: 'deals', objectId: '1002', properties: DeliveredProperties });
});

it('declares the crm-object-changes binding with its required object type picker', () => {
  const flow = new CRMObjectChangeFlow(newConnectionWithOperations({} as Operations, { provider: 'hubspot', name: ConnectionName }));
  const [binding] = flow.getConnectorTriggerBindings();
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: CRMObjectChangedTriggerBinding, definition: { trigger: { connectorId: 'hubspot', triggerName: 'crmObjectChanged' } } });
  expect(binding!.configurationUI!.units).toEqual([expect.objectContaining({ unitId: 'objectTypePicker', required: true, bindings: [{ port: 'objectType', jsonPointer: '/objectType' }] })]);
  expect(binding!.configurationUI!.units[0]!.description).toMatch(/crm\.objects\.<type>\.read/);
});

it('refuses a binding without a saved object type or with an unsupported one', () => {
  const project = (bindings: Array<Record<string, unknown>>, settings: Record<string, unknown> = {}) => new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: { projectId: 'hubspot-unit', kind: 'live' }, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'hubspot', connectionName: ConnectionName, modulePath: 'hubspot', provider: 'hubspot', configuration: settings }],
    triggerBindings: bindings.map(configuration => ({ connectorId: 'hubspot', connectionName: ConnectionName, triggerName: 'crmObjectChanged', bindingName: CRMObjectChangedTriggerBinding, configuration })),
  }, undefined, new InMemoryObjectStore(), undefined, undefined);
  const routes = { crmObjectChangedRoutes: [{ bindingName: CRMObjectChangedTriggerBinding, target: { handleTrigger: async () => undefined } }] };
  expect(() => newProjectCRMObjectTriggerRunner(project([], { pollInterval: 4_999_999_999 }), ConnectionName, routes), 'a poll interval under 5s is refused').toThrow('at least 5s');
  const valid = (bindings: Array<Record<string, unknown>>) => project(bindings, { pollInterval: 5_000_000_000 });
  expect(() => newProjectCRMObjectTriggerRunner(valid([]), ConnectionName, routes)).toThrow('has no saved object type; pick one in Dex Web');
  expect(() => newProjectCRMObjectTriggerRunner(valid([{ objectType: 'tickets' }]), ConnectionName, routes)).toThrow('object type must be contacts, companies, or deals');
  expect(() => newProjectCRMObjectTriggerRunner(valid([{ objectType: 'deals', unknown: true }]), ConnectionName, routes)).toThrow('configuration');
  expect(newProjectCRMObjectTriggerRunner(valid([{ objectType: 'deals' }]), ConnectionName, routes)).toBeDefined();
});
