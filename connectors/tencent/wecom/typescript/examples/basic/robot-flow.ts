// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendGroupRobotMessageStep, type Connection, type SendGroupRobotMessageInput, type SentGroupRobotMessage } from '../../src/index.js';

/** Static robot connection name. */ export const RobotConnectionName = 'wecom-robot';
const robotResult = new Attribute<SentGroupRobotMessage>('wecom-robot-result', jsonCodec<SentGroupRobotMessage>());

/** Post one robot notification through a typed Dex Step. */
export class WeComRobotNotificationFlow implements Flow<SendGroupRobotMessageInput> {
  private readonly finish = new CompleteRobotNotification();
  private readonly send;
  /** Bind the static robot connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== RobotConnectionName) throw new TypeError('WeCom robot connection name differs');
    this.send = newSendGroupRobotMessageStep<SendGroupRobotMessageInput>({
      stepType: 'SendWeComRobotMessage', annotations: { groupId: 'provider', groupLabel: 'WeCom robot', explanation: 'Post one message to the configured internal group.' },
      connection, connectionName: RobotConnectionName, mapToOperationInput: input => input,
      sent: new StepRef('CompleteWeComRobotNotification'),
    });
  }
  /** Return the stable Flow identity. */ getFlowType(): string { return 'WeComRobotNotificationTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<SendGroupRobotMessageInput> { return StepList.startStep(this.send).otherSteps(this.finish); }
  /** Persist only the nonsecret send result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [robotResult] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the robot acceptance and finish the Flow."
class CompleteRobotNotification implements Step<MutationResult<SentGroupRobotMessage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWeComRobotNotification'; }
  /** Persist the accepted result. */ execute(context: Context, result: MutationResult<SentGroupRobotMessage>): StepDecision {
    robotResult.set(context, result.value); return gracefulComplete(result.value);
  }
}
