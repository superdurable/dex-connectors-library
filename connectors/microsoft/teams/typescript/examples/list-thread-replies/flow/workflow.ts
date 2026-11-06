// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListThreadRepliesStep, type Connection, type ListThreadRepliesInput, type ListThreadRepliesOutput } from '@superdurable/connector-microsoft-teams';

/** Static Dex Web connection name shared by Worker and starter. */
export const ConnectionName = 'microsoft-teams-incident';
/** Retained bounded page of thread replies. */
export const repliesResult = new Attribute<ListThreadRepliesOutput>('microsoft-teams-replies', jsonCodec<ListThreadRepliesOutput>());

/** Read one Teams thread page through a named OAuth connection. */
export class ThreadRepliesFlow implements Flow<ListThreadRepliesInput> {
  private readonly complete = new CompleteRepliesRead();
  /** Bind the static Teams connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection || connection.reference.name !== ConnectionName) throw new TypeError('Teams replies require the static connection');
  }
  /** Stable Flow type. */ getFlowType(): string { return 'MicrosoftTeamsListThreadRepliesTypeScript'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<ListThreadRepliesInput> {
    return StepList.startStep(newListThreadRepliesStep<ListThreadRepliesInput>({
      stepType: 'ReadMicrosoftTeamsThreadReplies',
      annotations: { groupId: 'teams-replies', groupLabel: 'Teams replies', explanation: 'Read one bounded page of a Teams thread.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: ListThreadRepliesInput) => input,
      read: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the reply page Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [repliesResult] }; }
}

/** Save the provider page and complete the Flow. */
// dex:group group-id:teams-replies group-label:"Teams replies"
// dex:explanation text:"Store one bounded page of Teams replies and complete the Flow."
export class CompleteRepliesRead implements Step<QueryResult<ListThreadRepliesOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteMicrosoftTeamsThreadReplies'; }
  /** Complete with the provider's reply page. */
  execute(context: Context, result: QueryResult<ListThreadRepliesOutput>): StepDecision {
    repliesResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
