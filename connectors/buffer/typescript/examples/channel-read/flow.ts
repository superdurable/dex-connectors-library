// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetChannelStep, type Connection, type ChannelProfile, type GetChannelInput,
} from '@superdurable/connector-buffer';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'buffer';
const usersAttribute = new Attribute<ChannelProfile>('buffer-channel-read', jsonCodec<ChannelProfile>());

/** Read channel metadata through a typed Buffer Query. */
export class ChannelReadFlow implements Flow<GetChannelInput> {
  private readonly complete = new CompleteChannelRead();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Buffer connection name differs');
    this.read = newGetChannelStep<GetChannelInput>({
      stepType: 'CallGetChannel',
      annotations: { groupId: 'provider', groupLabel: 'Buffer', explanation: 'Read the connected channel name and social service from Buffer.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteBuffer'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'BufferChannelReadTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetChannelInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the channel metadata result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [usersAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the channel metadata and complete the Flow."
class CompleteChannelRead implements Step<QueryResult<ChannelProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteBuffer'; }
  /** Persist and return the channel metadata. */
  execute(context: Context, result: QueryResult<ChannelProfile>): StepDecision {
    usersAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
