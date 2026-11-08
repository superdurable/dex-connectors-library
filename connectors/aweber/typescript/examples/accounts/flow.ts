// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAccountsStep, type Connection, type ListAccountsInput, type AccountsPage } from '@superdurable/connector-aweber';

/** Static Dex Web connection for the Accounts example. */
export const ConnectionName = 'aweber';
const resultAttribute = new Attribute<AccountsPage>('aweber-typescript-page', jsonCodec<AccountsPage>());

/** List accounts visible to the authorized AWeber customer. */
export class AccountsFlow implements Flow<ListAccountsInput> {
  private readonly complete = new CompleteAccounts();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Accounts connection name differs');
    this.read = newListAccountsStep<ListAccountsInput>({
      stepType: 'CallListAccounts',
      annotations: { groupId: 'provider', groupLabel: 'AWeber', explanation: 'List accounts available to the authorized AWeber customer.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'AWeberAccountsListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListAccountsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the accounts and complete the Flow."
class CompleteAccounts implements Step<QueryResult<AccountsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<AccountsPage>>();
  getStepType(): string { return 'CompleteAccounts'; }
  execute(context: Context, result: QueryResult<AccountsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
