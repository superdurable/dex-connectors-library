// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListContractsStep, type Connection, type ListContractsInput, type ListContractsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'qiyuesuo';
const resultAttribute = new Attribute<ListContractsOutput>('qiyuesuo-result', jsonCodec<ListContractsOutput>());

/** Query Qiyuesuo through a typed connector Step. */
export class QiyuesuoInspectionFlow implements Flow<ListContractsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Qiyuesuo connection name differs');
    this.read = newListContractsStep<ListContractsInput>({
      stepType: 'CallListContracts',
      annotations: { groupId: 'provider', groupLabel: 'Qiyuesuo', explanation: 'Read provider data through Qiyuesuo.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteQiyuesuo'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'QiyuesuoInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListContractsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListContractsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteQiyuesuo'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListContractsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
