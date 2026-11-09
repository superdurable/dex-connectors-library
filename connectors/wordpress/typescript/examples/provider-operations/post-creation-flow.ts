// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newCreatePostStep, type Connection, type CreatePostInput, type CreatedPost } from '@superdurable/connector-wordpress';

export const ConnectionName = 'wordpress';
const resultAttribute = new Attribute<CreatedPost>('wordpress-typescript-created-post', jsonCodec<CreatedPost>());

/** Create one WordPress draft or published post. */
export class PostCreationFlow implements Flow<CreatePostInput> {
  private readonly complete = new CompleteWordPressPostCreation();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('WordPress connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WordPressTypeScriptPostCreationFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<CreatePostInput> {
    return StepList.startStep(newCreatePostStep<CreatePostInput>({
      stepType: 'CallWordPressCreatePost',
      annotations: { groupId: 'post-creation', groupLabel: 'Post creation',
        explanation: 'Create one WordPress draft or published post.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, created: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:post-creation group-label:"Post creation"
// dex:explanation text:"Persist the confirmed WordPress result and complete the Flow."
export class CompleteWordPressPostCreation implements Step<MutationResult<CreatedPost>> {
  readonly inputCodec = jsonCodec<MutationResult<CreatedPost>>();
  getStepType(): string { return 'CompleteWordPressPostCreation'; }
  execute(context: Context, result: MutationResult<CreatedPost>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
