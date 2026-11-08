// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetWorkerStep, type Connection, type WorkerSummary, type GetWorkerInput,
} from '@superdurable/connector-rippling';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'rippling';
const workerAttribute = new Attribute<WorkerSummary>('rippling-worker', jsonCodec<WorkerSummary>());

/** Read one worker through a typed Rippling Query. */
export class WorkerInspectionFlow implements Flow<GetWorkerInput> {
  private readonly complete = new CompleteWorkerInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Rippling connection name differs');
    this.read = newGetWorkerStep<GetWorkerInput>({
      stepType: 'CallGetWorker',
      annotations: { groupId: 'provider', groupLabel: 'Rippling', explanation: 'Read one worker from Rippling.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteRippling'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'RipplingWorkerInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetWorkerInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected worker result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [workerAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected worker and complete the Flow."
class CompleteWorkerInspection implements Step<QueryResult<WorkerSummary>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteRippling'; }
  /** Persist and return the selected worker. */
  execute(context: Context, result: QueryResult<WorkerSummary>): StepDecision {
    workerAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
