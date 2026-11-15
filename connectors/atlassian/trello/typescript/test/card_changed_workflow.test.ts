// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { CardChangeFlow, CardChangedTriggerBinding, ConnectionName, acceptEveryCardChange, mapToFlowInput, mapToGetCardInput, resolveFlowId, validateCardChange } from '../examples/card-changed/flow/workflow.js';
import { newConnectionWithOperations, newProjectCardTriggerRunner, type CardActionType, type CardChangedEvent } from '../src/index.js';
import type { Operations } from '../src/generated/connection.js';

function change(actionType: CardActionType = 'commentCard'): TriggerEvent<CardChangedEvent> {
  return {
    id: 'trello-action-6512f0a1c2d3e4f5a6b74001', occurredAt: new Date('2026-11-02T09:00:00Z'),
    payload: { boardId: '6512f0a1c2d3e4f5a6b70001', actionId: '6512f0a1c2d3e4f5a6b74001', actionType, occurredAt: '2026-11-02T09:00:00Z', card: { id: '6512f0a1c2d3e4f5a6b71001', name: 'Door sensor' } },
  };
}

it('maps each card action to one Flow and reads the card back', () => {
  const event = change();
  expect(acceptEveryCardChange()).toBe(true);
  expect(resolveFlowId(event)).toBe('trello-card-change-trello-action-6512f0a1c2d3e4f5a6b74001');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, change: event.payload });
  expect(mapToGetCardInput(input)).toEqual({ cardId: '6512f0a1c2d3e4f5a6b71001' });
  expect(validateCardChange(input)).toBe('');
  expect(validateCardChange({ ...input, triggerEventId: ' ' })).toContain('triggerEventId');
  expect(validateCardChange({ ...input, change: { ...input.change, card: { id: '' } } })).toContain('card.id');
  expect(validateCardChange({ ...input, change: { ...input.change, actionType: 'updateBoard' as CardActionType } })).toContain('actionType');
});

it('declares the cardChanged binding with its required board input', () => {
  const flow = new CardChangeFlow(newConnectionWithOperations({} as Operations, { provider: 'trello', name: ConnectionName }));
  expect(flow.getFlowType()).toBe('TrelloCardChangeTypeScript');
  const [binding] = flow.getConnectorTriggerBindings();
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: CardChangedTriggerBinding, definition: { trigger: { connectorId: 'trello', triggerName: 'cardChanged' } } });
  const [unit] = binding!.configurationUI!.units;
  expect(unit).toMatchObject({ id: 'watchedBoard', unitId: 'boardInput', label: 'Watched board', required: true, bindings: [{ port: 'boardId', jsonPointer: '/boardId' }] });
  expect(unit!.description).toContain('https://trello.com/b/<short link>/<board name>');
  expect(unit!.description).toContain('leaving it blank stops the Worker');
  expect(unit!.description).toContain('records a new baseline');
});

function project(bindings: Record<string, Record<string, unknown>>, pollInterval: unknown = 15_000_000_000): LoadedProject {
  return new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: { projectId: 'trello-example', kind: 'live' }, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'trello', connectionName: ConnectionName, modulePath: 'trello', provider: 'trello', configuration: { pollInterval } }],
    triggerBindings: Object.entries(bindings).map(([bindingName, configuration]) => ({ connectorId: 'trello', connectionName: ConnectionName, triggerName: 'cardChanged', bindingName, configuration })),
  }, undefined, new InMemoryObjectStore(), undefined, undefined);
}

it('opens saved bindings and rejects unsaved boards, bad routes, and bad configurations', () => {
  const target = { handleTrigger: async () => undefined };
  expect(newProjectCardTriggerRunner(project({ saved: { boardId: 'AbCd1234' } }), ConnectionName, [{ bindingName: 'saved', target }])).toBeDefined();
  expect(newProjectCardTriggerRunner(project({ saved: { boardId: 'AbCd1234' } }, '1m30s'), ConnectionName, [{ bindingName: 'saved', target }])).toBeDefined();
  expect(() => newProjectCardTriggerRunner(project({}), ConnectionName, [{ bindingName: 'unsaved', target }])).toThrow('save the watched board');
  expect(() => newProjectCardTriggerRunner(project({}), ConnectionName, [])).toThrow('routes are required');
  expect(() => newProjectCardTriggerRunner(project({}), ConnectionName, [{ bindingName: ' ', target }])).toThrow('binding name and target');
  expect(() => newProjectCardTriggerRunner(project({ a: { boardId: 'AbCd1234' } }), ConnectionName, [{ bindingName: 'a', target }, { bindingName: 'a', target }])).toThrow('duplicated');
  expect(() => newProjectCardTriggerRunner(project({ bad: { boardId: 'x' } }), ConnectionName, [{ bindingName: 'bad', target }])).toThrow('boardId must be');
  expect(() => newProjectCardTriggerRunner(project({ typo: { board: 'AbCd1234' } }), ConnectionName, [{ bindingName: 'typo', target }])).toThrow('typo');
  expect(() => newProjectCardTriggerRunner(project({ saved: { boardId: 'AbCd1234' } }, 9_000_000_000), ConnectionName, [{ bindingName: 'saved', target }])).toThrow('at least 10s');
});
