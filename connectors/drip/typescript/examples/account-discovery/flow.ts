// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAccountsStep, type Connection, type ListAccountsInput, type AccountsList } from '@superdurable/connector-drip';

/** Static Dex Web connection for the Drip example. */
export const ConnectionName = 'drip';
const resultAttribute = new Attribute<AccountsList>('drip-accounts-list', jsonCodec<AccountsList>());

/** List accounts visible to the connected Drip user. */
export class AccountDiscoveryFlow implements Flow<ListAccountsInput> {
  private readonly complete = new CompleteAccountDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Drip connection name differs');
    this.read = newListAccountsStep<ListAccountsInput>({
      stepType: 'CallListAccounts',
      annotations: { groupId: 'provider', groupLabel: 'Drip', explanation: 'List accounts accessible to the connected user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'DripAccountDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListAccountsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the account list and complete the Flow."
class CompleteAccountDiscovery implements Step<QueryResult<AccountsList>> {
  readonly inputCodec = jsonCodec<QueryResult<AccountsList>>();
  getStepType(): string { return 'CompleteListAccounts'; }
  execute(context: Context, result: QueryResult<AccountsList>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
