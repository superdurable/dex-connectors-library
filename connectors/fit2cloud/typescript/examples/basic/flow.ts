// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListPostsStep, type Connection, type ListPostsInput, type ListPostsOutput } from '@superdurable/connector-fit2cloud';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'fit2cloud';
const resultAttribute = new Attribute<ListPostsOutput>('fit2cloud-result', jsonCodec<ListPostsOutput>());

/** Query Fit2cloud through a typed connector Step. */
export class Fit2cloudInspectionFlow implements Flow<ListPostsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Fit2cloud connection name differs');
    this.read = newListPostsStep<ListPostsInput>({
      stepType: 'CallListPosts',
      annotations: { groupId: 'provider', groupLabel: 'Fit2cloud', explanation: 'Read provider data through Fit2cloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteFit2cloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'Fit2cloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListPostsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListPostsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFit2cloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListPostsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
