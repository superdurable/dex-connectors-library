// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetUserStep, type Connection, type UserIdentity, type GetUserInput,
} from '@superdurable/connector-iterable';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'iterable';
const userAttribute = new Attribute<UserIdentity>('iterable-user', jsonCodec<UserIdentity>());

/** Read one user through a typed Iterable Query. */
export class UserInspectionFlow implements Flow<GetUserInput> {
  private readonly complete = new CompleteUserInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Iterable connection name differs');
    this.read = newGetUserStep<GetUserInput>({
      stepType: 'CallGetUser',
      annotations: { groupId: 'provider', groupLabel: 'Iterable', explanation: 'Read one user from Iterable.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteIterable'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'IterableUserInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetUserInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected user result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [userAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected user and complete the Flow."
class CompleteUserInspection implements Step<QueryResult<UserIdentity>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteIterable'; }
  /** Persist and return the selected user. */
  execute(context: Context, result: QueryResult<UserIdentity>): StepDecision {
    userAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
