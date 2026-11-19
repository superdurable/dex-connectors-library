// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryTrackingStep, type Connection, type QueryTrackingOutput, type QueryTrackingInput } from '../../src/index.js';

/** Static Dex Web connection for the KuaidiBird example. */
export const ConnectionName = 'kuaidibird';
const trackingResult = new Attribute<QueryTrackingOutput>('kuaidibird-tracking-result', jsonCodec<QueryTrackingOutput>());

/** Read one city's current tracking through KuaidiBird. */
export class TrackingFlow implements Flow<QueryTrackingInput> {
  private readonly start = new StartTracking();
  private readonly complete = new CompleteTracking();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('KuaidiBird connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'KuaidiBirdTracking'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<QueryTrackingInput> {
    return StepList.startStep(this.start).otherSteps(newQueryTrackingStep({
      stepType: 'CallKuaidiBirdTracking',
      annotations: { groupId: 'kuaidibird', groupLabel: 'KuaidiBird', explanation: 'Read the current observations for one city.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, retrieved: new StepRef('CompleteKuaidiBirdTracking'),
    }), this.complete);
  }
  /** Register the durable tracking observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [trackingResult] }; }
}

// dex:group group-id:kuaidibird group-label:"KuaidiBird"
// dex:explanation text:"Start the tracking query."
class StartTracking implements Step<QueryTrackingInput> {
  getStepType(): string { return 'StartKuaidiBirdTracking'; }
  execute(_context: Context, input: QueryTrackingInput): StepDecision { return goTo(new StepRef('CallKuaidiBirdTracking'), input); }
}

// dex:group group-id:kuaidibird group-label:"KuaidiBird"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteTracking implements Step<QueryResult<QueryTrackingOutput>> {
  getStepType(): string { return 'CompleteKuaidiBirdTracking'; }
  execute(context: Context, result: QueryResult<QueryTrackingOutput>): StepDecision { trackingResult.set(context, result.value); return gracefulComplete(result.value); }
}
