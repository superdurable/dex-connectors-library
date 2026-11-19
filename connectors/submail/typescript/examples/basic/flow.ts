// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetSMSBalanceStep, type Connection, type GetSMSBalanceInput, type SMSBalance } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'submail';
const resultAttribute = new Attribute<SMSBalance>('submail-result', jsonCodec<SMSBalance>());

/** Query Submail through a typed connector Step. */
export class SubmailInspectionFlow implements Flow<GetSMSBalanceInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Submail connection name differs');
    this.read = newGetSMSBalanceStep<GetSMSBalanceInput>({
      stepType: 'CallGetSMSBalance',
      annotations: { groupId: 'provider', groupLabel: 'Submail', explanation: 'Read provider data through Submail.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteSubmail'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'SubmailInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetSMSBalanceInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<SMSBalance>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSubmail'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<SMSBalance>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
