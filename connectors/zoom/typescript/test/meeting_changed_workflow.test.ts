// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Deterministic checks of the meeting-changed example's Flow wiring and mappers.
import { Registry } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import {
  ConnectionName, MeetingChangeFlow, MeetingChangedTriggerBinding, acceptEveryMeetingChange, mapToFlowInput, mapToGetMeetingInput, resolveFlowId, validateMeetingChangeInput,
} from '../examples/meeting-changed/flow.js';
import { ZoomMeetingsFake, newFakeConnection } from './zoom_meetings_fake.js';

const event = (changeKind: string, meetingId = 92674392836) => ({
  id: 'zoom-meeting-0123', occurredAt: new Date('2036-02-23T00:00:00Z'), payload: { changeKind, meeting: { id: meetingId, topic: 'Consultation' } },
}) as Parameters<typeof mapToFlowInput>[0];

describe('meeting-changed example', () => {
  it('names one Flow per change and reads the changed meeting', () => {
    expect(resolveFlowId(event('updated'))).toBe('zoom-meeting-change-zoom-meeting-0123');
    const input = mapToFlowInput(event('updated'));
    expect(input).toEqual({ triggerEventId: 'zoom-meeting-0123', change: event('updated').payload });
    expect(mapToGetMeetingInput(input)).toEqual({ meetingId: 92674392836 });
    expect(acceptEveryMeetingChange()).toBe(true);
  });
  it('refuses an unusable change', () => {
    expect(validateMeetingChangeInput(mapToFlowInput(event('removed')))).toBe('');
    expect(validateMeetingChangeInput({ ...mapToFlowInput(event('updated')), triggerEventId: ' ' })).toContain('triggerEventId');
    expect(validateMeetingChangeInput(mapToFlowInput(event('updated', 0)))).toContain('meeting.id');
    expect(validateMeetingChangeInput(mapToFlowInput(event('ended')))).toContain('changeKind');
  });
  it('registers and declares its Trigger binding', () => {
    const fake = new ZoomMeetingsFake('zoom-host-1');
    const flow = new MeetingChangeFlow(newFakeConnection(fake, ConnectionName));
    expect(() => new Registry([flow])).not.toThrow();
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding?.connectionName, binding?.bindingName, binding?.definition.trigger.triggerName]).toEqual([ConnectionName, MeetingChangedTriggerBinding, 'meetingChanged']);
    expect(() => new MeetingChangeFlow(newFakeConnection(fake, 'other'))).toThrow(ConnectionName);
  });
});
