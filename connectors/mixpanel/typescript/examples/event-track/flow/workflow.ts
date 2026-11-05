// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newTrackEventStep, type Connection, type TrackEventInput, type EventImportResult } from '@superdurable/connector-mixpanel';

/** Static Dex Web connection used by Worker and starter. */
export const ConnectionName = 'mixpanel';
/** Safe event request retained for Flow inspection. */
export const eventRequest = new Attribute<TrackEventInput>('mixpanel-event-request', jsonCodec<TrackEventInput>());
/** Safe import result retained for Flow inspection. */
export const eventOutcome = new Attribute<EventImportResult>('mixpanel-event-outcome', jsonCodec<EventImportResult>());

/** One stable, deduplicated Mixpanel event import Flow. */
export class EventTrackFlow implements Flow<TrackEventInput> {
  private readonly prepare = new PrepareEvent();
  private readonly complete = new CompleteEvent();

  /** Bind the named Mixpanel connection. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Mixpanel connection is required');
  }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'MixpanelEventTrack'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<TrackEventInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newTrackEventStep({
        stepType: 'ImportMixpanelEvent',
        annotations: { groupId: 'event-track', groupLabel: 'Event track', explanation: 'Import one event with stable time and distinct ID for deduplication.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        imported: this.complete,
      }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [eventRequest, eventOutcome] }; }
}

/** Validate event identity before provider dispatch. */
// dex:group group-id:event-track group-label:"Event track"
// dex:explanation text:"Validate stable event identity before calling Mixpanel."
export class PrepareEvent implements Step<TrackEventInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareMixpanelEvent'; }
  /** Persist safe input and start the import Step. */
  execute(context: Context, input: TrackEventInput): StepDecision {
    if (!input.event?.trim() || !input.distinctID?.trim() || !Number.isSafeInteger(input.time) || input.time <= 0) return forceFail('event, distinctID, and positive Unix time are required');
    eventRequest.set(context, input);
    return goTo(new StepRef('ImportMixpanelEvent'), input);
  }
}

/** Persist successful import and complete the Flow. */
// dex:group group-id:event-track group-label:"Event track"
// dex:explanation text:"Complete after Mixpanel confirms event ingestion."
export class CompleteEvent implements Step<MutationResult<EventImportResult>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteMixpanelEvent'; }
  /** Retain only the safe provider result. */
  execute(context: Context, result: MutationResult<EventImportResult>): StepDecision {
    eventOutcome.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
