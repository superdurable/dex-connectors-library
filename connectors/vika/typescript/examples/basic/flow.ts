// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSpacesStep, type Connection, type ListSpacesInput, type SpacePage } from '@superdurable/connector-vika';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'vika';
const resultAttribute = new Attribute<SpacePage>('vika-result', jsonCodec<SpacePage>());

/** Query Vika through a typed connector Step. */
export class VikaInspectionFlow implements Flow<ListSpacesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Vika connection name differs');
    this.read = newListSpacesStep<ListSpacesInput>({
      stepType: 'CallListSpaces',
      annotations: { groupId: 'provider', groupLabel: 'Vika', explanation: 'Read provider data through Vika.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteVika'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'VikaInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListSpacesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<SpacePage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteVika'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<SpacePage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
