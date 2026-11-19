// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryWaybillTraceStep, type Connection, type WaybillTrace, type QueryWaybillTraceInput } from '../../src/index.js';

/** Static Dex Web connection for the STO example. */
export const ConnectionName = 'sto';
const trackingResult = new Attribute<WaybillTrace>('sto-tracking-result', jsonCodec<WaybillTrace>());

/** Read one authorized waybill trace through STO. */
export class TrackingFlow implements Flow<QueryWaybillTraceInput> {
  private readonly start = new StartTracking();
  private readonly complete = new CompleteTracking();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('STO connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'STOTracking'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<QueryWaybillTraceInput> {
    return StepList.startStep(this.start).otherSteps(newQueryWaybillTraceStep({
      stepType: 'CallSTOTracking',
      annotations: { groupId: 'sto', groupLabel: 'STO', explanation: 'Read the authorized waybill trace.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteSTOTracking'),
    }), this.complete);
  }
  /** Register the durable tracking observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [trackingResult] }; }
}

// dex:group group-id:sto group-label:"STO"
// dex:explanation text:"Start the tracking query."
class StartTracking implements Step<QueryWaybillTraceInput> {
  getStepType(): string { return 'StartSTOTracking'; }
  execute(_context: Context, input: QueryWaybillTraceInput): StepDecision { return goTo(new StepRef('CallSTOTracking'), input); }
}

// dex:group group-id:sto group-label:"STO"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteTracking implements Step<QueryResult<WaybillTrace>> {
  getStepType(): string { return 'CompleteSTOTracking'; }
  execute(context: Context, result: QueryResult<WaybillTrace>): StepDecision { trackingResult.set(context, result.value); return gracefulComplete(result.value); }
}
