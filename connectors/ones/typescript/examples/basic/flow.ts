// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListTeamsStep, type Connection, type ListTeamsInput, type ListTeamsOutput } from '@superdurable/connector-ones';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'ones';
const resultAttribute = new Attribute<ListTeamsOutput>('ones-result', jsonCodec<ListTeamsOutput>());

/** Query Ones through a typed connector Step. */
export class OnesInspectionFlow implements Flow<ListTeamsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Ones connection name differs');
    this.read = newListTeamsStep<ListTeamsInput>({
      stepType: 'CallListTeams',
      annotations: { groupId: 'provider', groupLabel: 'Ones', explanation: 'Read provider data through Ones.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteOnes'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'OnesInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListTeamsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListTeamsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOnes'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListTeamsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
