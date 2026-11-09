// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetWorkerStep, type Connection, type GetWorkerInput, type WorkerSummary } from '@superdurable/connector-workday';

/** Static Dex Web connection for the worker example. */
export const ConnectionName = 'workday';
const resultAttribute = new Attribute<WorkerSummary>('workday-typescript-worker', jsonCodec<WorkerSummary>());

/** Read one worker visible to the authorized Workday user. */
export class WorkerFlow implements Flow<GetWorkerInput> {
  private readonly complete = new CompleteWorker();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Workday connection name differs');
    this.read = newGetWorkerStep<GetWorkerInput>({
      stepType: 'CallGetWorker',
      annotations: { groupId: 'provider', groupLabel: 'Workday', explanation: 'Read the selected worker profile.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WorkdayWorkerInspectionTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetWorkerInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the worker and complete the Flow."
class CompleteWorker implements Step<QueryResult<WorkerSummary>> {
  readonly inputCodec = jsonCodec<QueryResult<WorkerSummary>>();
  getStepType(): string { return 'CompleteWorker'; }
  execute(context: Context, result: QueryResult<WorkerSummary>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
