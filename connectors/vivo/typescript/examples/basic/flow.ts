// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetOnlineVersionStep, type Connection, type GetOnlineVersionInput, type GetOnlineVersionOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'vivo';
const resultAttribute = new Attribute<GetOnlineVersionOutput>('vivo-result', jsonCodec<GetOnlineVersionOutput>());

/** Query Vivo through a typed connector Step. */
export class VivoInspectionFlow implements Flow<GetOnlineVersionInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Vivo connection name differs');
    this.read = newGetOnlineVersionStep<GetOnlineVersionInput>({
      stepType: 'CallGetOnlineVersion',
      annotations: { groupId: 'provider', groupLabel: 'Vivo', explanation: 'Read provider data through Vivo.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteVivo'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'VivoInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetOnlineVersionInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetOnlineVersionOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteVivo'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetOnlineVersionOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
