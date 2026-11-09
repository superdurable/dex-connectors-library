// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetPostStep, type Connection, type GetPostInput, type Post } from '@superdurable/connector-wordpress';

export const ConnectionName = 'wordpress';
const resultAttribute = new Attribute<Post>('wordpress-typescript-published-post', jsonCodec<Post>());

/** Read one published WordPress post. */
export class PostLookupFlow implements Flow<GetPostInput> {
  private readonly complete = new CompleteWordPressPostLookup();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('WordPress connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WordPressTypeScriptPostLookupFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetPostInput> {
    return StepList.startStep(newGetPostStep<GetPostInput>({
      stepType: 'CallWordPressGetPost',
      annotations: { groupId: 'post-lookup', groupLabel: 'Post lookup',
        explanation: 'Read one published WordPress post.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:post-lookup group-label:"Post lookup"
// dex:explanation text:"Persist the confirmed WordPress result and complete the Flow."
export class CompleteWordPressPostLookup implements Step<QueryResult<Post>> {
  readonly inputCodec = jsonCodec<QueryResult<Post>>();
  getStepType(): string { return 'CompleteWordPressPostLookup'; }
  execute(context: Context, result: QueryResult<Post>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
