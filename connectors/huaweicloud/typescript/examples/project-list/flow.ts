// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAccessibleProjectsStep, type Connection, type ListAccessibleProjectsInput, type ListAccessibleProjectsOutput } from '@superdurable/connector-huaweicloud';

/** Static Dex Web connection name. */
export const ConnectionName = 'huaweicloud';
/** Durable project metadata without IAM credentials. */
export const projectListAttribute = new Attribute('huaweicloud-projects', jsonCodec<ListAccessibleProjectsOutput>());

/** List IAM projects accessible to the configured Huawei Cloud user. */
export class HuaweiCloudProjectListFlow implements Flow<ListAccessibleProjectsInput> {
  private readonly complete = new ProjectsFound();
  private readonly list;
  /** Bind the named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('project Flow requires the huaweicloud connection');
    this.list = newListAccessibleProjectsStep<ListAccessibleProjectsInput>({
      stepType: 'ListHuaweiCloudProjects',
      annotations: { groupId: 'huaweicloud', groupLabel: 'Huawei Cloud', explanation: 'Authenticate and read IAM projects accessible to the configured user.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      found: new StepRef('CompleteHuaweiCloudProjectList'),
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'HuaweiCloudProjectList'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<ListAccessibleProjectsInput> { return StepList.startStep(this.list).otherSteps(this.complete); }
  /** Register the persisted project list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [projectListAttribute] }; }
}

// dex:group group-id:huaweicloud group-label:"Huawei Cloud"
// dex:explanation text:"Persist project metadata and complete the Flow."
class ProjectsFound implements Step<QueryResult<ListAccessibleProjectsOutput>> {
  getStepType(): string { return 'CompleteHuaweiCloudProjectList'; }
  execute(context: Context, result: QueryResult<ListAccessibleProjectsOutput>): StepDecision {
    projectListAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
