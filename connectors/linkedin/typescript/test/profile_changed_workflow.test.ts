// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Deterministic checks of the profile-changed example's Flow wiring and mappers.
import { Registry } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import {
  ConnectionName, ProfileChangeFlow, ProfileChangedTriggerBinding, acceptEveryProfileChange, mapToFlowInput, mapToGetAuthenticatedProfileInput, resolveFlowId, validateProfileChangeInput,
} from '../examples/profile-changed/flow.js';
import { LinkedInProfileFake, adaProfile, newFakeConnection } from './linkedin_profile_fake.js';

const event = (changedFields: string[] = ['name'], subject = '782bbtaQ') => ({
  id: 'linkedin-profile-0123', occurredAt: new Date('2036-02-23T00:00:00Z'),
  payload: { subject, previous: { subject, name: 'Ada Lovelace', emailVerified: true }, current: { subject, name: 'Ada King', emailVerified: true }, changedFields },
}) as Parameters<typeof mapToFlowInput>[0];

describe('profile-changed example', () => {
  it('names one Flow per change and reads the verified profile', () => {
    expect(resolveFlowId(event())).toBe('linkedin-profile-change-linkedin-profile-0123');
    const input = mapToFlowInput(event());
    expect(input).toEqual({ triggerEventId: 'linkedin-profile-0123', change: event().payload });
    expect(mapToGetAuthenticatedProfileInput()).toEqual({});
    expect(acceptEveryProfileChange()).toBe(true);
  });
  it('refuses an unusable change', () => {
    expect(validateProfileChangeInput(mapToFlowInput(event()))).toBe('');
    expect(validateProfileChangeInput({ ...mapToFlowInput(event()), triggerEventId: ' ' })).toContain('triggerEventId');
    expect(validateProfileChangeInput(mapToFlowInput(event(['name'], ' ')))).toContain('change.subject');
    expect(validateProfileChangeInput(mapToFlowInput(event([])))).toContain('changedFields');
  });
  it('registers and declares its Trigger binding', () => {
    const fake = new LinkedInProfileFake(adaProfile());
    const flow = new ProfileChangeFlow(newFakeConnection(fake, ConnectionName));
    expect(() => new Registry([flow])).not.toThrow();
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding?.connectionName, binding?.bindingName, binding?.definition.trigger.triggerName]).toEqual([ConnectionName, ProfileChangedTriggerBinding, 'profileChanged']);
    expect(() => new ProfileChangeFlow(newFakeConnection(fake, 'other'))).toThrow(ConnectionName);
  });
});
