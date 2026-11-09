// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListTopicsStep, type Connection, type TopicPage, type ListTopicsInput,
} from '@superdurable/connector-amazon-sns';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'amazon-sns';
const topicPageAttribute = new Attribute<TopicPage>('sns-topic-listing', jsonCodec<TopicPage>());

/** Read one page of topic ARNs through a typed Amazon SNS Query. */
export class TopicListingFlow implements Flow<ListTopicsInput> {
  private readonly complete = new CompleteTopicPage();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Amazon SNS connection name differs');
    this.read = newListTopicsStep<ListTopicsInput>({
      stepType: 'CallListTopics',
      annotations: { groupId: 'provider', groupLabel: 'Amazon SNS', explanation: 'Read one page of topic ARNs from Amazon SNS.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      page: new StepRef('CompleteListTopics'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AmazonSNSTopicListingTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListTopicsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected page of topic ARNs result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [topicPageAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected page of topic ARNs and complete the Flow."
class CompleteTopicPage implements Step<QueryResult<TopicPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteListTopics'; }
  /** Persist and return the selected page of topic ARNs. */
  execute(context: Context, result: QueryResult<TopicPage>): StepDecision {
    topicPageAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
