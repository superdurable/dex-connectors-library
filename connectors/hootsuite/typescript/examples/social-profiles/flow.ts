// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSocialProfilesStep, type Connection, type ListSocialProfilesInput, type SocialProfilesPage } from '@superdurable/connector-hootsuite';

/** Static Dex Web connection for the Hootsuite example. */
export const ConnectionName = 'hootsuite';
const resultAttribute = new Attribute<SocialProfilesPage>('hootsuite-social-profiles-page', jsonCodec<SocialProfilesPage>());

/** List profiles visible to the connected Hootsuite user. */
export class SocialProfilesFlow implements Flow<ListSocialProfilesInput> {
  private readonly complete = new CompleteSocialProfiles();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Hootsuite connection name differs');
    this.read = newListSocialProfilesStep<ListSocialProfilesInput>({
      stepType: 'CallListSocialProfiles',
      annotations: { groupId: 'provider', groupLabel: 'Hootsuite', explanation: 'List connected social profiles using Hootsuite OAuth.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'HootsuiteSocialProfilesTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListSocialProfilesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the social profile page and complete the Flow."
class CompleteSocialProfiles implements Step<QueryResult<SocialProfilesPage>> {
  readonly inputCodec = jsonCodec<QueryResult<SocialProfilesPage>>();
  getStepType(): string { return 'CompleteSocialProfiles'; }
  execute(context: Context, result: QueryResult<SocialProfilesPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
