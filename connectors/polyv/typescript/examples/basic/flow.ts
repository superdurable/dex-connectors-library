// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListChannelsStep, type Connection, type ListChannelsInput, type ListChannelsOutput } from '@superdurable/connector-polyv';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'polyv';
const resultAttribute = new Attribute<ListChannelsOutput>('polyv-result', jsonCodec<ListChannelsOutput>());

/** Query Polyv through a typed connector Step. */
export class PolyvInspectionFlow implements Flow<ListChannelsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Polyv connection name differs');
    this.read = newListChannelsStep<ListChannelsInput>({
      stepType: 'CallListChannels',
      annotations: { groupId: 'provider', groupLabel: 'Polyv', explanation: 'Read provider data through Polyv.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompletePolyv'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'PolyvInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListChannelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListChannelsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePolyv'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListChannelsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
