// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetParseStatusStep, type Connection, type GetParseStatusInput, type GetParseStatusOutput } from '@superdurable/connector-doc2x';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'doc2x';
const resultAttribute = new Attribute<GetParseStatusOutput>('doc2x-result', jsonCodec<GetParseStatusOutput>());

/** Query Doc2x through a typed connector Step. */
export class Doc2xInspectionFlow implements Flow<GetParseStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Doc2x connection name differs');
    this.read = newGetParseStatusStep<GetParseStatusInput>({
      stepType: 'CallGetParseStatus',
      annotations: { groupId: 'provider', groupLabel: 'Doc2X', explanation: 'Read provider data through Doc2x.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteDoc2x'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'Doc2xInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetParseStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetParseStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteDoc2x'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetParseStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
