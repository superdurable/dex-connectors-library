// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionRefSchema, type ConnectionRef, type Mutation, type Query } from '@superdurable/connectors-sdk';
import type { GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput } from './models.js';
import { Connection as GeneratedConnection } from './generated/connection.js';

/** Three operation contracts supplied by a real client or scripted mock. */
export interface GmailOperations {
  /** Read one message. */ getMessage(): Query<GetMessageInput, Message>;
  /** Send a new message. */ sendMessage(): Mutation<SendMessageInput, SendMessageOutput>;
  /** Reply in an existing thread. */ replyToMessage(): Mutation<ReplyToMessageInput, SendMessageOutput>;
}

/** Named Gmail connection passed to operation Step factories. */
export class GmailConnection extends GeneratedConnection {
  /** Bind operation implementations to a static named Google connection. */
  constructor(operations: GmailOperations, reference: ConnectionRef) {
    if (!operations) throw new TypeError('Gmail connector operations are required');
    const validatedReference = ConnectionRefSchema.parse(reference);
    if (validatedReference.provider !== 'google') throw new TypeError('Gmail connection provider must be google');
    super(validatedReference, operations);
  }
}
