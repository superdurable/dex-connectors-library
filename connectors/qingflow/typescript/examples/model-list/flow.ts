// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListApplicationsStep, type Connection, type ListApplicationsInput, type ListApplicationsOutput } from '../../src/index.js';

/** Static Dex Web connection for the Qingflow example. */
export const ConnectionName = 'qingflow';
const applicationsResult = new Attribute<ListApplicationsOutput>('qingflow-application-list-result', jsonCodec<ListApplicationsOutput>());

/** Read the application list through Qingflow. */
export class ApplicationCatalogFlow implements Flow<ListApplicationsInput> {
  private readonly start = new StartApplicationList();
  private readonly complete = new CompleteApplicationList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Qingflow connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'QingflowApplicationCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListApplicationsInput> {
    return StepList.startStep(this.start).otherSteps(newListApplicationsStep({
      stepType: 'CallQingflow',
      annotations: { groupId: 'qingflow', groupLabel: 'Qingflow', explanation: 'Read the application catalog with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteQingflowApplications'),
    }), this.complete);
  }
  /** Register the durable application list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [applicationsResult] }; }
}

// dex:group group-id:qingflow group-label:"Qingflow"
// dex:explanation text:"Start the application-list query."
class StartApplicationList implements Step<ListApplicationsInput> {
  getStepType(): string { return 'StartQingflowApplications'; }
  execute(_context: Context, input: ListApplicationsInput): StepDecision { return goTo(new StepRef('CallQingflow'), input); }
}

// dex:group group-id:qingflow group-label:"Qingflow"
// dex:explanation text:"Persist the application list and complete the Flow."
class CompleteApplicationList implements Step<QueryResult<ListApplicationsOutput>> {
  getStepType(): string { return 'CompleteQingflowApplications'; }
  execute(context: Context, result: QueryResult<ListApplicationsOutput>): StepDecision { applicationsResult.set(context, result.value); return gracefulComplete(result.value); }
}
