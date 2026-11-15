// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { newConnectionWithOperations, type InviteeEvent } from '../src/index.js';
import type { Operations } from '../src/generated/connection.js';
import { ConnectionName, InviteeCreatedTriggerBinding, InviteeRecorderFlow, acceptBooking, mapToFlowInput, mapToGetScheduledEventInput, resolveFlowId } from '../examples/invitee-recorder/flow/workflow.js';

function inviteeEvent(event: string, status: string): TriggerEvent<InviteeEvent> {
  const scheduledEventUri = 'https://api.calendly.com/scheduled_events/EVENT0001';
  return {
    id: `${event}:EVENT0001:INVITEE01`, occurredAt: new Date('2026-09-30T11:59:00Z'),
    payload: {
      event, createdAt: '2026-09-30T11:59:00Z',
      invitee: { uri: `${scheduledEventUri}/invitees/INVITEE01`, scheduledEventUri, email: 'ada@example.com', name: 'Ada Lovelace', status, rescheduled: false },
      scheduledEvent: { uri: scheduledEventUri, status: 'active', startTime: '2026-10-02T17:00:00Z', endTime: '2026-10-02T17:30:00Z', location: {}, inviteesCounter: { total: 1, active: 1, limit: 1 } },
    },
  };
}

it('admits only an active invitee.created event and maps it to one Flow', () => {
  const booked = inviteeEvent('invitee.created', 'active');
  expect(acceptBooking(booked)).toBe(true);
  expect(acceptBooking(inviteeEvent('invitee.canceled', 'canceled'))).toBe(false);
  expect(acceptBooking(inviteeEvent('invitee.created', 'canceled'))).toBe(false);
  expect(resolveFlowId(booked)).toBe('calendly-invitee.created-EVENT0001-INVITEE01');
  const booking = mapToFlowInput(booked);
  expect(booking).toEqual({ eventId: booked.id, inviteeUri: booked.payload.invitee.uri, inviteeEmail: 'ada@example.com', inviteeName: 'Ada Lovelace', scheduledEventUri: booked.payload.invitee.scheduledEventUri, bookedAt: '2026-09-30T11:59:00Z' });
  expect(mapToGetScheduledEventInput(booking)).toEqual({ scheduledEventUri: 'https://api.calendly.com/scheduled_events/EVENT0001' });
});

it('declares the invitee-created binding with its event type unit', () => {
  const flow = new InviteeRecorderFlow(newConnectionWithOperations({} as Operations, { provider: 'calendly', name: ConnectionName }));
  const [binding] = flow.getConnectorTriggerBindings();
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: InviteeCreatedTriggerBinding, definition: { trigger: { connectorId: 'calendly', triggerName: 'inviteeEventReceived' } } });
  expect(binding!.configurationUI!.units).toEqual([expect.objectContaining({ unitId: 'eventTypePicker', required: false, bindings: [{ port: 'eventTypeUri', jsonPointer: '/eventTypeUri' }] })]);
  expect(binding!.configurationUI!.units[0]!.description).toMatch(/Leave it empty/);
});
