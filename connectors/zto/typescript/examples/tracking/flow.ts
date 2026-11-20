// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryWaybillTraceStep, type Connection, type WaybillTrace, type QueryWaybillTraceInput } from '../../src/index.js';

/** Static Dex Web connection for the ZTO example. */
export const ConnectionName = 'zto';
const trackingResult = new Attribute<WaybillTrace>('zto-tracking-result', jsonCodec<WaybillTrace>());

/** Read one authorized waybill trace through ZTO. */
export class TrackingFlow implements Flow<QueryWaybillTraceInput> {
  private readonly start = new StartTracking();
  private readonly complete = new CompleteTracking();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('ZTO connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'ZTOTracking'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<QueryWaybillTraceInput> {
    return StepList.startStep(this.start).otherSteps(newQueryWaybillTraceStep({
      stepType: 'CallZTOTracking',
      annotations: { groupId: 'zto', groupLabel: 'ZTO', explanation: 'Read the authorized waybill trace.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteZTOTracking'),
    }), this.complete);
  }
  /** Register the durable tracking observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [trackingResult] }; }
}

// dex:group group-id:zto group-label:"ZTO"
// dex:explanation text:"Start the tracking query."
class StartTracking implements Step<QueryWaybillTraceInput> {
  getStepType(): string { return 'StartZTOTracking'; }
  execute(_context: Context, input: QueryWaybillTraceInput): StepDecision { return goTo(new StepRef('CallZTOTracking'), input); }
}

// dex:group group-id:zto group-label:"ZTO"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteTracking implements Step<QueryResult<WaybillTrace>> {
  getStepType(): string { return 'CompleteZTOTracking'; }
  execute(context: Context, result: QueryResult<WaybillTrace>): StepDecision { trackingResult.set(context, result.value); return gracefulComplete(result.value); }
}
