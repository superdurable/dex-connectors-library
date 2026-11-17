// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineRequestReceivedTriggerBinding, type WebhookRequestEvent } from '@superdurable/connector-superdurable-webhook';

/** Static Dex Web connection. */ export const ConnectionName = 'webhook-receiver';
/** Static requestReceived binding. */ export const SubmissionBinding = 'submissions';
/** Nonsecret event metadata kept by the Flow. */
export interface RecordedRequest { /** Stable delivery ID. */ eventId: string; /** Request media type. */ contentType: string; /** Receipt time. */ receivedAt: string; }
const requestAttribute = new Attribute('webhook-request', jsonCodec<RecordedRequest>());

/** Persist one verified request per stable delivery ID. */
export class WebhookRequestIntakeFlow implements Flow<RecordedRequest> {
  private readonly record = new RecordRequest();
  /** Stable Flow identity. */ getFlowType(): string { return 'WebhookRequestIntake'; }
  /** Start with the recording Step. */ getSteps(): StepList<RecordedRequest> { return StepList.startStep(this.record); }
  /** Register the persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [requestAttribute] }; }
  /** Declare the requestReceived binding. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineRequestReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: SubmissionBinding, configurationUI: { units: [] } })];
  }
}
/** Accept verified form_response events. */
export function acceptSubmission(event: TriggerEvent<WebhookRequestEvent>): boolean {
  return event.payload.contentType === 'application/json' && !!event.payload.jsonBody && typeof event.payload.jsonBody === 'object'
    && !Array.isArray(event.payload.jsonBody) && (event.payload.jsonBody as Record<string, unknown>).event_type === 'form_response';
}
/** Deduplicate redeliveries with the stable sender ID. */
export function resolveFlowId(event: TriggerEvent<WebhookRequestEvent>): string { return `webhook-request-${Buffer.from(event.id).toString('hex')}`; }
/** Exclude request body and headers from persisted Flow input. */
export function mapToFlowInput(event: TriggerEvent<WebhookRequestEvent>): RecordedRequest {
  return { eventId: event.id, contentType: event.payload.contentType, receivedAt: event.payload.receivedAt };
}

// dex:group group-id:webhook group-label:"Webhook"
// dex:explanation text:"Persist the verified request's nonsecret delivery metadata and complete the Flow."
class RecordRequest implements Step<RecordedRequest> {
  readonly inputCodec = jsonCodec<RecordedRequest>();
  getStepType(): string { return 'RecordRequest'; }
  execute(context: Context, input: RecordedRequest): StepDecision { requestAttribute.set(context, input); return gracefulComplete(input); }
}
