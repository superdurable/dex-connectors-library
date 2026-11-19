// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGenerateTextStep, type Connection, type GenerateTextInput, type GenerateTextOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'qihoo360';
const resultAttribute = new Attribute<GenerateTextOutput>('qihoo360-result', jsonCodec<GenerateTextOutput>());

/** Query Qihoo360 through a typed connector Step. */
export class Qihoo360GenerateTextFlow implements Flow<GenerateTextInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Qihoo360 connection name differs');
    this.read = newGenerateTextStep<GenerateTextInput>({
      stepType: 'CallGenerateText',
      annotations: { groupId: 'provider', groupLabel: 'Qihoo360', explanation: 'Read provider data through Qihoo360.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      generated: new StepRef('CompleteQihoo360'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'Qihoo360GenerateTextTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GenerateTextInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GenerateTextOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteQihoo360'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GenerateTextOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
