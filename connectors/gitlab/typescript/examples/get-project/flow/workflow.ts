// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetProjectStep, type Connection, type GetProjectInput, type Project } from '@superdurable/connector-gitlab';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'gitlab';
/** Safe project lookup input retained for flow inspection. */
export const projectRequest = new Attribute<GetProjectInput>('gitlab-project-request', jsonCodec<GetProjectInput>());
/** Safe project result retained for flow inspection. */
export const projectResult = new Attribute<Project>('gitlab-project-result', jsonCodec<Project>());

/** Read one GitLab project through a Dex Worker. */
export class GetProjectFlow implements Flow<GetProjectInput> {
  private readonly prepare = new PrepareGetProject();
  private readonly complete = new CompleteGetProject();
  /** Bind the named GitLab connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('GitLab connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'GitLabGetProjectFlow'; }
  /** Register application and provider Steps. */
  getSteps(): StepList<GetProjectInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetProjectStep({
        stepType: 'GetGitLabProject',
        annotations: { groupId: 'gitlab-project', groupLabel: 'GitLab Project', explanation: 'Read one GitLab project.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }), this.complete,
    );
  }
  /** Register persisted Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [projectRequest, projectResult] }; }
}

// dex:group group-id:gitlab-project group-label:"GitLab Project"
// dex:explanation text:"Validate the project identifier before calling GitLab."
/** Validate and retain the safe project identifier. */
export class PrepareGetProject implements Step<GetProjectInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareGitLabGetProject'; }
  /** Start the provider lookup. */
  execute(context: Context, input: GetProjectInput): StepDecision {
    if (!input.projectId?.trim()) return forceFail('projectId is required');
    projectRequest.set(context, input);
    return goTo(new StepRef('GetGitLabProject'), input);
  }
}

// dex:group group-id:gitlab-project group-label:"GitLab Project"
// dex:explanation text:"Persist the project summary and complete the Flow."
/** Complete the Flow with the safe project summary. */
export class CompleteGetProject implements Step<QueryResult<Project>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteGitLabGetProject'; }
  /** Persist and return the project. */
  execute(context: Context, result: QueryResult<Project>): StepDecision {
    projectResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
