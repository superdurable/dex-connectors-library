// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineResponseSubmittedTriggerBinding, type FormResponseEvent } from '@superdurable/connector-typeform';

/** Static connection name selected by the Worker and Trigger. */ export const ConnectionName = 'typeform-workspace';
/** Stable binding name for form responses. */ export const BindingName = 'submitted-responses';
/** Persisted, verified form response. */ export const formResponseEventAttribute = new Attribute<FormResponseEvent>('typeform-response-recorder', jsonCodec<FormResponseEvent>());

/** Record one verified Typeform form response. */
export class FormResponseEventFlow implements Flow<FormResponseEvent> {
  private readonly record = new RecordFormResponseEvent();
  /** Stable Flow type. */ getFlowType(): string { return 'TypeformFormResponseEventFlow'; }
  /** Register the record Step. */ getSteps(): StepList<FormResponseEvent> { return StepList.startStep(this.record); }
  /** Register the persisted form response. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [formResponseEventAttribute] }; }
  /** Declare the event picker shown in Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineResponseSubmittedTriggerBinding({ connectionName: ConnectionName, bindingName: BindingName, configurationUI: { units: [{
      id: 'form', unitId: 'formPicker', label: 'Form', required: false,
      description: 'Open https://admin.typeform.com and choose the form under My forms. Select it in this provider-derived picker, or paste the nonsecret form ID from https://form.typeform.com/to/<formId>; this binding then records that form\'s submissions. Leave blank to accept submissions from every form whose webhook points at this endpoint.',
      bindings: [{ port: 'formId', jsonPointer: '/formId' }],
    }] } })];
  }
}

/** Admit verified submissions with a response token. */
export function acceptSubmission(event: TriggerEvent<FormResponseEvent>): boolean { return !!event.payload.response.token; }
/** Map redelivery to the same Flow ID. */
export function resolveFlowId(event: TriggerEvent<FormResponseEvent>): string { return `typeform-${event.id.replaceAll(':', '-')}`; }
/** Keep the verified event and no credentials in Flow input. */
export function mapToFlowInput(event: TriggerEvent<FormResponseEvent>): FormResponseEvent { return event.payload; }

/** Persist a verified submission. */
// dex:group group-id:typeform-response group-label:"Typeform Response"
// dex:explanation text:"Persist the verified submission and complete the Flow."
export class RecordFormResponseEvent implements Step<FormResponseEvent> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTypeformFormResponseEvent'; }
  /** Store the event and complete. */
  execute(context: Context, event: FormResponseEvent): StepDecision { formResponseEventAttribute.set(context, event); return gracefulComplete(event); }
}
