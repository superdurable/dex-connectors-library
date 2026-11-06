// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetMemberStep, type Connection, type GetMemberInput, type Member } from '@superdurable/connector-intuit-mailchimp';

/** Static Dex Web connection name shared by Worker and starter. */
export const ConnectionName = 'mailchimp-audience';
/** Safe request retained for Flow inspection. */
export const memberRequest = new Attribute<GetMemberInput>('mailchimp-member-request', jsonCodec<GetMemberInput>());
/** Returned provider contact retained for Flow inspection. */
export const memberResult = new Attribute<Member>('mailchimp-member-result', jsonCodec<Member>());

/** Read one Mailchimp audience contact through a Dex Step. */
export class GetMemberFlow implements Flow<GetMemberInput> {
  private readonly prepare = new PrepareGetMember();
  private readonly complete = new CompleteGetMember();
  /** Bind a named Mailchimp connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Mailchimp connection is required'); }
  /** Stable Flow type. */ getFlowType(): string { return 'MailchimpGetMemberFlow'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<GetMemberInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetMemberStep({ stepType: 'ReadMailchimpMember',
        annotations: { groupId: 'mailchimp-read', groupLabel: 'Mailchimp Member', explanation: 'Read one contact by its audience and email address.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        found: this.complete }), this.complete);
  }
  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [memberRequest, memberResult] }; }
}
/** Check IDs and retain safe request fields. */
// dex:group group-id:mailchimp-read group-label:"Mailchimp Member"
// dex:explanation text:"Validate the audience and email address before reading a contact."
export class PrepareGetMember implements Step<GetMemberInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareMailchimpGetMember'; }
  /** Continue to the provider Step. */
  execute(context: Context, input: GetMemberInput): StepDecision {
    if (!/^[A-Za-z0-9]{1,64}$/.test(input.listId) || !input.emailAddress) return forceFail('Mailchimp audience ID and email address are required');
    memberRequest.set(context, input);
    return goTo(new StepRef('ReadMailchimpMember'), input);
  }
}
/** Persist the contact and complete the Flow. */
// dex:group group-id:mailchimp-read group-label:"Mailchimp Member"
// dex:explanation text:"Persist the returned contact and complete the Flow."
export class CompleteGetMember implements Step<QueryResult<Member>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteMailchimpGetMember'; }
  /** Retain the provider's safe contact result. */
  execute(context: Context, result: QueryResult<Member>): StepDecision {
    memberResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
