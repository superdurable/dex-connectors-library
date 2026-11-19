// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListObjectMetadataStep, type Connection, type ListObjectMetadataInput, type ListObjectMetadataOutput } from '@superdurable/connector-fxiaoke';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'fxiaoke';
const resultAttribute = new Attribute<ListObjectMetadataOutput>('fxiaoke-result', jsonCodec<ListObjectMetadataOutput>());

/** Query Fxiaoke through a typed connector Step. */
export class FxiaokeInspectionFlow implements Flow<ListObjectMetadataInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Fxiaoke connection name differs');
    this.read = newListObjectMetadataStep<ListObjectMetadataInput>({
      stepType: 'CallListObjectMetadata',
      annotations: { groupId: 'provider', groupLabel: 'Fxiaoke', explanation: 'Read provider data through Fxiaoke.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteFxiaoke'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'FxiaokeInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListObjectMetadataInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListObjectMetadataOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFxiaoke'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListObjectMetadataOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
