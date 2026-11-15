// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry } from '@superdurable/dex';
import { SecretString, type TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { Client, newConnection, type TaskChangedEvent } from '../src/index.js';
import {
  ConnectionName, TaskChangedFlow, TaskChangedTriggerBinding, mapToFlowInput, mapToGetTaskInput, resolveFlowId, validateTaskChangeInput,
} from '../examples/task-changed/flow/workflow.js';

const change: TaskChangedEvent = {
  projectId: '1201000000000001', action: 'changed', task: { id: '1204000000000001', name: 'Inspect door sensors' },
  changedField: 'completed', changeAction: 'changed', occurredAt: '2026-03-02T09:00:01Z',
};
const event: TriggerEvent<TaskChangedEvent> = { id: 'asana-94ef95efc2e9cbae0b7708d10be4af34', occurredAt: new Date('2026-03-02T09:00:01Z'), payload: change };

function newFlow(): TaskChangedFlow {
  const client = new Client({}, { resolve: async () => ({ accessToken: new SecretString('asana-fake-token') }) }, { send: async () => { throw new Error('no request'); } });
  return new TaskChangedFlow(newConnection(client, { provider: 'asana', name: ConnectionName }));
}

it('names one Flow per event and reads its task', () => {
  expect(resolveFlowId(event)).toBe('asana-task-change-asana-94ef95efc2e9cbae0b7708d10be4af34');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, change });
  expect(mapToGetTaskInput(input)).toEqual({ taskId: '1204000000000001' });
  expect(validateTaskChangeInput(input)).toBe('');
});

it('refuses invalid deliveries', () => {
  expect(validateTaskChangeInput({ triggerEventId: ' ', change })).not.toBe('');
  expect(validateTaskChangeInput({ triggerEventId: 'e', change: { ...change, action: 'moved' } })).toContain('added, changed');
  expect(validateTaskChangeInput({ triggerEventId: 'e', change: { ...change, task: { id: '' } } })).toContain('task.id');
});

it('registers and declares the taskChanged binding with its project picker', () => {
  const flow = newFlow();
  expect(() => new Registry([flow])).not.toThrow();
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding!.connectionName, binding!.bindingName, binding!.definition.trigger.triggerName]).toEqual([ConnectionName, TaskChangedTriggerBinding, 'taskChanged']);
  const [unit] = binding!.configurationUI!.units;
  expect([unit!.unitId, unit!.required]).toEqual(['projectPicker', true]);
  expect(unit!.bindings.map(item => [item.port, item.jsonPointer])).toEqual([['workspaceId', '/workspaceId'], ['projectId', '/projectId'], ['projectName', '/projectName']]);
  expect(unit!.description).toContain('/project/');
  expect(unit!.description).toContain('baseline');
});
