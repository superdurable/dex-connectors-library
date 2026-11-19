// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAuthorizedUserStep, type Connection, type GetAuthorizedUserInput, type AuthorizedUser } from '@superdurable/connector-bilibili';

/** Static Dex Web connection name. */
export const ConnectionName = 'bilibili';
export const authorizedUserAttribute = new Attribute('bilibili-authorized-user', jsonCodec<AuthorizedUser>());

/** Read and persist the identity of the authorized Bilibili user. */
export class BilibiliAuthorizedUserFlow implements Flow<GetAuthorizedUserInput> {
  private readonly completed = new AuthorizedUserFound();
  private readonly lookup: Step<GetAuthorizedUserInput>;
  /** Bind the named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('authorized user Flow requires the bilibili connection');
    this.lookup = newGetAuthorizedUserStep<GetAuthorizedUserInput>({
      stepType: 'GetBilibiliAuthorizedUser',
      annotations: { groupId: 'bilibili', groupLabel: 'Bilibili', explanation: 'Read the public identity attached to the authorized Bilibili token.' },
      connection, connectionName: ConnectionName, mapToOperationInput: request => request,
      found: new StepRef('AuthorizedUserFound'),
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'BilibiliAuthorizedUser'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<GetAuthorizedUserInput> { return StepList.startStep(this.lookup).otherSteps(this.completed); }
  /** Register the persisted account identity. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [authorizedUserAttribute] }; }
}

// dex:group group-id:bilibili group-label:"Bilibili"
// dex:explanation text:"Persist the authorized account identity and complete the Flow."
class AuthorizedUserFound implements Step<QueryResult<AuthorizedUser>> {
  getStepType(): string { return 'AuthorizedUserFound'; }
  execute(context: Context, result: QueryResult<AuthorizedUser>): StepDecision {
    authorizedUserAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
