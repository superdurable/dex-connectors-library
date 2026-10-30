// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionRefSchema, type ConnectionRef, type Mutation, type Query } from '@superdurable/connectors-sdk';
import type { GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput } from './models.js';

/** Three operation contracts supplied by a real client or scripted mock. */
export interface GmailOperations {
  /** Read one message. */ getMessage(): Query<GetMessageInput, Message>;
  /** Send a new message. */ sendMessage(): Mutation<SendMessageInput, SendMessageOutput>;
  /** Reply in an existing thread. */ replyToMessage(): Mutation<ReplyToMessageInput, SendMessageOutput>;
}

/** Named Gmail connection passed to operation Step factories. */
export class GmailConnection {
  /** Stable logical credential reference. */ readonly reference: ConnectionRef;
  /** Provider or mock operation implementations. */ readonly operations: GmailOperations;

  /** Bind operation implementations to a static named Google connection. */
  constructor(operations: GmailOperations, reference: ConnectionRef) {
    if (!operations) throw new TypeError('Gmail connector operations are required');
    this.reference = ConnectionRefSchema.parse(reference);
    if (this.reference.provider !== 'google') throw new TypeError('Gmail connection provider must be google');
    this.operations = operations;
  }
}
