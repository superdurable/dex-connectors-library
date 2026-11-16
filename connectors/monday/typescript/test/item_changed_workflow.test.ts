// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Deterministic checks of the item-changed example's Flow wiring, binding, and mappers.
import { Registry } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import {
  ConnectionName, ItemChangeFlow, ItemChangedTriggerBinding, acceptEveryItemChange, mapToFlowInput, mapToGetItemInput, resolveFlowId, validateItemChangeInput,
} from '../examples/item-changed/flow.js';
import { newConnection } from '../src/index.js';
import { MondayBoardFake, newFakeClient } from './monday_board_fake.js';

const event = (changeKind: string, itemId = '5550001') => ({
  id: 'monday-item-0123', occurredAt: new Date('2026-03-02T09:00:00Z'), payload: { boardId: '1234567890', changeKind, item: { id: itemId, name: 'Request' } },
}) as Parameters<typeof mapToFlowInput>[0];
const connectionNamed = (name: string) => newConnection(newFakeClient(new MondayBoardFake('1234567890').send), { provider: 'monday', name });

describe('item-changed example', () => {
  it('names one Flow per change and reads the changed item', () => {
    expect(resolveFlowId(event('updated'))).toBe('monday-item-change-monday-item-0123');
    const input = mapToFlowInput(event('updated'));
    expect(input).toEqual({ triggerEventId: 'monday-item-0123', change: event('updated').payload });
    expect(mapToGetItemInput(input)).toEqual({ itemId: '5550001' });
    expect(acceptEveryItemChange()).toBe(true);
  });
  it('refuses an unusable change', () => {
    expect(validateItemChangeInput(mapToFlowInput(event('created')))).toBe('');
    expect(validateItemChangeInput({ ...mapToFlowInput(event('updated')), triggerEventId: ' ' })).toContain('triggerEventId');
    expect(validateItemChangeInput(mapToFlowInput(event('updated', '')))).toContain('item.id');
    expect(validateItemChangeInput(mapToFlowInput(event('archived')))).toContain('changeKind');
  });
  it('registers and declares its Trigger binding and board field', () => {
    const flow = new ItemChangeFlow(connectionNamed(ConnectionName));
    expect(() => new Registry([flow])).not.toThrow();
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding?.connectionName, binding?.bindingName, binding?.definition.trigger.triggerName]).toEqual([ConnectionName, ItemChangedTriggerBinding, 'itemChanged']);
    const [unit] = binding?.configurationUI?.units ?? [];
    expect([unit?.unitId, unit?.required, unit?.bindings]).toEqual(['boardIdInput', true, [{ port: 'boardId', jsonPointer: '/boardId' }]]);
    for (const guidance of ['https://monday.com/', '/boards/', 'not secret', 'Blank saves nothing']) expect(unit?.description).toContain(guidance);
    expect(() => new ItemChangeFlow(connectionNamed('other'))).toThrow(ConnectionName);
  });
});
