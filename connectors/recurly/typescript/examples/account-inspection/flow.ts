// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetAccountStep, type Connection, type AccountIdentity, type GetAccountInput,
} from '@superdurable/connector-recurly';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'recurly';
const accountAttribute = new Attribute<AccountIdentity>('recurly-account', jsonCodec<AccountIdentity>());

/** Read one account through a typed Recurly Query. */
export class AccountInspectionFlow implements Flow<GetAccountInput> {
  private readonly complete = new CompleteAccountInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Recurly connection name differs');
    this.read = newGetAccountStep<GetAccountInput>({
      stepType: 'CallGetAccount',
      annotations: { groupId: 'provider', groupLabel: 'Recurly', explanation: 'Read one account from Recurly.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteRecurly'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'RecurlyAccountInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetAccountInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected account result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [accountAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected account and complete the Flow."
class CompleteAccountInspection implements Step<QueryResult<AccountIdentity>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteRecurly'; }
  /** Persist and return the selected account. */
  execute(context: Context, result: QueryResult<AccountIdentity>): StepDecision {
    accountAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
