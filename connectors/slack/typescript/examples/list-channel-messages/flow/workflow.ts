// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListChannelMessagesStep, type Connection, type ListChannelMessagesInput, type ListChannelMessagesOutput } from '@superdurable/connector-slack';

/** Static Dex Web connection name. */
export const ConnectionName = 'slack-channel-history-reader';
/** Safe input retained for inspecting the Flow. */
export const channelHistoryRequest = new Attribute<ListChannelMessagesInput>('slack-channel-history-request', jsonCodec<ListChannelMessagesInput>());
/** Safe result retained for inspecting the Flow. */
export const channelHistoryPage = new Attribute<ListChannelMessagesOutput>('slack-channel-history-page', jsonCodec<ListChannelMessagesOutput>());

/** Read one bounded, newest-first page of a Slack channel's top-level messages. */
export class ListChannelMessagesFlow implements Flow<ListChannelMessagesInput> {
  private readonly prepare = new PrepareChannelHistoryRequest();
  private readonly complete = new CompleteChannelHistoryPage();
  /** Bind the named Slack connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow type. */ getFlowType(): string { return 'SlackListChannelMessagesFlow'; }
  /** Register the application and connector Steps. */
  getSteps(): StepList<ListChannelMessagesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListChannelMessagesStep({
        stepType: 'ListSlackChannelMessages',
        annotations: { groupId: 'slack-channel', groupLabel: 'Slack Channel', explanation: 'Read one bounded, newest-first page of channel messages.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        read: this.complete,
      }), this.complete,
    );
  }
  /** Persist both safe Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [channelHistoryRequest, channelHistoryPage] }; }
}

/** Reject a missing channel before the provider Step. */
// dex:group group-id:slack-channel group-label:"Slack Channel"
// dex:explanation text:"Validate the channel selection before reading its history."
export class PrepareChannelHistoryRequest implements Step<ListChannelMessagesInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareSlackChannelHistoryRequest'; }
  /** Retain the safe request and start the provider Step. */
  execute(context: Context, input: ListChannelMessagesInput): StepDecision {
    if (!input.channelId?.trim()) return forceFail('Slack channel is required');
    channelHistoryRequest.set(context, input);
    return goTo(new StepRef('ListSlackChannelMessages'), input);
  }
}

/** Store the result and complete. */
// dex:group group-id:slack-channel group-label:"Slack Channel"
// dex:explanation text:"Persist the returned Slack channel history page and complete the Flow."
export class CompleteChannelHistoryPage implements Step<QueryResult<ListChannelMessagesOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteSlackChannelHistoryPage'; }
  /** Complete with the bounded page. */
  execute(context: Context, result: QueryResult<ListChannelMessagesOutput>): StepDecision {
    channelHistoryPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
