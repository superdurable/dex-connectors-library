// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAccountBalanceStep, type Connection, type GetAccountBalanceInput, type GetAccountBalanceOutput } from '@superdurable/connector-sendcloud';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'sendcloud';
const resultAttribute = new Attribute<GetAccountBalanceOutput>('sendcloud-result', jsonCodec<GetAccountBalanceOutput>());

/** Query SendCloud through a typed connector Step. */
export class SendCloudInspectionFlow implements Flow<GetAccountBalanceInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SendCloud connection name differs');
    this.read = newGetAccountBalanceStep<GetAccountBalanceInput>({
      stepType: 'CallGetAccountBalance',
      annotations: { groupId: 'provider', groupLabel: 'SendCloud', explanation: 'Read provider data through SendCloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      retrieved: new StepRef('CompleteSendCloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'SendCloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetAccountBalanceInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetAccountBalanceOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSendCloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetAccountBalanceOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
