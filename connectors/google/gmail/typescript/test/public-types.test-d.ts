// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Mutation, MutationStep, Query, QueryStep } from '@superdurable/connectors-sdk';
import type { MockMutation, MockQuery } from '@superdurable/connectors-sdk/connectormock';
import { newGetMessageStep, newReplyToMessageStep, newSendMessageStep, type GetMessageInput, type GmailConnection, type Message, type ReplyToMessageInput, type SendMessageInput, type SendMessageOutput } from '../src/index.js';
import type { GmailMock } from '../src/mock/index.js';

type Exact<Left, Right> = (<Value>() => Value extends Left ? 1 : 2) extends (<Value>() => Value extends Right ? 1 : 2) ?
  ((<Value>() => Value extends Right ? 1 : 2) extends (<Value>() => Value extends Left ? 1 : 2) ? true : false) : false;
type Assert<Value extends true> = Value;
type StepInput = { id: string };

export type PublicTypeAssertions = [
  Assert<Exact<ReturnType<GmailConnection['operations']['getMessage']>, Query<GetMessageInput, Message>>>,
  Assert<Exact<ReturnType<GmailConnection['operations']['sendMessage']>, Mutation<SendMessageInput, SendMessageOutput>>>,
  Assert<Exact<ReturnType<GmailConnection['operations']['replyToMessage']>, Mutation<ReplyToMessageInput, SendMessageOutput>>>,
  Assert<Exact<ReturnType<typeof newGetMessageStep<StepInput>>, QueryStep<StepInput, GetMessageInput, Message>>>,
  Assert<Exact<ReturnType<typeof newSendMessageStep<StepInput>>, MutationStep<StepInput, SendMessageInput, SendMessageOutput>>>,
  Assert<Exact<ReturnType<typeof newReplyToMessageStep<StepInput>>, MutationStep<StepInput, ReplyToMessageInput, SendMessageOutput>>>,
  Assert<Exact<ReturnType<GmailMock['getMessage']>, MockQuery<GetMessageInput, Message>>>,
  Assert<Exact<ReturnType<GmailMock['sendMessage']>, MockMutation<SendMessageInput, SendMessageOutput>>>,
  Assert<Exact<ReturnType<GmailMock['replyToMessage']>, MockMutation<ReplyToMessageInput, SendMessageOutput>>>,
];
