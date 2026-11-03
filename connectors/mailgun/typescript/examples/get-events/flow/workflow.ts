// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetEventsStep, type Connection, type EventsOutput, type GetEventsInput } from '@superdurable/connector-mailgun';

/** Static Dex Web connection name for the Mailgun example. */
export const ConnectionName = 'mailgun';
/** Safe result retained in Flow persistence. */
export const eventsPage = new Attribute<EventsOutput>('mailgun-events-page', jsonCodec<EventsOutput>());

/** Fetch one Mailgun delivery events page. */
export class GetEventsFlow implements Flow<GetEventsInput> {
  private readonly start = new StartGetEvents();
  private readonly complete = new CompleteGetEvents();
  /** Bind the static Mailgun connection during registration. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Mailgun example requires its static connection');
  }
  /** Stable application Flow identity. */ getFlowType(): string { return 'MailgunGetEventsFlow'; }
  /** Register the start, provider, and completion Steps. */
  getSteps(): StepList<GetEventsInput> {
    return StepList.startStep(this.start).otherSteps(
      newGetEventsStep({
        stepType: 'CallMailgunGetEvents',
        annotations: { groupId: 'mailgun-events', groupLabel: 'Mailgun Events', explanation: 'Read one bounded delivery events page.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the result Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [eventsPage] }; }
}

/** Pass valid application input into the provider Step. */
// dex:group group-id:mailgun-events group-label:"Mailgun Events"
// dex:explanation text:"Validate the requested domain and pass it to the Mailgun operation."
class StartGetEvents implements Step<GetEventsInput> {
  getStepType(): string { return 'StartMailgunGetEvents'; }
  execute(_context: Context, input: GetEventsInput): StepDecision {
    if (!input.domain) return forceFail('Mailgun domain is required');
    return goTo(new StepRef('CallMailgunGetEvents'), input);
  }
}

/** Persist and return the listed events page. */
// dex:group group-id:mailgun-events group-label:"Mailgun Events"
// dex:explanation text:"Store the returned events page and complete the Flow."
class CompleteGetEvents implements Step<QueryResult<EventsOutput>> {
  getStepType(): string { return 'CompleteMailgunGetEvents'; }
  execute(context: Context, result: QueryResult<EventsOutput>): StepDecision {
    eventsPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
