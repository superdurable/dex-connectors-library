// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { ConnectionName, MaxRecordedBodyCharacters, PageChangeFlow, PageChangedTriggerBinding, acceptEveryPageChange, mapToFlowInput, mapToGetPageInput, resolveFlowId, validatePageChange } from '../examples/page-changed/flow/workflow.js';
import { newConnectionWithOperations, newProjectPageTriggerRunner, type PageChangedEvent } from '../src/index.js';
import type { Operations } from '../src/generated/connection.js';
import { CloudID } from './confluence_page_fake.js';

function change(changeKind: 'created' | 'updated' = 'created', versionNumber = 1): TriggerEvent<PageChangedEvent> {
  return { id: 'confluence-page-0123', occurredAt: new Date('2026-11-02T09:00:00Z'), payload: { cloudId: CloudID, changeKind, page: { id: '600001', title: 'Remote work policy', spaceKey: 'OPS', versionNumber, lastModifiedAt: '2026-11-02T09:00:00Z' } } };
}

it('maps each page version to one Flow and reads the page back as bounded Markdown', () => {
  const event = change();
  expect(acceptEveryPageChange()).toBe(true);
  expect(resolveFlowId(event)).toBe('confluence-page-change-confluence-page-0123');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: 'confluence-page-0123', change: event.payload });
  expect(mapToGetPageInput(input)).toEqual({ pageId: '600001', maxBodyCharacters: MaxRecordedBodyCharacters });
  expect(validatePageChange(input)).toBe('');
  expect(validatePageChange({ ...input, triggerEventId: ' ' })).toContain('triggerEventId');
  expect(validatePageChange(mapToFlowInput(change('created', 0)))).toContain('versionNumber');
  expect(validatePageChange({ ...input, change: { ...input.change, changeKind: 'deleted' as 'created' } })).toContain('changeKind');
});

it('declares the pageChanged binding with its space picker', () => {
  const flow = new PageChangeFlow(newConnectionWithOperations({} as Operations, { provider: 'atlassian', name: ConnectionName }));
  expect(flow.getFlowType()).toBe('ConfluencePageChangeTypeScript');
  const [binding] = flow.getConnectorTriggerBindings();
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: PageChangedTriggerBinding, definition: { trigger: { connectorId: 'confluence', triggerName: 'pageChanged' } } });
  const [unit] = binding!.configurationUI!.units;
  expect(unit).toMatchObject({ unitId: 'spacePicker', label: 'Watched space', required: false, bindings: [
    { port: 'spaceId', jsonPointer: '/spaceId' }, { port: 'spaceKey', jsonPointer: '/spaceKey' }, { port: 'spaceName', jsonPointer: '/spaceName' },
  ] });
  expect(unit!.description).toMatch(/Leave it unsaved to watch every space/);
});

function project(bindings: Record<string, Record<string, unknown>>, pollInterval: unknown = 30_000_000_000): LoadedProject {
  return new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: { projectId: 'confluence-example', kind: 'live' }, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'confluence', connectionName: ConnectionName, modulePath: 'confluence', provider: 'atlassian', configuration: { cloudId: CloudID, pollInterval } }],
    triggerBindings: Object.entries(bindings).map(([bindingName, configuration]) => ({ connectorId: 'confluence', connectionName: ConnectionName, triggerName: 'pageChanged', bindingName, configuration })),
  }, undefined, new InMemoryObjectStore(), undefined, undefined);
}

it('opens saved and unsaved bindings and rejects bad routes and configurations', () => {
  const target = { handleTrigger: async () => undefined };
  expect(newProjectPageTriggerRunner(project({ saved: { spaceId: '98306', spaceKey: 'OPS', spaceName: 'Operations' } }), ConnectionName, [{ bindingName: 'saved', target }, { bindingName: 'unsaved', target }])).toBeDefined();
  expect(newProjectPageTriggerRunner(project({}, '1m30s'), ConnectionName, [{ bindingName: 'unsaved', target }])).toBeDefined();
  expect(() => newProjectPageTriggerRunner(project({}), ConnectionName, [])).toThrow('routes are required');
  expect(() => newProjectPageTriggerRunner(project({}), ConnectionName, [{ bindingName: ' ', target }])).toThrow('binding name and target');
  expect(() => newProjectPageTriggerRunner(project({}), ConnectionName, [{ bindingName: 'a', target }, { bindingName: 'a', target }])).toThrow('duplicated');
  expect(() => newProjectPageTriggerRunner(project({ saved: { spaceKey: 'bad key' } }), ConnectionName, [{ bindingName: 'saved', target }])).toThrow('space key');
  expect(() => newProjectPageTriggerRunner(project({ typo: { space: 'OPS' } }), ConnectionName, [{ bindingName: 'typo', target }])).toThrow('typo');
  expect(() => newProjectPageTriggerRunner(project({}, 5_000_000_000), ConnectionName, [{ bindingName: 'a', target }])).toThrow('at least 10s');
});
