// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, deadEnd, forceFail, goTo, gracefulComplete, jsonCodec, rpc, type Context, type Flow, type PersistenceSchema, type RPCResult, type Step, type StepDecision, type StepOptions } from '@superdurable/dex';
import type { MutationResult, TriggerBindingDefinition, TriggerEvent, TriggerFilter } from '@superdurable/connectors-sdk';
import {
  defineEnvelopeEventReceivedTriggerBinding, envelopeEventCustomFieldValue, newCreateEnvelopeFromTemplateStep,
  type Connection, type CreateEnvelopeFromTemplateInput, type CreatedEnvelope, type EnvelopeEvent, type TemplateRole,
} from '@superdurable/connector-docusign';

/** Static Dex Web connection name, shared with the Go envelope-signing example. */
export const ConnectionName = 'docusign-esignature';
/** Static envelopeEventReceived binding that resumes this Flow. */
export const EnvelopeOutcomeTriggerBinding = 'envelope-outcomes';
/** Hidden envelope custom field that carries the signing request ID back through Connect. */
export const CorrelationCustomFieldName = 'dexSigningRequestId';
const sendEnvelopeStepType = 'SendEnvelope';
const recordOutcomeStepType = 'RecordEnvelopeOutcome';
const requestIdPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;

/** One signature request: the Flow ID is docusign-signing- plus requestId. */
export interface SigningRequest { requestId: string; templateId: string; signers: TemplateRole[]; emailSubject?: string }
/** Lifecycle of one signature request. */
export type SigningStatus = '' | 'waitingForSignature' | 'completed' | 'declined' | 'voided';
/** Durable signing state. */
export interface SigningState { request: SigningRequest; status: SigningStatus; envelopeId?: string; receivedEventIds: string[] }
/** RPC input mapped from one Connect event. */
export interface EnvelopeEventInput { eventId: string; envelopeId: string; event: string }
/** RPC result. */
export interface ReceiveEnvelopeEventResult { isAccepted: boolean; isDuplicate: boolean; status: SigningStatus }

/** Signing state, the default home of Flow state. */
export const signingState = new Attribute<SigningState>('docusign-signing-state', jsonCodec<SigningState>());
const signingLock: StepOptions = { executeLockAttributes: [signingState.lock()] };

/** Sends one envelope from a template and completes when Connect reports its outcome. */
export class EnvelopeOutcomeFlow implements Flow<SigningRequest> {
  private readonly record = new RecordSigningRequest();
  private readonly sent = new RecordEnvelopeSent();
  private readonly outcome = new RecordEnvelopeOutcome();

  /** Bind the DocuSign connection. */
  constructor(private readonly connection: Connection) {}

  /** Stable Flow type. */ getFlowType(): string { return 'DocuSignEnvelopeOutcomeFlow'; }

  /** Register the application and connector Steps. */
  getSteps(): StepList<SigningRequest> {
    return StepList.startStep(this.record).otherSteps(
      newCreateEnvelopeFromTemplateStep<SigningRequest>({
        stepType: sendEnvelopeStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'docusign', groupLabel: 'DocuSign', explanation: 'Send the template envelope tagged with the signing request ID.' },
        mapToOperationInput: mapToCreateEnvelopeInput,
        created: this.sent,
      }),
      this.sent,
      this.outcome,
    );
  }

  /** Persist the signing state. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [signingState] }; }

  /** The envelopeEventReceived binding that resumes this Flow; it has no configuration units, so Dex Web saves {}. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineEnvelopeEventReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: EnvelopeOutcomeTriggerBinding })];
  }

  /**
   * Accept the envelope's first Connect outcome once and schedule completion. A redelivered event is a
   * duplicate, an event for another envelope or after the outcome has no effect, and an event that
   * overtook the recorded envelope stays pending through an error.
   */
  @rpc({ lockAttributes: [signingState.lock()] })
  receiveEnvelopeEvent(context: Context, input: EnvelopeEventInput): RPCResult<ReceiveEnvelopeEventResult> {
    const state = signingState.get(context);
    if (state.receivedEventIds.includes(input.eventId)) return { output: { isAccepted: false, isDuplicate: true, status: state.status } };
    // The error keeps the event pending, so the Trigger retries it once the envelope ID is recorded.
    if (!state.envelopeId) throw new Error('DocuSign envelope is not recorded yet');
    if (input.envelopeId !== state.envelopeId || state.status !== 'waitingForSignature') return { output: { isAccepted: false, isDuplicate: false, status: state.status } };
    const next: SigningState = { ...state, receivedEventIds: [...state.receivedEventIds, input.eventId] };
    signingState.set(context, next);
    return { output: { isAccepted: true, isDuplicate: false, status: next.status }, nextSteps: [{ step: new StepRef(recordOutcomeStepType), input }] };
  }

  /** Read the signing state. */
  @rpc({ lockAttributes: [signingState.lock()] })
  getSigningState(context: Context): RPCResult<SigningState> { return { output: signingState.get(context) }; }
}

/** Validate and persist the signing request before sending. */
// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Validate the signing request and persist it before creating the envelope."
export class RecordSigningRequest implements Step<SigningRequest> {
  /** Stable Step type. */ getStepType(): string { return 'RecordSigningRequest'; }
  /** Lock the signing state. */ getStepOptions(): StepOptions { return signingLock; }
  /** Store the request and send the envelope. */
  execute(context: Context, request: SigningRequest): StepDecision {
    if (!requestIdPattern.test(request.requestId) || context.flowId !== flowIdForRequest(request.requestId)) return forceFail('requestId must be 1-64 letters, digits, - or _, and the Flow ID docusign-signing-<requestId>');
    signingState.set(context, { request, status: '', receivedEventIds: [] });
    return goTo(new StepRef(sendEnvelopeStepType), request);
  }
}

/** Record the sent envelope and wait for its Connect outcome. */
// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Record the envelope ID and wait for the envelopeEventReceived Trigger to invoke the RPC."
export class RecordEnvelopeSent implements Step<MutationResult<CreatedEnvelope>> {
  /** Stable Step type. */ getStepType(): string { return 'RecordEnvelopeSent'; }
  /** Lock the signing state. */ getStepOptions(): StepOptions { return signingLock; }
  /** Wait for the RPC. */
  execute(context: Context, result: MutationResult<CreatedEnvelope>): StepDecision {
    signingState.set(context, { ...signingState.get(context), envelopeId: result.value.envelopeId, status: 'waitingForSignature' });
    return deadEnd();
  }
}

/** Complete with the envelope's outcome. */
// dex:group group-id:signing group-label:"Signing"
// dex:explanation text:"Complete the Flow as completed, declined, or voided from the accepted Connect event."
export class RecordEnvelopeOutcome implements Step<EnvelopeEventInput> {
  /** Stable Step type. */ getStepType(): string { return recordOutcomeStepType; }
  /** Lock the signing state. */ getStepOptions(): StepOptions { return signingLock; }
  /** Record the outcome. */
  execute(context: Context, input: EnvelopeEventInput): StepDecision {
    const state: SigningState = { ...signingState.get(context), status: input.event.replace(/^envelope-/, '') as SigningStatus };
    signingState.set(context, state);
    return gracefulComplete(state);
  }
}

/** The Flow ID of one signing request. */
export function flowIdForRequest(requestId: string): string { return `docusign-signing-${requestId}`; }

/** Admit only an event whose envelope carries a well-formed correlation field. */
export const acceptEnvelopeEvent: TriggerFilter<EnvelopeEvent> = event => {
  const requestId = envelopeEventCustomFieldValue(event.payload, CorrelationCustomFieldName);
  return requestId !== undefined && requestIdPattern.test(requestId) && event.payload.envelopeId !== '';
};

/** Find the waiting Flow from the envelope's correlation custom field. */
export function resolveFlowId(event: TriggerEvent<EnvelopeEvent>): string {
  return flowIdForRequest(envelopeEventCustomFieldValue(event.payload, CorrelationCustomFieldName) ?? '');
}

/** Copy the event's identity into the RPC input. */
export function mapToEnvelopeEventInput(event: TriggerEvent<EnvelopeEvent>): EnvelopeEventInput {
  return { eventId: event.id, envelopeId: event.payload.envelopeId, event: event.payload.event };
}

/** Send the template to the signers and tag the envelope with the request ID. */
export function mapToCreateEnvelopeInput(request: SigningRequest): CreateEnvelopeFromTemplateInput {
  return {
    templateId: request.templateId, templateRoles: request.signers, ...(request.emailSubject ? { emailSubject: request.emailSubject } : {}),
    customFields: [{ name: CorrelationCustomFieldName, value: request.requestId }],
  };
}
