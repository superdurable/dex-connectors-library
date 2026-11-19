// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendApplicationMessageStep, type Connection, type SendApplicationMessageInput, type SentApplicationMessage } from '../../src/index.js';

/** Static application connection name. */ export const ApplicationConnectionName = 'wecom-app';
const applicationResult = new Attribute<SentApplicationMessage>('wecom-application-result', jsonCodec<SentApplicationMessage>());

/** Send one application notification through a typed Dex Step. */
export class WeComApplicationNotificationFlow implements Flow<SendApplicationMessageInput> {
  private readonly finish = new CompleteApplicationNotification();
  private readonly send;
  /** Bind the static application connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ApplicationConnectionName) throw new TypeError('WeCom application connection name differs');
    this.send = newSendApplicationMessageStep<SendApplicationMessageInput>({
      stepType: 'SendWeComApplicationMessage', annotations: { groupId: 'provider', groupLabel: 'WeCom application', explanation: 'Send one self-built application message to selected recipients.' },
      connection, connectionName: ApplicationConnectionName, mapToOperationInput: input => input,
      sent: new StepRef('CompleteWeComApplicationNotification'),
    });
  }
  /** Return the stable Flow identity. */ getFlowType(): string { return 'WeComApplicationNotificationTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<SendApplicationMessageInput> { return StepList.startStep(this.send).otherSteps(this.finish); }
  /** Persist only the nonsecret send result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [applicationResult] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the application acceptance and finish the Flow."
class CompleteApplicationNotification implements Step<MutationResult<SentApplicationMessage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWeComApplicationNotification'; }
  /** Persist the accepted result. */ execute(context: Context, result: MutationResult<SentApplicationMessage>): StepDecision {
    applicationResult.set(context, result.value); return gracefulComplete(result.value);
  }
}
