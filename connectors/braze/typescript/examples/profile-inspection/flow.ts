// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetUserProfileStep, type Connection, type UserProfile, type GetUserProfileInput,
} from '@superdurable/connector-braze';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'braze';
const profileAttribute = new Attribute<UserProfile>('braze-profile', jsonCodec<UserProfile>());

/** Read one profile through a typed Braze Query. */
export class ProfileInspectionFlow implements Flow<GetUserProfileInput> {
  private readonly complete = new CompleteProfileInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Braze connection name differs');
    this.read = newGetUserProfileStep<GetUserProfileInput>({
      stepType: 'CallGetUserProfile',
      annotations: { groupId: 'provider', groupLabel: 'Braze', explanation: 'Read one profile from Braze.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteBraze'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'BrazeProfileInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetUserProfileInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected profile result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [profileAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected profile and complete the Flow."
class CompleteProfileInspection implements Step<QueryResult<UserProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteBraze'; }
  /** Persist and return the selected profile. */
  execute(context: Context, result: QueryResult<UserProfile>): StepDecision {
    profileAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
