// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetUserStep, type Connection, type GetUserInput, type User } from '@superdurable/connector-google-workspace-admin';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'workspace-admin-directory';
/** Persist the account returned by the Directory API. */
export const account = new Attribute<User>('workspace-admin-account', jsonCodec<User>());

/** Read one account through a named Google Workspace Admin connection. */
export class GetWorkspaceUserFlow implements Flow<GetUserInput> {
  private readonly complete = new CompleteGetWorkspaceUser();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'GetWorkspaceUserFlow'; }
  /** Register the query and completion Steps. */
  getSteps(): StepList<GetUserInput> {
    return StepList.startStep(newGetUserStep<GetUserInput>({
      stepType: 'GetWorkspaceUser',
      annotations: { groupId: 'workspace-user-read', groupLabel: 'Workspace user read', explanation: 'Read one Google Workspace account.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted account. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [account] }; }
}

// dex:group group-id:workspace-user-read group-label:"Workspace user read"
// dex:explanation text:"Persist the Directory account and complete the Flow."
/** Complete the account read. */
export class CompleteGetWorkspaceUser implements Step<QueryResult<User>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteGetWorkspaceUser'; }
  /** Store the nonsecret account record. */
  execute(context: Context, result: QueryResult<User>): StepDecision { account.set(context, result.value); return gracefulComplete(result.value); }
}
