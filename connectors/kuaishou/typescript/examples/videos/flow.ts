// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListUserVideosStep, type Connection, type UserVideosPage, type ListUserVideosInput } from '../../src/index.js';

/** Static Dex Web connection for the Kuaishou example. */
export const ConnectionName = 'kuaishou';
const videosResult = new Attribute<UserVideosPage>('kuaishou-videos-result', jsonCodec<UserVideosPage>());

/** Read one city's current videos through Kuaishou. */
export class UserVideosFlow implements Flow<ListUserVideosInput> {
  private readonly start = new StartVideos();
  private readonly complete = new CompleteVideos();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Kuaishou connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'KuaishouUserVideos'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListUserVideosInput> {
    return StepList.startStep(this.start).otherSteps(newListUserVideosStep({
      stepType: 'CallKuaishouVideos',
      annotations: { groupId: 'kuaishou', groupLabel: 'Kuaishou', explanation: 'Read the current observations for one city.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteKuaishouVideos'),
    }), this.complete);
  }
  /** Register the durable videos observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [videosResult] }; }
}

// dex:group group-id:kuaishou group-label:"Kuaishou"
// dex:explanation text:"Start the videos query."
class StartVideos implements Step<ListUserVideosInput> {
  getStepType(): string { return 'StartKuaishouVideos'; }
  execute(_context: Context, input: ListUserVideosInput): StepDecision { return goTo(new StepRef('CallKuaishouVideos'), input); }
}

// dex:group group-id:kuaishou group-label:"Kuaishou"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteVideos implements Step<QueryResult<UserVideosPage>> {
  getStepType(): string { return 'CompleteKuaishouVideos'; }
  execute(context: Context, result: QueryResult<UserVideosPage>): StepDecision { videosResult.set(context, result.value); return gracefulComplete(result.value); }
}
