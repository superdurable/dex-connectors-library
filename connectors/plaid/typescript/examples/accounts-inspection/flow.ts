// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetAccountsStep, type Connection, type AccountsSnapshot, type GetAccountsInput,
} from '@superdurable/connector-plaid';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'plaid';
const accountsAttribute = new Attribute<AccountsSnapshot>('plaid-accounts', jsonCodec<AccountsSnapshot>());

/** Read the linked accounts through a typed Plaid Query. */
export class AccountsInspectionFlow implements Flow<GetAccountsInput> {
  private readonly complete = new CompleteAccountsInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Plaid connection name differs');
    this.read = newGetAccountsStep<GetAccountsInput>({
      stepType: 'CallGetAccounts',
      annotations: { groupId: 'provider', groupLabel: 'Plaid', explanation: 'Read linked accounts from Plaid.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompletePlaid'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'PlaidAccountsInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetAccountsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected accounts result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [accountsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected accounts and complete the Flow."
class CompleteAccountsInspection implements Step<QueryResult<AccountsSnapshot>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePlaid'; }
  /** Persist and return the selected accounts. */
  execute(context: Context, result: QueryResult<AccountsSnapshot>): StepDecision {
    accountsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
