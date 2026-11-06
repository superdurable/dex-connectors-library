// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, forceComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetUserStep, type Connection, type GetUserInput, type User } from '@superdurable/connector-microsoft-entra-id';

/** Static Dex Web connection name used by the example. */
export const ConnectionName = 'entra-directory';

/** Read one Entra account through a real Dex Worker. */
export class UserLookupFlow implements Flow<GetUserInput> {
  private readonly completed = new LookupCompletedStep();

  /** Bind the statically named Microsoft connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Entra connection is required'); }

  /** Stable Flow identity. */
  getFlowType(): string { return 'EntraUserLookupExample'; }
  /** Register the provider Step and completion Step. */
  getSteps() { return StepList.startStep(newGetUserStep<GetUserInput>({ stepType: 'LookupEntraUser', connectionName: ConnectionName, connection: this.connection,
    annotations: { groupId: 'directory', groupLabel: 'Directory', explanation: 'Read the selected Microsoft Entra account.' },
    mapToOperationInput: input => input, found: new StepRef('CompleteEntraUserLookup') })).otherSteps(this.completed); }
}

// dex:group group-id:directory group-label:"Directory"
// dex:explanation text:"Complete after Microsoft Entra returns the account."
class LookupCompletedStep implements Step<QueryResult<User>> {
  getStepType(): string { return 'CompleteEntraUserLookup'; }
  execute(_context: Context, result: QueryResult<User>): StepDecision { return forceComplete(result); }
}
