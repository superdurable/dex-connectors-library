// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListPostsStep, type Connection, type ListPostsInput, type PagePostsPage } from '@superdurable/connector-meta-facebook-pages';

/** Static Dex Web connection for the Facebook Pages example. */
export const ConnectionName = 'facebook-pages';
const resultAttribute = new Attribute<PagePostsPage>('facebook-pages-posts-page', jsonCodec<PagePostsPage>());

/** List Page feed posts visible to the connected Page token. */
export class PagePostsFlow implements Flow<ListPostsInput> {
  private readonly complete = new CompletePagePosts();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Facebook Pages connection name differs');
    this.read = newListPostsStep<ListPostsInput>({
      stepType: 'CallListPosts',
      annotations: { groupId: 'provider', groupLabel: 'Facebook Pages', explanation: 'List Page feed post IDs and messages using the Page token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FacebookPagesPostFeed'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListPostsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the post page and complete the Flow."
class CompletePagePosts implements Step<QueryResult<PagePostsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<PagePostsPage>>();
  getStepType(): string { return 'CompleteListPosts'; }
  execute(context: Context, result: QueryResult<PagePostsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
