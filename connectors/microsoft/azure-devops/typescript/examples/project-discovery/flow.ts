// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListProjectsStep, type Connection, type ListProjectsInput, type Projects } from '@superdurable/connector-microsoft-azure-devops';

/** Static Dex Web connection for the Azure DevOps example. */
export const ConnectionName = 'azure-devops';
const resultAttribute = new Attribute<Projects>('azure-devops-projects', jsonCodec<Projects>());

/** List projects visible to the connected Azure DevOps user. */
export class ProjectDiscoveryFlow implements Flow<ListProjectsInput> {
  private readonly complete = new CompleteProjectDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Azure DevOps connection name differs');
    this.read = newListProjectsStep<ListProjectsInput>({
      stepType: 'CallListProjects',
      annotations: { groupId: 'provider', groupLabel: 'AzureDevOps', explanation: 'List projects visible to the connected user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'AzureDevOpsProjectDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListProjectsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the project page and complete the Flow."
class CompleteProjectDiscovery implements Step<QueryResult<Projects>> {
  readonly inputCodec = jsonCodec<QueryResult<Projects>>();
  getStepType(): string { return 'CompleteListProjects'; }
  execute(context: Context, result: QueryResult<Projects>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
