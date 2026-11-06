// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAuthenticatedProfileStep, type Connection, type GetAuthenticatedProfileInput, type AuthenticatedProfile } from '@superdurable/connector-linkedin';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'linkedin-signup';
/** Persisted authenticated profile. */
export const profileAttribute = new Attribute<AuthenticatedProfile>('linkedin-authenticated-profile', jsonCodec<AuthenticatedProfile>());

/** Read the authenticated LinkedIn profile through the connector. */
export class AuthenticatedProfileFlow implements Flow<GetAuthenticatedProfileInput> {
  private readonly prepare = new PrepareAuthenticatedProfile();
  private readonly complete = new CompleteAuthenticatedProfile();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'LinkedInAuthenticatedProfileFlow'; }
  /** Register the profile query and completion Steps. */
  getSteps(): StepList<GetAuthenticatedProfileInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetAuthenticatedProfileStep({
        stepType: 'GetLinkedInAuthenticatedProfile',
        annotations: { groupId: 'linkedin-profile', groupLabel: 'LinkedIn Profile', explanation: 'Read the authenticated member UserInfo profile.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        profileLoaded: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted provider output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [profileAttribute] }; }
}

/** Validate a profile query before a provider call. */
// dex:group group-id:linkedin-profile group-label:"LinkedIn Profile"
// dex:explanation text:"Start the authenticated LinkedIn profile query."
export class PrepareAuthenticatedProfile implements Step<GetAuthenticatedProfileInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareLinkedInAuthenticatedProfile'; }
  /** Start the provider query with nonsecret input. */
  execute(_context: Context, input: GetAuthenticatedProfileInput): StepDecision {
    void input;
    return goTo(new StepRef('GetLinkedInAuthenticatedProfile'), input);
  }
}

/** Complete the Flow with the authenticated profile. */
// dex:group group-id:linkedin-profile group-label:"LinkedIn Profile"
// dex:explanation text:"Persist the bounded profile and complete the Flow."
export class CompleteAuthenticatedProfile implements Step<QueryResult<AuthenticatedProfile>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteLinkedInAuthenticatedProfile'; }
  /** Keep only safe provider metadata. */
  execute(context: Context, result: QueryResult<AuthenticatedProfile>): StepDecision {
    profileAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
