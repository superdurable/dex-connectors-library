// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListProjectsStep, newListTimeEntriesStep, type Connection, type ListProjectsInput, type ProjectPage, type TimeEntryPage } from '../../src/index.js';

/** Static Dex Web connection name. */
export const ConnectionName = 'harvest';

/** Read project and time entry pages in one Flow. */
export class HarvestLookupFlow implements Flow<ListProjectsInput> {
  private readonly start = new StartHarvestLookup();
  private readonly projectsListed = new ProjectsListed();
  private readonly complete = new CompleteHarvestLookup();

  /** Bind the connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Harvest lookup requires the static connection');
  }

  /** Stable Dex Flow identity. */ getFlowType(): string { return 'HarvestLookupTypeScript'; }
  /** Register the start, both provider Steps, and completion. */
  getSteps(): StepList<ListProjectsInput> {
    return StepList.startStep(this.start).otherSteps(
      newListProjectsStep({ stepType: 'ReadHarvestProjects', annotations: { groupId: 'harvest-lookup', groupLabel: 'Harvest lookup', explanation: 'Read one bounded project page.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, listed: this.projectsListed }),
      this.projectsListed,
      newListTimeEntriesStep({ stepType: 'ReadHarvestTimeEntries', annotations: { groupId: 'harvest-lookup', groupLabel: 'Harvest lookup', explanation: 'Read one bounded time entry page.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, listed: this.complete }),
      this.complete,
    );
  }
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Start the Harvest project page read."
class StartHarvestLookup implements Step<ListProjectsInput> {
  readonly inputCodec = jsonCodec<ListProjectsInput>();
  getStepType(): string { return 'StartHarvestLookupTypeScript'; }
  execute(_context: Context, input: ListProjectsInput): StepDecision { return goTo(new StepRef('ReadHarvestProjects'), input); }
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Read time entries after the project page succeeds."
class ProjectsListed implements Step<QueryResult<ProjectPage>> {
  getStepType(): string { return 'HarvestProjectsListed'; }
  execute(context: Context, result: QueryResult<ProjectPage>): StepDecision { void context; void result; return goTo(new StepRef('ReadHarvestTimeEntries'), { perPage: 10 }); }
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Return the time entry page to the caller."
class CompleteHarvestLookup implements Step<QueryResult<TimeEntryPage>> {
  getStepType(): string { return 'CompleteHarvestLookupTypeScript'; }
  execute(_context: Context, result: QueryResult<TimeEntryPage>): StepDecision { return gracefulComplete(result.value); }
}
