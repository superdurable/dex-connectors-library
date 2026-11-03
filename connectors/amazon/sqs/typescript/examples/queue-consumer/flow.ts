// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newChangeMessageVisibilityStep, newDeleteMessageStep, newReceiveMessagesStep, type ChangedMessageVisibility, type Connection, type DeletedMessage, type ReceivedMessages } from '@superdurable/connector-amazon-sqs';

/** Static Dex Web connection used by the queue consumer. */
export const ConnectionName = 'amazon-sqs-orders';
/** Safe queue selection for one receive and acknowledgement. */
export interface ConsumerInput { /** Queue URL, or omit to use the connection default. */ queueUrl?: string; }
/** Safe completed business outcome without the receipt handle. */
export interface ConsumerOutcome {
  /** Business outcome of one queue poll. */ status: 'noMessage' | 'processed' | 'rejected';
  /** Provider message ID when a message arrived. */ messageId?: string;
  /** Whether SQS accepted the delete or release action. */ isAcknowledgementConfirmed: boolean;
}
/** Durable queue selection. */
export const consumerRequest = new Attribute<ConsumerInput>('amazon-sqs-ts-consumer-request', jsonCodec<ConsumerInput>());
/** Durable result without credentials or receipt handles. */
export const consumerOutcome = new Attribute<ConsumerOutcome>('amazon-sqs-ts-consumer-outcome', jsonCodec<ConsumerOutcome>());
interface MessageReceipt { queueUrl: string; messageId: string; receiptHandle: string; }

/** Receive one order event, then delete valid events or release invalid events. */
export class QueueConsumerFlow implements Flow<ConsumerInput> {
  private readonly prepare = new PrepareConsumerRequest();
  private readonly inspect = new InspectReceivedEvent();
  private readonly noMessage = new CompleteNoMessage();
  private readonly acknowledged = new CompleteAcknowledgedEvent();
  private readonly released = new CompleteReleasedEvent();
  private readonly connection: Connection;

  /** Bind the static named SQS connection. */
  constructor(connection: Connection) {
    if (!connection || connection.reference.name !== ConnectionName) throw new TypeError('queue consumer requires its static SQS connection');
    this.connection = connection;
  }
  /** Return the stable Flow identity. */ getFlowType(): string { return 'AmazonSQSQueueConsumerTypeScript'; }
  /** Register the request, provider, and completion Steps. */
  getSteps(): StepList<ConsumerInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newReceiveMessagesStep<ConsumerInput>({ stepType: 'ReceiveOrderEvent',
        annotations: { groupId: 'receive-order-event', groupLabel: 'Receive order event', explanation: 'Long-poll the queue for one order event.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => ({ queueUrl: input.queueUrl, maxMessages: 1, waitTimeSeconds: 0, visibilityTimeoutSeconds: 300 }),
        received: new StepRef('InspectReceivedEvent'),
        // An empty queue is a routine outcome; complete without processing a message.
        empty: new StepRef('CompleteNoMessage'),
      }),
      this.inspect, this.noMessage,
      newDeleteMessageStep<MessageReceipt>({ stepType: 'AcknowledgeOrderEvent',
        annotations: { groupId: 'acknowledge-order-event', groupLabel: 'Acknowledge order event', explanation: 'Delete the processed order event.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        deleted: new StepRef('CompleteAcknowledgedEvent'),
        // A previous delivery may already be deleted; report the handle as unconfirmed.
        receiptHandleInvalid: new StepRef('CompleteAcknowledgedEvent'),
      }),
      this.acknowledged,
      newChangeMessageVisibilityStep<MessageReceipt>({ stepType: 'ReleaseOrderEvent',
        annotations: { groupId: 'release-order-event', groupLabel: 'Release order event', explanation: 'Return an invalid order event to the queue.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => ({ ...input, visibilityTimeoutSeconds: 0 }),
        changed: new StepRef('CompleteReleasedEvent'),
        // A stale receipt cannot release the message; report the handle as unconfirmed.
        receiptHandleInvalid: new StepRef('CompleteReleasedEvent'),
      }),
      this.released,
    );
  }
  /** Register the request and outcome Attributes. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [consumerRequest, consumerOutcome] }; }
}

// dex:group group-id:receive-order-event group-label:"Receive order event"
// dex:explanation text:"Persist the safe queue selection before polling SQS."
class PrepareConsumerRequest implements Step<ConsumerInput> {
  getStepType(): string { return 'PrepareConsumerRequest'; }
  execute(context: Context, input: ConsumerInput): StepDecision {
    if (input.queueUrl !== undefined && typeof input.queueUrl !== 'string') return forceFail('queueUrl must be a string');
    consumerRequest.set(context, input);
    return goTo(new StepRef('ReceiveOrderEvent'), input);
  }
}

// dex:group group-id:receive-order-event group-label:"Receive order event"
// dex:explanation text:"Inspect the first event and choose acknowledgement or release."
class InspectReceivedEvent implements Step<MutationResult<ReceivedMessages>> {
  getStepType(): string { return 'InspectReceivedEvent'; }
  execute(_context: Context, result: MutationResult<ReceivedMessages>): StepDecision {
    const message = result.value.messages?.[0];
    if (!message) return forceFail('SQS received branch had no message');
    const receipt: MessageReceipt = { queueUrl: result.value.queueUrl, messageId: message.messageId, receiptHandle: message.receiptHandle };
    try {
      const event = JSON.parse(message.body) as { orderId?: unknown; event?: unknown };
      if (typeof event.orderId === 'string' && /^[A-Za-z0-9-]{1,64}$/.test(event.orderId) &&
          typeof event.event === 'string' && ['created', 'paid', 'shipped', 'cancelled'].includes(event.event)) {
        return goTo(new StepRef('AcknowledgeOrderEvent'), receipt);
      }
    } catch { /* Malformed event payload is released for the queue redrive policy. */ }
    return goTo(new StepRef('ReleaseOrderEvent'), receipt);
  }
}

// dex:group group-id:receive-order-event group-label:"Receive order event"
// dex:explanation text:"Complete when no order event arrived."
class CompleteNoMessage implements Step<MutationResult<ReceivedMessages>> {
  getStepType(): string { return 'CompleteNoMessage'; }
  execute(context: Context): StepDecision { const outcome: ConsumerOutcome = { status: 'noMessage', isAcknowledgementConfirmed: false }; consumerOutcome.set(context, outcome); return gracefulComplete(outcome); }
}

// dex:group group-id:acknowledge-order-event group-label:"Acknowledge order event"
// dex:explanation text:"Record whether SQS accepted deletion of the processed event."
class CompleteAcknowledgedEvent implements Step<MutationResult<DeletedMessage>> {
  getStepType(): string { return 'CompleteAcknowledgedEvent'; }
  execute(context: Context, result: MutationResult<DeletedMessage>): StepDecision {
    const outcome: ConsumerOutcome = { status: 'processed', messageId: result.value.messageId, isAcknowledgementConfirmed: result.branch === 'deleted' };
    consumerOutcome.set(context, outcome);
    return gracefulComplete(outcome);
  }
}

// dex:group group-id:release-order-event group-label:"Release order event"
// dex:explanation text:"Record whether SQS accepted release of the invalid event."
class CompleteReleasedEvent implements Step<MutationResult<ChangedMessageVisibility>> {
  getStepType(): string { return 'CompleteReleasedEvent'; }
  execute(context: Context, result: MutationResult<ChangedMessageVisibility>): StepDecision {
    const outcome: ConsumerOutcome = { status: 'rejected', messageId: result.value.messageId, isAcknowledgementConfirmed: result.branch === 'changed' };
    consumerOutcome.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
