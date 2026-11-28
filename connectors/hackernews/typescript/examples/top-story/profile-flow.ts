// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetUserStep, type Connection, type User } from '@superdurable/connector-hackernews';

const ConnectionName = 'hackernews-public';

/** Select the public Hacker News username to read. */
export interface ProfileRequest { username: string }

/** Read one public profile and complete with the provider's public fields. */
export class ProfileFlow implements Flow<ProfileRequest> {
  private readonly complete = new ReturnProfile();
  private readonly read;

  /** Bind the Worker's static public connection. */
  constructor(connection: Connection) {
    this.read = newGetUserStep<ProfileRequest>({
      stepType: 'ReadHackerNewsProfile',
      annotations: { groupId: 'profile', groupLabel: 'Public profile', explanation: 'Read the selected user and submitted item IDs.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => ({ id: input.username }),
      found: new StepRef('ReturnHackerNewsProfile'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'HackerNewsProfileTypeScript'; }
  /** Register the provider read and completion Steps. */ getSteps(): StepList<ProfileRequest> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
}

// dex:group group-id:profile group-label:"Public profile"
// dex:explanation text:"Return the public profile and its canonical permalink."
class ReturnProfile implements Step<QueryResult<User>> {
  readonly inputCodec = jsonCodec<QueryResult<User>>();
  getStepType(): string { return 'ReturnHackerNewsProfile'; }
  execute(_context: Context, input: QueryResult<User>): StepDecision {
    return gracefulComplete(input.value);
  }
}
