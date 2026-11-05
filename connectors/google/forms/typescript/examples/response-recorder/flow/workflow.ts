// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetFormStep, newListResponsesStep, newGetResponseStep, type Connection, type Form, type FormResponse, type GetFormInput, type ResponsePage } from '@superdurable/connector-google-forms';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'google-forms-reader';
/** Persisted form content. */
export const formAttribute = new Attribute<Form>('google-forms-form', jsonCodec<Form>());
/** Persisted first response page. */
export const responsePageAttribute = new Attribute<ResponsePage>('google-forms-response-page', jsonCodec<ResponsePage>());
/** Persisted one response. */
export const responseAttribute = new Attribute<FormResponse>('google-forms-response', jsonCodec<FormResponse>());

/** Read a form, its first response page, and one response through Google Forms. */
export class ResponseRecorderFlow implements Flow<GetFormInput> {
  private readonly prepare = new PrepareFormRead();
  private readonly afterForm = new AfterFormRead();
  private readonly afterPage = new AfterResponsePage();
  private readonly complete = new CompleteResponseRead();
  /** Bind the static Google Forms connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleFormsResponseRecorderFlow'; }
  /** Register every read Step. */
  getSteps(): StepList<GetFormInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetFormStep({ stepType: 'ReadGoogleForm', annotations: { groupId: 'google-forms', groupLabel: 'Google Forms', explanation: 'Read the form and its question IDs.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ formId: input.formId }), found: this.afterForm }),
      newListResponsesStep({ stepType: 'ListGoogleFormResponses', annotations: { groupId: 'google-forms', groupLabel: 'Google Forms', explanation: 'List one bounded page of responses.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ formId: input.formId }), listed: this.afterPage }),
      newGetResponseStep({ stepType: 'ReadGoogleFormResponse', annotations: { groupId: 'google-forms', groupLabel: 'Google Forms', explanation: 'Read one selected response.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ formId: input.formId, responseId: input.responseId }), found: this.complete }),
      this.afterForm, this.afterPage, this.complete,
    );
  }
  /** Register each persisted output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [formAttribute, responsePageAttribute, responseAttribute] }; }
}

// dex:group group-id:google-forms group-label:"Google Forms"
// dex:explanation text:"Validate the selected form ID before reading Google Forms."
/** Validate application input. */
export class PrepareFormRead implements Step<GetFormInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareGoogleFormRead'; }
  /** Start the form read. */ execute(_context: Context, input: GetFormInput): StepDecision {
    if (!/^[A-Za-z0-9_-]{1,256}$/.test(input.formId)) return forceFail('Google Forms IDs are invalid');
    return goTo(new StepRef('ReadGoogleForm'), input);
  }
}
// dex:group group-id:google-forms group-label:"Google Forms"
// dex:explanation text:"Store the form before listing its first response page."
/** Persist the form and continue. */
export class AfterFormRead implements Step<QueryResult<Form>> {
  /** Stable Step identity. */ getStepType(): string { return 'AfterGoogleFormRead'; }
  /** Persist the form and continue. */ execute(context: Context, result: QueryResult<Form>): StepDecision { formAttribute.set(context, result.value); return goTo(new StepRef('ListGoogleFormResponses'), { formId: result.value.formId }); }
}
// dex:group group-id:google-forms group-label:"Google Forms"
// dex:explanation text:"Store the bounded response page before reading its first response."
/** Persist the response page and continue. */
export class AfterResponsePage implements Step<QueryResult<ResponsePage>> {
  /** Stable Step identity. */ getStepType(): string { return 'AfterGoogleResponsePage'; }
  /** Persist the page and continue. */ execute(context: Context, result: QueryResult<ResponsePage>): StepDecision { responsePageAttribute.set(context, result.value); const first = result.value.responses?.[0]; if (!first) return forceFail('Google Form has no responses'); return goTo(new StepRef('ReadGoogleFormResponse'), { formId: result.value.formId, responseId: first.responseId }); }
}
// dex:group group-id:google-forms group-label:"Google Forms"
// dex:explanation text:"Store one response and complete the Flow."
/** Complete with one response. */
export class CompleteResponseRead implements Step<QueryResult<FormResponse>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteGoogleResponseRead'; }
  /** Persist the response. */ execute(context: Context, result: QueryResult<FormResponse>): StepDecision { responseAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
