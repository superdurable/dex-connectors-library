// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListTeamsStep, type Connection, type ListTeamsInput, type ListTeamsOutput } from '../../src/index.js';

/** Static Dex Web connection for the Rainbond example. */
export const ConnectionName = 'rainbond';
const teamsResult = new Attribute<ListTeamsOutput>('rainbond-team-list-result', jsonCodec<ListTeamsOutput>());

/** Read the team list through Rainbond. */
export class TeamCatalogFlow implements Flow<ListTeamsInput> {
  private readonly start = new StartTeamList();
  private readonly complete = new CompleteTeamList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Rainbond connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'RainbondTeamCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListTeamsInput> {
    return StepList.startStep(this.start).otherSteps(newListTeamsStep({
      stepType: 'CallRainbond',
      annotations: { groupId: 'rainbond', groupLabel: 'Rainbond', explanation: 'Read the team catalog with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteRainbondTeams'),
    }), this.complete);
  }
  /** Register the durable team list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [teamsResult] }; }
}

// dex:group group-id:rainbond group-label:"Rainbond"
// dex:explanation text:"Start the team-list query."
class StartTeamList implements Step<ListTeamsInput> {
  getStepType(): string { return 'StartRainbondTeams'; }
  execute(_context: Context, input: ListTeamsInput): StepDecision { return goTo(new StepRef('CallRainbond'), input); }
}

// dex:group group-id:rainbond group-label:"Rainbond"
// dex:explanation text:"Persist the team list and complete the Flow."
class CompleteTeamList implements Step<QueryResult<ListTeamsOutput>> {
  getStepType(): string { return 'CompleteRainbondTeams'; }
  execute(context: Context, result: QueryResult<ListTeamsOutput>): StepDecision { teamsResult.set(context, result.value); return gracefulComplete(result.value); }
}
