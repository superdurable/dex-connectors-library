// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetProjectAPISummaryStep, type Connection, type GetProjectAPISummaryInput, type GetProjectAPISummaryOutput } from '@superdurable/connector-apifox';

/** Static Dex Web connection for the Apifox example. */
export const ConnectionName = 'apifox';
const projectSummaryResult = new Attribute<GetProjectAPISummaryOutput>('apifox-project-summary-result', jsonCodec<GetProjectAPISummaryOutput>());

/** Read one accessible project's bounded OpenAPI summary. */
export class ProjectAPISummaryFlow implements Flow<GetProjectAPISummaryInput> {
  private readonly complete = new CompleteProjectSummary();
  private readonly call;

  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Apifox connection name differs');
    this.call = newGetProjectAPISummaryStep<GetProjectAPISummaryInput>({
      stepType: 'CallApifox',
      annotations: { groupId: 'apifox', groupLabel: 'Apifox', explanation: 'Export an accessible project and keep only its OpenAPI summary.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteApifoxProjectSummary'),
    });
  }

  /** Preserve the Flow identity. */
  getFlowType(): string { return 'ApifoxProjectAPISummary'; }

  /** Register provider and terminal Steps. */
  getSteps(): StepList<GetProjectAPISummaryInput> {
    return StepList.startStep(this.call).otherSteps(this.complete);
  }

  /** Register the durable project summary. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [projectSummaryResult] }; }
}

// dex:group group-id:apifox group-label:"Apifox"
// dex:explanation text:"Persist the bounded summary and complete the Flow."
class CompleteProjectSummary implements Step<QueryResult<GetProjectAPISummaryOutput>> {
  getStepType(): string { return 'CompleteApifoxProjectSummary'; }
  execute(context: Context, result: QueryResult<GetProjectAPISummaryOutput>): StepDecision {
    projectSummaryResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
