// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetMessageStep, type Connection, type GetMessageInput, type Message,
} from '@superdurable/connector-microsoft-outlook-mail';

/** Static Outlook Mail connection name used by the Worker and the starter. */
export const ConnectionName = 'outlook-mail';
/** The safe message ID retained for Flow inspection. */
export const messageRequest = new Attribute<GetMessageInput>('outlook-mail-message-request', jsonCodec<GetMessageInput>());
/** The read result retained for Flow inspection. */
export const messageResult = new Attribute<Message>('outlook-mail-message-result', jsonCodec<Message>());

/** Read one Outlook message in a real Dex Flow. */
export class OutlookMessageFlow implements Flow<GetMessageInput> {
  private readonly prepare = new PrepareMessageRead();
  private readonly complete = new CompleteMessageRead();

  /** Bind the named Outlook Mail connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Outlook Mail connection is required'); }
  /** Stable Dex Flow type. */
  getFlowType(): string { return 'OutlookMessageFlow'; }
  /** Register the application and provider Steps. */
  getSteps(): StepList<GetMessageInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetMessageStep({
        stepType: 'ReadOutlookMessage',
        annotations: { groupId: 'outlook-mail', groupLabel: 'Outlook Mail', explanation: 'Read one Outlook message without marking it read.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }), this.complete,
    );
  }
  /** Register persisted Attributes. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [messageRequest, messageResult] }; }
}

/** Validate and persist the requested message ID. */
// dex:group group-id:outlook-mail group-label:"Outlook Mail"
// dex:explanation text:"Validate the message ID before reading Outlook Mail."
export class PrepareMessageRead implements Step<GetMessageInput> {
  /** Stable Dex Step type. */
  getStepType(): string { return 'PrepareOutlookMessageRead'; }
  /** Start the provider read. */
  execute(context: Context, input: GetMessageInput): StepDecision {
    if (!input.messageId?.trim()) return forceFail('messageId is required');
    messageRequest.set(context, input);
    return goTo(new StepRef('ReadOutlookMessage'), input);
  }
}

/** Save the provider result as a Dex Attribute. */
// dex:group group-id:outlook-mail group-label:"Outlook Mail"
// dex:explanation text:"Persist the Outlook message and complete the Flow."
export class CompleteMessageRead implements Step<QueryResult<Message>> {
  /** Stable Dex Step type. */
  getStepType(): string { return 'CompleteOutlookMessageRead'; }
  /** Save and complete. */
  execute(context: Context, result: QueryResult<Message>): StepDecision {
    messageResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
