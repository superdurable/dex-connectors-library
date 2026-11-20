// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newTranslateTextStep, type Connection, type TranslateTextInput, type TextTranslation } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'youdao';
const resultAttribute = new Attribute<TextTranslation>('youdao-result', jsonCodec<TextTranslation>());

/** Query Youdao through a typed connector Step. */
export class YoudaoTranslateTextFlow implements Flow<TranslateTextInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Youdao connection name differs');
    this.read = newTranslateTextStep<TranslateTextInput>({
      stepType: 'CallTranslateText',
      annotations: { groupId: 'provider', groupLabel: 'Youdao', explanation: 'Translate text through Youdao.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      translated: new StepRef('CompleteYoudao'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'YoudaoTranslateTextTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<TranslateTextInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<TextTranslation>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteYoudao'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<TextTranslation>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
