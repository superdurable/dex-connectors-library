// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListFormsStep, type Connection, type ListFormsInput, type FormPage } from '@superdurable/connector-typeform';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'typeform-forms';

/** Safe request fields retained for flow inspection. */
export const listRequest = new Attribute<ListFormsInput>('typeform-list-request', jsonCodec<ListFormsInput>());
/** Safe provider result retained for flow inspection. */
export const formPage = new Attribute<FormPage>('typeform-form-page', jsonCodec<FormPage>());

/** One bounded Typeform form-list Flow. */
export class ListFormsFlow implements Flow<ListFormsInput> {
  private readonly prepare = new PrepareListForms();
  private readonly complete = new CompleteListForms();

  /** Bind a named Typeform connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Typeform connection is required');
  }

  /** Stable Dex Flow type. */
  getFlowType(): string { return 'TypeformListFormsFlow'; }

  /** Register the preparation, provider, and completion Steps. */
  getSteps(): StepList<ListFormsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListFormsStep({
        stepType: 'ListTypeformForms',
        annotations: { groupId: 'typeform-list', groupLabel: 'Typeform Forms', explanation: 'List one bounded page of Typeform forms.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }

  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [listRequest, formPage] }; }
}

/** Validate listing fields and retain the safe request. */
// dex:group group-id:typeform-list group-label:"Typeform Forms"
// dex:explanation text:"Validate the form listing filters before listing forms."
export class PrepareListForms implements Step<ListFormsInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareTypeformListForms'; }
  /** Start the provider Step with nonsecret input. */
  execute(context: Context, input: ListFormsInput): StepDecision {
    if ((input.workspaceId && !/^[A-Za-z0-9_-]{1,128}$/.test(input.workspaceId)) || (input.pageSize !== undefined && (input.pageSize < 0 || input.pageSize > 200))) return forceFail('Typeform listing filters are invalid');
    listRequest.set(context, input);
    return goTo(new StepRef('ListTypeformForms'), input);
  }
}

/** Persist the listed page and complete the Flow. */
// dex:group group-id:typeform-list group-label:"Typeform Forms"
// dex:explanation text:"Persist the returned form page and complete the Flow."
export class CompleteListForms implements Step<QueryResult<FormPage>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteTypeformListForms'; }
  /** Retain only the provider's safe result fields. */
  execute(context: Context, result: QueryResult<FormPage>): StepDecision {
    formPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
