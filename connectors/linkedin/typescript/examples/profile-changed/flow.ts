// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, forceFail, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineProfileChangedTriggerBinding, type ProfileChangedEvent } from '@superdurable/connector-linkedin';

/** Static LinkedIn connection selected in Dex Web. */
export const ConnectionName = 'linkedin-profile-watcher';
/** Static Trigger binding for the connected member. */
export const ProfileChangedBinding = 'own-profile-changes';
/** Delivered profile change and stable event identity. */
export interface ProfileChangeInput { /** Trigger event ID. */ triggerEventId: string; /** Previous and current profile claims. */ change: ProfileChangedEvent }
/** Saved nonsecret profile change record. */
export const profileChangeAttribute = new Attribute<ProfileChangeInput>('linkedin-profile-change', jsonCodec<ProfileChangeInput>());

/** Record each changed connected-member profile in its own Flow. */
export class ProfileChangeFlow implements Flow<ProfileChangeInput> {
  private readonly record = new RecordProfileChange();
  /** Bind the named LinkedIn connection. */ constructor(readonly connectionName = ConnectionName) {}
  /** Stable Flow type. */ getFlowType(): string { return 'LinkedInProfileChangeTypeScript'; }
  /** Register the recording Step. */ getSteps(): StepList<ProfileChangeInput> { return StepList.startStep(this.record); }
  /** Register the persisted change. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [profileChangeAttribute] }; }
  /** Declare the fieldless binding that always watches the connected member. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] { return [defineProfileChangedTriggerBinding({ connectionName: this.connectionName, bindingName: ProfileChangedBinding })]; }
}

/** Stable Flow ID for duplicate Trigger delivery. */
export function resolveFlowId(event: TriggerEvent<ProfileChangedEvent>): string { return `linkedin-profile-change-${event.id}`; }
/** Keep event ID beside its delivered change. */
export function mapToFlowInput(event: TriggerEvent<ProfileChangedEvent>): ProfileChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Deliver every changed profile. */
export function acceptEveryProfileChange(): boolean { return true; }

// dex:group group-id:linkedin-profile group-label:"LinkedIn Profile"
// dex:explanation text:"Validate and persist the connected member's delivered profile change."
class RecordProfileChange implements Step<ProfileChangeInput> {
  getStepType(): string { return 'RecordLinkedInProfileChange'; }
  execute(context: Context, input: ProfileChangeInput): StepDecision {
    if (!input.triggerEventId?.trim() || !input.change?.subject?.trim() || input.change.previous.subject !== input.change.subject || input.change.current.subject !== input.change.subject || !input.change.changedFields.length) return forceFail('invalid LinkedIn profile change');
    profileChangeAttribute.set(context, input);
    return gracefulComplete(input);
  }
}
