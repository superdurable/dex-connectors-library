// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListThreadMessagesStep, type Connection, type ListThreadMessagesInput, type ListThreadMessagesOutput } from '@superdurable/connector-slack';

/** Static Dex Web connection name. */
export const ConnectionName = 'slack-thread-reader';
/** Safe input retained for inspecting the Flow. */
export const threadRequest = new Attribute<ListThreadMessagesInput>('slack-thread-request', jsonCodec<ListThreadMessagesInput>());
/** Safe result retained for inspecting the Flow. */
export const threadPage = new Attribute<ListThreadMessagesOutput>('slack-thread-page', jsonCodec<ListThreadMessagesOutput>());

/** Read one bounded Slack thread page. */
export class ListThreadMessagesFlow implements Flow<ListThreadMessagesInput> {
  private readonly prepare = new PrepareThreadRequest();
  private readonly complete = new CompleteThreadPage();
  /** Bind the named Slack connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow type. */ getFlowType(): string { return 'SlackListThreadMessagesFlow'; }
  /** Register the application and connector Steps. */
  getSteps(): StepList<ListThreadMessagesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListThreadMessagesStep({
        stepType: 'ListSlackThreadMessages',
        annotations: { groupId: 'slack-thread', groupLabel: 'Slack Thread', explanation: 'Read one bounded page of a Slack thread.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        read: this.complete,
      }), this.complete,
    );
  }
  /** Persist both safe Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [threadRequest, threadPage] }; }
}

/** Reject missing identifiers before the provider Step. */
// dex:group group-id:slack-thread group-label:"Slack Thread"
// dex:explanation text:"Validate the channel and thread selection before reading messages."
export class PrepareThreadRequest implements Step<ListThreadMessagesInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareSlackThreadRequest'; }
  /** Retain the safe request and start the provider Step. */
  execute(context: Context, input: ListThreadMessagesInput): StepDecision {
    if (!input.channelId.trim() || !input.threadTimestamp.trim() || input.pageSize < 1 || input.pageSize > 15) return forceFail('Slack channel, thread timestamp, and page size from 1 to 15 are required');
    threadRequest.set(context, input);
    return goTo(new StepRef('ListSlackThreadMessages'), input);
  }
}

/** Store the result and complete. */
// dex:group group-id:slack-thread group-label:"Slack Thread"
// dex:explanation text:"Persist the returned Slack thread page and complete the Flow."
export class CompleteThreadPage implements Step<QueryResult<ListThreadMessagesOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteSlackThreadPage'; }
  /** Complete with the bounded page. */
  execute(context: Context, result: QueryResult<ListThreadMessagesOutput>): StepDecision {
    threadPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
