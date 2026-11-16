// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import { GeneratedMock } from '../src/mock.js';
import type { EventChangedEvent } from '../src/index.js';
import {
  ConnectionName, EventChangeFlow, EventChangedTriggerBinding, FlowType, acceptEveryEventChange, invalidInputReason, mapToFlowInput, mapToGetEventInput, resolveFlowId,
} from '../examples/event-changed/flow/workflow.js';

const change: EventChangedEvent = {
  calendarId: 'team@group.calendar.google.com', changeKind: 'updated',
  event: { id: 'standup0001', calendarId: 'team@group.calendar.google.com', status: 'confirmed', start: {}, end: {}, isAllDay: false, transparency: 'opaque' },
};
const triggerEvent = { id: 'gcal-0123', occurredAt: new Date('2026-03-02T09:04:00Z'), payload: change };

describe('event-changed example', () => {
  it('names one Flow per change and reads its event back', () => {
    expect(resolveFlowId(triggerEvent)).toBe('google-calendar-event-change-gcal-0123');
    expect(acceptEveryEventChange()).toBe(true);
    expect(mapToFlowInput(triggerEvent)).toEqual({ triggerEventId: 'gcal-0123', change });
    expect(mapToGetEventInput(mapToFlowInput(triggerEvent))).toEqual({ calendarId: 'team@group.calendar.google.com', eventId: 'standup0001' });
  });

  it('refuses incomplete changes', () => {
    expect(invalidInputReason({ triggerEventId: 'gcal-0123', change })).toBe('');
    expect(invalidInputReason({ triggerEventId: ' ', change })).toBe('triggerEventId is required');
    expect(invalidInputReason({ triggerEventId: 'g', change: { ...change, event: { ...change.event, id: '' } } })).toContain('change.event.id');
    expect(invalidInputReason({ triggerEventId: 'g', change: { ...change, changeKind: 'moved' as never } })).toContain('changeKind');
  });

  it('declares the static binding with the calendar picker', () => {
    const flow = new EventChangeFlow(new GeneratedMock(ConnectionName).connection());
    expect(() => new Registry([flow])).not.toThrow();
    expect(flow.getFlowType()).toBe(FlowType);
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding!.definition.trigger.triggerName, binding!.connectionName, binding!.bindingName]).toEqual(['eventChanged', ConnectionName, EventChangedTriggerBinding]);
    const [unit] = binding!.configurationUI!.units;
    expect([unit!.id, unit!.unitId, unit!.label, unit!.required]).toEqual(['watchedCalendar', 'calendarPicker', 'Watched calendar', false]);
    expect(unit!.bindings).toEqual([{ port: 'calendarId', jsonPointer: '/calendarId' }, { port: 'calendarName', jsonPointer: '/calendarName' }]);
    expect(unit!.description).toMatch(/primary calendar.*baseline/s);
  });
});
