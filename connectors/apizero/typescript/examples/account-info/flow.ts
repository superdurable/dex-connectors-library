// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAccountInfoStep, type Connection, type GetAccountInfoInput, type GetAccountInfoOutput } from '../../src/index.js';

/** Static Dex Web connection for the ApiZero example. */
export const ConnectionName = 'apizero';
const accountResult = new Attribute<GetAccountInfoOutput>('apizero-account-info-result', jsonCodec<GetAccountInfoOutput>());

/** Read the current Key's account or prepaid quota information. */
export class AccountInfoFlow implements Flow<GetAccountInfoInput> {
  private readonly start = new StartAccountInfo();
  private readonly complete = new CompleteAccountInfo();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('ApiZero connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'ApiZeroAccountInfo'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetAccountInfoInput> {
    return StepList.startStep(this.start).otherSteps(newGetAccountInfoStep({
      stepType: 'CallApiZero',
      annotations: { groupId: 'apizero', groupLabel: 'ApiZero', explanation: 'Read wallet balance or prepaid quota for this Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteApiZeroAccount'),
    }), this.complete);
  }
  /** Register the durable account information. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [accountResult] }; }
}

// dex:group group-id:apizero group-label:"ApiZero"
// dex:explanation text:"Start the account-information query."
class StartAccountInfo implements Step<GetAccountInfoInput> {
  getStepType(): string { return 'StartApiZeroAccount'; }
  execute(_context: Context, input: GetAccountInfoInput): StepDecision { return goTo(new StepRef('CallApiZero'), input); }
}

// dex:group group-id:apizero group-label:"ApiZero"
// dex:explanation text:"Persist the account information and complete the Flow."
class CompleteAccountInfo implements Step<QueryResult<GetAccountInfoOutput>> {
  getStepType(): string { return 'CompleteApiZeroAccount'; }
  execute(context: Context, result: QueryResult<GetAccountInfoOutput>): StepDecision { accountResult.set(context, result.value); return gracefulComplete(result.value); }
}
