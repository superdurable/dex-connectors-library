// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  defineMemberChangedTriggerBinding, newGetMemberStep,
  type Connection, type GetMemberInput, type Member, type MemberChangedEvent,
} from '@superdurable/connector-intuit-mailchimp';

/** Static Dex Web connection for Mailchimp. */
export const ConnectionName = 'mailchimp-audience';
/** The stable memberChanged binding that starts this Flow. */
export const MemberChangedTriggerBinding = 'audience-member-changes';

/** One memberChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface MemberChangeInput { /** Names the Flow too. */ triggerEventId: string; /** The delivered change. */ change: MemberChangedEvent }
/** The Flow result: the delivered contact and its current state read back from Mailchimp. */
export interface MemberChangeOutcome {
  /** The watched audience. */ audienceId: string;
  /** The changed contact's address as delivered. */ emailAddress: string;
  /** The contact's status in the delivered change. */ deliveredStatus: string;
  /** The contact read back from Mailchimp, which may be newer than the delivered change. */ current: Member;
}

/** The delivered change. */
export const memberChangeAttribute = new Attribute<MemberChangeInput>('mailchimp-member-change', jsonCodec<MemberChangeInput>());
/** The recorded outcome. */
export const memberChangeOutcomeAttribute = new Attribute<MemberChangeOutcome>('mailchimp-member-change-outcome', jsonCodec<MemberChangeOutcome>());

/** Records one Mailchimp contact change and reads the contact back. */
export class MemberChangeFlow implements Flow<MemberChangeInput> {
  private readonly record = new RecordMemberChange();
  private readonly complete = new CompleteMemberChange();
  /** Bind the static Mailchimp connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError(`member change Flow requires the ${ConnectionName} connection`);
  }
  /** Stable Flow type. */ getFlowType(): string { return 'MailchimpMemberChangeTypeScript'; }
  /** Start at the record Step, which the Trigger starts the Flow at. */
  getSteps(): StepList<MemberChangeInput> {
    return StepList.startStep(this.record).otherSteps(newGetMemberStep<MemberChangeInput>({
      stepType: 'ReadChangedMember', connection: this.connection, connectionName: ConnectionName,
      annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed contact back, so the record holds its current state rather than the polled copy.' },
      mapToOperationInput: mapToGetMemberInput, found: this.complete,
    }), this.complete);
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [memberChangeAttribute, memberChangeOutcomeAttribute] }; }
  /** Declare the memberChanged binding with its audience and status units. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineMemberChangedTriggerBinding({ connectionName: ConnectionName, bindingName: MemberChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedAudience', unitId: 'audienceIdEntry', label: 'Watched audience', required: true,
      description: 'Enter the ID of the audience whose added and changed contacts start this Flow, such as 57afe96172, from Audience > More options > Audience settings in Mailchimp. The first start records existing contacts as a baseline and starts no Flow for them. Restart the Worker after saving; a changed audience records a new baseline.',
      bindings: [{ port: 'audienceId', jsonPointer: '/audienceId' }],
    }, {
      id: 'deliveredStatuses', unitId: 'memberStatusPicker', label: 'Contact statuses', required: false,
      description: 'Select the statuses a contact must have after its change to start this Flow, for example subscribed for new sign-ups or unsubscribed and cleaned to suppress addresses elsewhere. Select none to start a Flow for every changed contact.',
      bindings: [{ port: 'statuses', jsonPointer: '/statuses' }],
    }] } })];
  }
}

/** Name one Flow per contact version, so a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<MemberChangedEvent>): string { return `mailchimp-member-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<MemberChangedEvent>): MemberChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; the binding's status picker already filters them. */
export function acceptEveryMemberChange(): boolean { return true; }
/** Read the changed contact from its audience. */
export function mapToGetMemberInput(input: MemberChangeInput): GetMemberInput { return { listId: input.change.audienceId, emailAddress: input.change.member.emailAddress }; }

/** Return why a delivered change is unusable, or '' when it is valid. */
export function validateMemberChangeInput(input: MemberChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.audienceId?.trim() || !input.change.member?.emailAddress?.trim()) return 'change.audienceId and change.member.emailAddress are required';
  return '';
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered contact change, then read the contact back."
class RecordMemberChange implements Step<MemberChangeInput> {
  getStepType(): string { return 'RecordMemberChange'; }
  execute(context: Context, input: MemberChangeInput): StepDecision {
    const problem = validateMemberChangeInput(input);
    if (problem) return forceFail(problem);
    memberChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedMember'), input);
  }
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the contact read back from Mailchimp as the outcome and complete the Flow."
class CompleteMemberChange implements Step<QueryResult<Member>> {
  getStepType(): string { return 'CompleteMemberChange'; }
  execute(context: Context, result: QueryResult<Member>): StepDecision {
    const change = memberChangeAttribute.get(context).change;
    const outcome: MemberChangeOutcome = { audienceId: change.audienceId, emailAddress: change.member.emailAddress, deliveredStatus: change.member.status, current: result.value };
    memberChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
