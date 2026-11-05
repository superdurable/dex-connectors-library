// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newFindContactByEmailStep, type Connection, type FindContactByEmailInput, type FindContactByEmailOutput } from '@superdurable/connector-front';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'front-support';

/** Safe request retained for inspection. */
export const contactRequest = new Attribute<FindContactByEmailInput>('front-contact-request', jsonCodec<FindContactByEmailInput>());
/** Contact result retained for inspection. */
export const contactResult = new Attribute<FindContactByEmailOutput>('front-contact-result', jsonCodec<FindContactByEmailOutput>());

/** Find one Front contact by email. */
export class FindContactByEmailFlow implements Flow<FindContactByEmailInput> {
  private readonly prepare = new PrepareContactLookup();
  private readonly complete = new CompleteContactLookup();

  /** Bind one named Front connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Front connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'FrontFindContactByEmailFlow'; }
  /** Register the provider Step and completion. */
  getSteps(): StepList<FindContactByEmailInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newFindContactByEmailStep({
        stepType: 'FindFrontContactByEmail',
        annotations: { groupId: 'front-contact', groupLabel: 'Front Contact', explanation: 'Find one Front contact by its email handle.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }), this.complete);
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [contactRequest, contactResult] }; }
}

// dex:group group-id:front-contact group-label:"Front Contact"
// dex:explanation text:"Validate the email before looking up a Front contact."
/** Validate the request before the provider Step. */
export class PrepareContactLookup implements Step<FindContactByEmailInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareFrontContactLookup'; }
  /** Retain safe input and enter the provider Step. */
  execute(context: Context, input: FindContactByEmailInput): StepDecision {
    if (!input.email || !input.email.includes('@')) return forceFail('A contact email is required');
    contactRequest.set(context, input);
    return goTo(new StepRef('FindFrontContactByEmail'), input);
  }
}

// dex:group group-id:front-contact group-label:"Front Contact"
// dex:explanation text:"Persist the found contact and complete the Flow."
/** Complete with the provider result. */
export class CompleteContactLookup implements Step<QueryResult<FindContactByEmailOutput>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteFrontContactLookup'; }
  /** Retain the safe result. */ execute(context: Context, result: QueryResult<FindContactByEmailOutput>): StepDecision { contactResult.set(context, result.value); return gracefulComplete(result.value); }
}
