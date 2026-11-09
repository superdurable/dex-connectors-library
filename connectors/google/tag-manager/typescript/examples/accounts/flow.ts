// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAccountsStep, type Connection, type ListAccountsInput, type AccountsPage } from '@superdurable/connector-google-tag-manager';

/** Static Dex Web connection for the Tag Manager example. */
export const ConnectionName = 'google-tag-manager';
const resultAttribute = new Attribute<AccountsPage>('google-tag-manager-accounts-page', jsonCodec<AccountsPage>());

/** List accounts visible to the connected Tag Manager user. */
export class AccountsFlow implements Flow<ListAccountsInput> {
  private readonly complete = new CompleteAccounts();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Tag Manager connection name differs');
    this.read = newListAccountsStep<ListAccountsInput>({
      stepType: 'CallListAccounts',
      annotations: { groupId: 'provider', groupLabel: 'Tag Manager', explanation: 'List accounts available to the connected Google user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleTagManagerAccountsTypeScript'; }
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
