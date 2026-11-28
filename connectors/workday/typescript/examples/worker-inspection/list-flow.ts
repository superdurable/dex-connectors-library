// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListWorkersStep, type Connection, type ListWorkersInput, type WorkerPage } from '@superdurable/connector-workday';

const ConnectionName = 'workday';
const resultAttribute = new Attribute<WorkerPage>('workday-typescript-worker-page', jsonCodec<WorkerPage>());

/** Read a page of workers visible to the authorized Workday user. */
export class WorkerListFlow implements Flow<ListWorkersInput> {
  private readonly complete = new CompleteWorkerPage();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Workday connection name differs');
    this.read = newListWorkersStep<ListWorkersInput>({
      stepType: 'CallListWorkers',
      annotations: { groupId: 'provider', groupLabel: 'Workday', explanation: 'Read one page of visible workers.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WorkdayWorkerListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListWorkersInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the worker page and complete the Flow."
class CompleteWorkerPage implements Step<QueryResult<WorkerPage>> {
  readonly inputCodec = jsonCodec<QueryResult<WorkerPage>>();
  getStepType(): string { return 'CompleteWorkerPage'; }
  execute(context: Context, result: QueryResult<WorkerPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
