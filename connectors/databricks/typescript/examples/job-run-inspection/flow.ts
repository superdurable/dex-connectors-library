// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetJobRunStep, type Connection, type JobRunStatus, type GetJobRunInput,
} from '@superdurable/connector-databricks';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'databricks';
const job_runAttribute = new Attribute<JobRunStatus>('databricks-job_run', jsonCodec<JobRunStatus>());

/** Read one job_run through a typed Databricks Query. */
export class JobRunInspectionFlow implements Flow<GetJobRunInput> {
  private readonly complete = new CompleteJobRunInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Databricks connection name differs');
    this.read = newGetJobRunStep<GetJobRunInput>({
      stepType: 'CallGetJobRun',
      annotations: { groupId: 'provider', groupLabel: 'Databricks', explanation: 'Read one job_run from Databricks.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteDatabricks'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'DatabricksJobRunInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetJobRunInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected job_run result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [job_runAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected job_run and complete the Flow."
class CompleteJobRunInspection implements Step<QueryResult<JobRunStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteDatabricks'; }
  /** Persist and return the selected job_run. */
  execute(context: Context, result: QueryResult<JobRunStatus>): StepDecision {
    job_runAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
