// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryWaybillTraceStep, type Connection, type QueryWaybillTraceInput, type WaybillTrace } from '@superdurable/connector-cainiao';

/** Static Cainiao connection name used in Dex Web. */
export const ConnectionName = 'cainiao';
/** Durable logistics trace without credentials. */
export const waybillTraceAttribute = new Attribute('cainiao-waybill-trace', jsonCodec<WaybillTrace>());

/** Read and persist the events for one electronic waybill. */
export class CainiaoWaybillTraceFlow implements Flow<QueryWaybillTraceInput> {
  private readonly complete = new CompleteWaybillTrace();
  private readonly query;
  /** Bind the named TOP connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('waybill trace Flow requires the cainiao connection');
    this.query = newQueryWaybillTraceStep<QueryWaybillTraceInput>({
      stepType: 'QueryCainiaoWaybillTrace',
      annotations: { groupId: 'cainiao', groupLabel: 'Cainiao', explanation: 'Read the electronic waybill events from Alibaba TOP.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      found: new StepRef('CompleteCainiaoWaybillTrace'),
    });
  }
  /** Preserve the stable Flow identity. */ getFlowType(): string { return 'CainiaoWaybillTrace'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<QueryWaybillTraceInput> { return StepList.startStep(this.query).otherSteps(this.complete); }
  /** Register the durable public trace. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [waybillTraceAttribute] }; }
}

// dex:group group-id:cainiao group-label:"Cainiao"
// dex:explanation text:"Persist the logistics events and complete the Flow."
class CompleteWaybillTrace implements Step<QueryResult<WaybillTrace>> {
  getStepType(): string { return 'CompleteCainiaoWaybillTrace'; }
  execute(context: Context, result: QueryResult<WaybillTrace>): StepDecision {
    waybillTraceAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
