// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetPostStep, type Connection, type GetPostInput, type Post } from '@superdurable/connector-reddit';

/** Static Dex Web connection for the Post example. */
export const ConnectionName = 'reddit';
const resultAttribute = new Attribute<Post>('reddit-typescript-post', jsonCodec<Post>());

/** Read the requested Reddit post. */
export class PostFlow implements Flow<GetPostInput> {
  private readonly complete = new CompletePost();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Post connection name differs');
    this.read = newGetPostStep<GetPostInput>({
      stepType: 'CallGetPost',
      annotations: { groupId: 'provider', groupLabel: 'Reddit', explanation: 'Read the requested Reddit post.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompletePost'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'RedditPostLookupTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetPostInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the post and complete the Flow."
class CompletePost implements Step<QueryResult<Post>> {
  readonly inputCodec = jsonCodec<QueryResult<Post>>();
  getStepType(): string { return 'CompletePost'; }
  execute(context: Context, result: QueryResult<Post>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
