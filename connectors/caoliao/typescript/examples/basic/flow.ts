// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListQRCodesStep, type Connection, type ListQRCodesInput, type ListQRCodesOutput } from '@superdurable/connector-caoliao';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'caoliao';
const resultAttribute = new Attribute<ListQRCodesOutput>('caoliao-result', jsonCodec<ListQRCodesOutput>());

/** Query Caoliao through a typed connector Step. */
export class CaoliaoInspectionFlow implements Flow<ListQRCodesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Caoliao connection name differs');
    this.read = newListQRCodesStep<ListQRCodesInput>({
      stepType: 'CallListQRCodes',
      annotations: { groupId: 'provider', groupLabel: 'Caoliao', explanation: 'Read provider data through Caoliao.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteCaoliao'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'CaoliaoInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListQRCodesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListQRCodesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteCaoliao'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListQRCodesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
