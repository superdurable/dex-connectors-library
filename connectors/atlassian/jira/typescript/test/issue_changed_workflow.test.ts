// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { ConnectionName, IssueChangeFlow, IssueChangedTriggerBinding, acceptEveryIssueChange, mapToFlowInput, mapToGetIssueInput, resolveFlowId, validateIssueChange } from '../examples/issue-changed/flow/workflow.js';
import { newConnectionWithOperations, newProjectIssueTriggerRunner, type IssueChangedEvent } from '../src/index.js';
import type { Operations } from '../src/generated/connection.js';
import { CloudID } from './jira_search_fake.js';

const zeroTime = '0001-01-01T00:00:00Z';
function change(changeKind: 'created' | 'updated' = 'created'): TriggerEvent<IssueChangedEvent> {
  return { id: 'jira-0123', occurredAt: new Date('2026-11-02T09:00:00Z'), payload: { cloudId: CloudID, changeKind, issue: { id: '20001', key: 'OPS-1', summary: 'Kickoff', status: { id: '1', name: 'To Do' }, issueType: { id: '10001', name: 'Task' }, project: { id: '10000', key: 'OPS' }, createdAt: zeroTime, updatedAt: zeroTime } } };
}

it('maps each issue version to one Flow and reads the issue by its stable ID', () => {
  const event = change();
  expect(acceptEveryIssueChange()).toBe(true);
  expect(resolveFlowId(event)).toBe('jira-issue-change-jira-0123');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: 'jira-0123', change: event.payload });
  expect(mapToGetIssueInput(input)).toEqual({ issueIdOrKey: '20001' });
  expect(validateIssueChange(input)).toBe('');
  expect(validateIssueChange({ ...input, triggerEventId: ' ' })).toContain('triggerEventId');
  expect(validateIssueChange({ ...input, change: { ...input.change, changeKind: 'deleted' as 'created' } })).toContain('changeKind');
});

it('declares the issueChanged binding with its project picker', () => {
  const flow = new IssueChangeFlow(newConnectionWithOperations({} as Operations, { provider: 'atlassian', name: ConnectionName }));
  expect(flow.getFlowType()).toBe('JiraIssueChangeTypeScript');
  const [binding] = flow.getConnectorTriggerBindings();
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: IssueChangedTriggerBinding, definition: { trigger: { connectorId: 'jira', triggerName: 'issueChanged' } } });
  const [unit] = binding!.configurationUI!.units;
  expect(unit).toMatchObject({ unitId: 'projectPicker', label: 'Watched project', required: false, bindings: [
    { port: 'projectId', jsonPointer: '/projectId' }, { port: 'projectKey', jsonPointer: '/projectKey' }, { port: 'projectName', jsonPointer: '/projectName' },
  ] });
  expect(unit!.description).toMatch(/Leave it unsaved to watch every project/);
});

function project(bindings: Record<string, Record<string, unknown>>, pollInterval: unknown = 15_000_000_000): LoadedProject {
  return new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: { projectId: 'jira-example', kind: 'live' }, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'jira', connectionName: ConnectionName, modulePath: 'jira', provider: 'atlassian', configuration: { cloudId: CloudID, pollInterval } }],
    triggerBindings: Object.entries(bindings).map(([bindingName, configuration]) => ({ connectorId: 'jira', connectionName: ConnectionName, triggerName: 'issueChanged', bindingName, configuration })),
  }, undefined, new InMemoryObjectStore(), undefined, undefined);
}

it('opens saved and unsaved bindings and rejects bad routes and configurations', () => {
  const target = { handleTrigger: async () => undefined };
  expect(newProjectIssueTriggerRunner(project({ saved: { projectId: '10000', projectKey: 'OPS', projectName: 'Operations' } }), ConnectionName, [{ bindingName: 'saved', target }, { bindingName: 'unsaved', target }])).toBeDefined();
  expect(newProjectIssueTriggerRunner(project({}, '1m30s'), ConnectionName, [{ bindingName: 'unsaved', target }])).toBeDefined();
  expect(() => newProjectIssueTriggerRunner(project({}), ConnectionName, [])).toThrow('routes are required');
  expect(() => newProjectIssueTriggerRunner(project({}), ConnectionName, [{ bindingName: ' ', target }])).toThrow('binding name and target');
  expect(() => newProjectIssueTriggerRunner(project({}), ConnectionName, [{ bindingName: 'a', target }, { bindingName: 'a', target }])).toThrow('duplicated');
  expect(() => newProjectIssueTriggerRunner(project({ saved: { projectKey: 'ops' } }), ConnectionName, [{ bindingName: 'saved', target }])).toThrow('uppercase');
  expect(() => newProjectIssueTriggerRunner(project({ typo: { project: 'OPS' } }), ConnectionName, [{ bindingName: 'typo', target }])).toThrow('typo');
  expect(() => newProjectIssueTriggerRunner(project({}, 4_000_000_000), ConnectionName, [{ bindingName: 'a', target }])).toThrow('at least 5s');
});
