// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAdAccountsStep, type Connection, type ListAdAccountsInput, type AdAccountsPage } from '@superdurable/connector-linkedin-ads';

/** Static Dex Web connection for the Accounts example. */
export const ConnectionName = 'linkedin-ads';
const resultAttribute = new Attribute<AdAccountsPage>('linkedin-ads-typescript-page', jsonCodec<AdAccountsPage>());

/** List ad accounts visible to the authorized LinkedIn member. */
export class AccountsFlow implements Flow<ListAdAccountsInput> {
  private readonly complete = new CompleteAccounts();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Accounts connection name differs');
    this.read = newListAdAccountsStep<ListAdAccountsInput>({
      stepType: 'CallListAdAccounts',
      annotations: { groupId: 'provider', groupLabel: 'LinkedIn Ads', explanation: 'List ad accounts visible to the authorized LinkedIn member.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'LinkedInAdsAccountsListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListAdAccountsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the accounts and complete the Flow."
class CompleteAccounts implements Step<QueryResult<AdAccountsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<AdAccountsPage>>();
  getStepType(): string { return 'CompleteAccounts'; }
  execute(context: Context, result: QueryResult<AdAccountsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
