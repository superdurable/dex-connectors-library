// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Deterministic checks of the member-changed example's Flow wiring, binding units, and mappers.
import { Registry } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import {
  ConnectionName, MemberChangeFlow, MemberChangedTriggerBinding, acceptEveryMemberChange, mapToFlowInput, mapToGetMemberInput, resolveFlowId, validateMemberChangeInput,
} from '../examples/member-changed/flow.js';
import { newConnection } from '../src/index.js';
import { MailchimpAudienceFake, newFakeClient } from './mailchimp_audience_fake.js';

const event = (emailAddress = 'ben@example.com') => ({
  id: 'mailchimp-0123', occurredAt: new Date('2026-11-01T00:00:00Z'),
  payload: { audienceId: '57afe96172', member: { subscriberHash: '2c66f6142933ed5a97948f89cb1c7be0', emailAddress, status: 'pending', tagCount: 0, listId: '57afe96172' } },
}) as Parameters<typeof mapToFlowInput>[0];
const connectionNamed = (name: string) => newConnection(newFakeClient(new MailchimpAudienceFake('57afe96172')), { provider: 'mailchimp', name });

describe('member-changed example', () => {
  it('names one Flow per change and reads the changed contact', () => {
    expect(resolveFlowId(event())).toBe('mailchimp-member-change-mailchimp-0123');
    const input = mapToFlowInput(event());
    expect(input).toEqual({ triggerEventId: 'mailchimp-0123', change: event().payload });
    expect(mapToGetMemberInput(input)).toEqual({ listId: '57afe96172', emailAddress: 'ben@example.com' });
    expect(acceptEveryMemberChange()).toBe(true);
  });
  it('refuses an unusable change', () => {
    expect(validateMemberChangeInput(mapToFlowInput(event()))).toBe('');
    expect(validateMemberChangeInput({ ...mapToFlowInput(event()), triggerEventId: ' ' })).toContain('triggerEventId');
    expect(validateMemberChangeInput(mapToFlowInput(event(' ')))).toContain('emailAddress');
  });
  it('registers and declares its Trigger binding with the audience and status units', () => {
    const flow = new MemberChangeFlow(connectionNamed(ConnectionName));
    expect(() => new Registry([flow])).not.toThrow();
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding?.connectionName, binding?.bindingName, binding?.definition.trigger.triggerName]).toEqual([ConnectionName, MemberChangedTriggerBinding, 'memberChanged']);
    const units = binding?.configurationUI?.units ?? [];
    expect(units.map(unit => [unit.unitId, unit.required, unit.bindings])).toEqual([
      ['audienceIdEntry', true, [{ port: 'audienceId', jsonPointer: '/audienceId' }]],
      ['memberStatusPicker', false, [{ port: 'statuses', jsonPointer: '/statuses' }]],
    ]);
    expect(units[0]?.description).toContain('Audience > More options > Audience settings');
    expect(units[1]?.description).toContain('Select none');
    expect(() => new MemberChangeFlow(connectionNamed('other'))).toThrow(ConnectionName);
  });
});
