// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  defineProfileChangedTriggerBinding, newGetAuthenticatedProfileStep,
  type AuthenticatedProfile, type Connection, type GetAuthenticatedProfileInput, type ProfileChangedEvent, type ProfileField,
} from '@superdurable/connector-linkedin';

/** Static Dex Web connection for LinkedIn authorization. */
export const ConnectionName = 'linkedin-profile-watcher';
/** The stable profileChanged binding that starts this Flow. */
export const ProfileChangedTriggerBinding = 'own-profile-changes';

/** One profileChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface ProfileChangeInput { /** Names the Flow too. */ triggerEventId: string; /** The delivered change. */ change: ProfileChangedEvent }
/** The Flow result: the changed claims and the verified profile read back from LinkedIn. */
export interface ProfileChangeOutcome {
  /** The member whose profile changed. */ subject: string;
  /** The delivered changed claims. */ changedFields: ProfileField[];
  /** Read back from LinkedIn; absent when the member has no verified email. */ verifiedProfile?: AuthenticatedProfile;
}

/** The delivered change. */
export const profileChangeAttribute = new Attribute<ProfileChangeInput>('linkedin-profile-change', jsonCodec<ProfileChangeInput>());
/** The recorded outcome. */
export const profileChangeOutcomeAttribute = new Attribute<ProfileChangeOutcome>('linkedin-profile-change-outcome', jsonCodec<ProfileChangeOutcome>());

/** Records one change of the connected LinkedIn member's own profile and reads the verified profile back. */
export class ProfileChangeFlow implements Flow<ProfileChangeInput> {
  private readonly record = new RecordProfileChange();
  private readonly complete = new CompleteProfileChange();
  /** Bind the static LinkedIn connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError(`profile change Flow requires the ${ConnectionName} connection`);
  }
  /** Stable Flow type. */ getFlowType(): string { return 'LinkedInProfileChangeTypeScript'; }
  /** Start at the record Step, which the Trigger starts the Flow at. */
  getSteps(): StepList<ProfileChangeInput> {
    return StepList.startStep(this.record).otherSteps(newGetAuthenticatedProfileStep<ProfileChangeInput>({
      stepType: 'ReadLinkedInVerifiedProfile', connection: this.connection, connectionName: ConnectionName,
      annotations: { groupId: 'change', groupLabel: 'Change', explanation: "Read the member's verified profile back from LinkedIn, so the record holds the current verified email." },
      mapToOperationInput: mapToGetAuthenticatedProfileInput, profileLoaded: this.complete,
      // Business reaction: a change that unverified or removed the email is recorded without a verified profile.
      verifiedEmailRequired: this.complete,
    }), this.complete);
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [profileChangeAttribute, profileChangeOutcomeAttribute] }; }
  /** Declare the profileChanged binding; it watches only the connected member's own profile, so Dex Web shows no field for it. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineProfileChangedTriggerBinding({ connectionName: ConnectionName, bindingName: ProfileChangedTriggerBinding })];
  }
}

/** Name one Flow per change, so a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<ProfileChangedEvent>): string { return `linkedin-profile-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<ProfileChangedEvent>): ProfileChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; an application narrows it, for example to email changes. */
export function acceptEveryProfileChange(): boolean { return true; }
/** The provider Query input, which has no field. */
export function mapToGetAuthenticatedProfileInput(): GetAuthenticatedProfileInput { return {}; }

/** Return why a delivered change is unusable, or '' when it is valid. */
export function validateProfileChangeInput(input: ProfileChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.subject?.trim()) return 'change.subject is required';
  if (!input.change.changedFields?.length) return 'change.changedFields is required';
  return '';
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered profile change before reading the verified profile."
class RecordProfileChange implements Step<ProfileChangeInput> {
  getStepType(): string { return 'RecordLinkedInProfileChange'; }
  execute(context: Context, input: ProfileChangeInput): StepDecision {
    const problem = validateProfileChangeInput(input);
    if (problem) return forceFail(problem);
    profileChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadLinkedInVerifiedProfile'), input);
  }
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the verified profile, or none without a verified email, and complete the Flow."
class CompleteProfileChange implements Step<QueryResult<AuthenticatedProfile>> {
  getStepType(): string { return 'CompleteLinkedInProfileChange'; }
  execute(context: Context, result: QueryResult<AuthenticatedProfile>): StepDecision {
    const { change } = profileChangeAttribute.get(context);
    const outcome: ProfileChangeOutcome = { subject: change.subject, changedFields: change.changedFields, ...(result.branch === 'profileLoaded' ? { verifiedProfile: result.value } : {}) };
    profileChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
