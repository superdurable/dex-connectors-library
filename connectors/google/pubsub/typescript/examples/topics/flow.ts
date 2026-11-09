// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListTopicsStep, type Connection, type ListTopicsInput, type TopicsPage } from '@superdurable/connector-google-pubsub';

/** Static Dex Web connection for the Topics example. */
export const ConnectionName = 'google-pubsub';
const resultAttribute = new Attribute<TopicsPage>('google-pubsub-typescript-page', jsonCodec<TopicsPage>());

/** List topics visible to the authorized Google identity. */
export class TopicsFlow implements Flow<ListTopicsInput> {
  private readonly complete = new CompleteTopics();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Topics connection name differs');
    this.read = newListTopicsStep<ListTopicsInput>({
      stepType: 'CallListTopics',
      annotations: { groupId: 'provider', groupLabel: 'Pub/Sub', explanation: 'List topics visible in the selected Google Cloud project.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'PubSubTopicsListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListTopicsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the topics and complete the Flow."
class CompleteTopics implements Step<QueryResult<TopicsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<TopicsPage>>();
  getStepType(): string { return 'CompleteTopics'; }
  execute(context: Context, result: QueryResult<TopicsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
