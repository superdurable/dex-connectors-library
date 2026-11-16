// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Deterministic checks of the employee-changed example's Flow wiring and mappers.
import { Registry } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import {
  ConnectionName, EmployeeChangeFlow, EmployeeChangedTriggerBinding, HandOffFields, acceptEveryEmployeeChange, mapToFlowInput, mapToGetEmployeeInput, resolveFlowId, validateEmployeeChangeInput,
} from '../examples/employee-changed/flow.js';
import { BambooHRFake, newFakeConnection } from './bamboohr_fake.js';

const event = (action = 'Updated', employeeId = '140', lastChanged = '2026-10-13T09:00:01Z') => ({
  id: `employee-${employeeId}-${action.toLowerCase()}-1791882001`, occurredAt: new Date(lastChanged), payload: { employeeId, action, lastChanged },
});

describe('employee-changed example', () => {
  it('names one Flow per change and reads the hand-off fields', () => {
    expect(resolveFlowId(event())).toBe('bamboohr-employee-change-employee-140-updated-1791882001');
    const input = mapToFlowInput(event());
    expect(input).toEqual({ triggerEventId: 'employee-140-updated-1791882001', change: event().payload });
    expect(mapToGetEmployeeInput(input)).toEqual({ employeeId: '140', fields: HandOffFields });
    expect(acceptEveryEmployeeChange()).toBe(true);
  });
  it('refuses an unusable change', () => {
    expect(validateEmployeeChangeInput(mapToFlowInput(event('Deleted')))).toBe('');
    expect(validateEmployeeChangeInput({ ...mapToFlowInput(event()), triggerEventId: ' ' })).toContain('triggerEventId');
    expect(validateEmployeeChangeInput(mapToFlowInput(event('Updated', '')))).toContain('employeeId');
    expect(validateEmployeeChangeInput(mapToFlowInput(event('Updated', '140', '0001-01-01T00:00:00Z')))).toContain('lastChanged');
    expect(validateEmployeeChangeInput(mapToFlowInput(event('Terminated')))).toContain('action');
  });
  it('registers and declares its Trigger binding with the change type picker', () => {
    const fake = new BambooHRFake();
    const flow = new EmployeeChangeFlow(newFakeConnection(fake, ConnectionName));
    expect(() => new Registry([flow])).not.toThrow();
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding?.connectionName, binding?.bindingName, binding?.definition.trigger.triggerName]).toEqual([ConnectionName, EmployeeChangedTriggerBinding, 'employeeChanged']);
    const [unit] = binding?.configurationUI?.units ?? [];
    expect([unit?.id, unit?.unitId, unit?.label]).toEqual(['changeType', 'changeTypePicker', 'Employee changes']);
    expect(unit?.description).toContain('Every change');
    expect(unit?.description).toContain('restart the Worker');
    expect(unit?.bindings).toEqual([{ port: 'changeType', jsonPointer: '/changeType' }]);
    expect(() => new EmployeeChangeFlow(newFakeConnection(fake, 'other'))).toThrow(ConnectionName);
  });
});
