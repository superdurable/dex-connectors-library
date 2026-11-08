// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newSearchUsersStep, type Connection, type UserSearchPage, type SearchUsersInput,
} from '@superdurable/connector-amplitude';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'amplitude';
const usersAttribute = new Attribute<UserSearchPage>('amplitude-user-search', jsonCodec<UserSearchPage>());

/** Read user IDs through a typed Amplitude Query. */
export class UserSearchFlow implements Flow<SearchUsersInput> {
  private readonly complete = new CompleteUserSearch();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Amplitude connection name differs');
    this.read = newSearchUsersStep<SearchUsersInput>({
      stepType: 'CallSearchUsers',
      annotations: { groupId: 'provider', groupLabel: 'Amplitude', explanation: 'Read user IDs from Amplitude.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      searched: new StepRef('CompleteAmplitude'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AmplitudeUserSearchTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<SearchUsersInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the user search results result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [usersAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the user search results and complete the Flow."
class CompleteUserSearch implements Step<QueryResult<UserSearchPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteAmplitude'; }
  /** Persist and return the user search results. */
  execute(context: Context, result: QueryResult<UserSearchPage>): StepDecision {
    usersAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
