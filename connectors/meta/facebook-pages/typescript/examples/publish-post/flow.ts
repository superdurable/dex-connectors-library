// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newPublishTextPostStep, type Connection, type PublishTextPostInput, type PublishedPost } from '@superdurable/connector-meta-facebook-pages';

/** Static Dex Web connection for the Facebook Pages example. */
export const ConnectionName = 'facebook-pages';
const resultAttribute = new Attribute<PublishedPost>('facebook-pages-published-post', jsonCodec<PublishedPost>());

/** Publish one plain-text post with the connected Page token. */
export class PublishPostFlow implements Flow<PublishTextPostInput> {
  private readonly complete = new CompletePublishPost();
  private readonly publish;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Facebook Pages connection name differs');
    this.publish = newPublishTextPostStep<PublishTextPostInput>({
      stepType: 'CallPublishTextPost',
      annotations: { groupId: 'provider', groupLabel: 'Facebook Pages', explanation: 'Publish a plain-text Page post using the Page token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, published: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FacebookPagesPublishPost'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<PublishTextPostInput> {
    return StepList.startStep(this.publish).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the published post ID and complete the Flow."
class CompletePublishPost implements Step<MutationResult<PublishedPost>> {
  readonly inputCodec = jsonCodec<MutationResult<PublishedPost>>();
  getStepType(): string { return 'CompletePublishTextPost'; }
  execute(context: Context, result: MutationResult<PublishedPost>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
