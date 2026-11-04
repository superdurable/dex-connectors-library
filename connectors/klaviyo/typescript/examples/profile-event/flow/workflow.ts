// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newCreateEventStep, newGetProfileStep, type Connection, type CreateEventInput, type EventAcceptance, type Profile } from '@superdurable/connector-klaviyo';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'klaviyo-profile-events';

/** Safe Flow input containing a profile ID and event metric. */
export interface ProfileEventInput { profileId: string; metricName: string; properties?: Record<string, string> }
/** Safe Flow input retained for the second operation. */
export const profileEventRequest = new Attribute<ProfileEventInput>('klaviyo-profile-event-request', jsonCodec<ProfileEventInput>());
/** Provider event acceptance retained for inspection. */
export const eventAcceptance = new Attribute<EventAcceptance>('klaviyo-event-acceptance', jsonCodec<EventAcceptance>());

/** Read a profile and submit a deduplicated event for its email. */
export class ProfileEventFlow implements Flow<ProfileEventInput> {
  private readonly prepare = new PrepareProfileEvent();
  private readonly prepareEvent = new PrepareEvent();
  private readonly complete = new CompleteProfileEvent();

  /** Bind a named Klaviyo connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Klaviyo connection is required'); }
  /** Stable Dex Flow type. */
  getFlowType(): string { return 'KlaviyoProfileEventFlow'; }
  /** Register both provider operations and local transitions. */
  getSteps(): StepList<ProfileEventInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetProfileStep({ stepType: 'GetKlaviyoProfile', annotations: { groupId: 'klaviyo-profile-event', groupLabel: 'Klaviyo Profile Event', explanation: 'Read one Klaviyo profile before sending its event.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ id: input.profileId }), found: this.prepareEvent }),
      this.prepareEvent,
      newCreateEventStep({ stepType: 'CreateKlaviyoEvent', annotations: { groupId: 'klaviyo-profile-event', groupLabel: 'Klaviyo Profile Event', explanation: 'Submit one event with a stable provider unique ID.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, accepted: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [profileEventRequest, eventAcceptance] }; }
}

/** Validate and retain nonsecret input. */
// dex:group group-id:klaviyo-profile-event group-label:"Klaviyo Profile Event"
// dex:explanation text:"Validate the profile and metric before reading Klaviyo."
export class PrepareProfileEvent implements Step<ProfileEventInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareKlaviyoProfileEvent'; }
  /** Start the profile lookup. */
  execute(context: Context, input: ProfileEventInput): StepDecision {
    if (!input.profileId?.trim() || !input.metricName?.trim()) return forceFail('Klaviyo profileId and metricName are required');
    profileEventRequest.set(context, input);
    return goTo(new StepRef('GetKlaviyoProfile'), input);
  }
}

/** Build event input from the returned profile and retained request. */
// dex:group group-id:klaviyo-profile-event group-label:"Klaviyo Profile Event"
// dex:explanation text:"Use the returned profile email for event submission."
export class PrepareEvent implements Step<QueryResult<Profile>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareKlaviyoEvent'; }
  /** Start the event mutation only when the profile has an email. */
  execute(context: Context, result: QueryResult<Profile>): StepDecision {
    if (!result.value.email) return forceFail('Klaviyo profile has no email');
    const request = profileEventRequest.get(context);
    const event: CreateEventInput = { email: result.value.email, metricName: request.metricName, ...(request.properties ? { properties: request.properties } : {}) };
    return goTo(new StepRef('CreateKlaviyoEvent'), event);
  }
}

/** Retain event acceptance and complete the Flow. */
// dex:group group-id:klaviyo-profile-event group-label:"Klaviyo Profile Event"
// dex:explanation text:"Record the accepted event and complete the Flow."
export class CompleteProfileEvent implements Step<MutationResult<EventAcceptance>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteKlaviyoProfileEvent'; }
  /** Retain only nonsecret acceptance. */
  execute(context: Context, result: MutationResult<EventAcceptance>): StepDecision { eventAcceptance.set(context, result.value); return gracefulComplete(result.value); }
}
