// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineTaskEventTriggerBinding, type TaskEvent } from '@superdurable/connector-clickup';

/** Static connection name selected by the Worker and Trigger. */ export const ConnectionName = 'clickup-workspace';
/** Stable binding name for task status changes. */ export const BindingName = 'task-status-changes';
/** Persisted, verified task event. */ export const taskEventAttribute = new Attribute<TaskEvent>('clickup-task-event', jsonCodec<TaskEvent>());

/** Record one verified ClickUp task status change. */
export class TaskEventFlow implements Flow<TaskEvent> {
  private readonly record = new RecordTaskEvent();
  /** Stable Flow type. */ getFlowType(): string { return 'ClickUpTaskEventFlow'; }
  /** Register the record Step. */ getSteps(): StepList<TaskEvent> { return StepList.startStep(this.record); }
  /** Register the persisted task event. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [taskEventAttribute] }; }
  /** Declare the event picker shown in Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineTaskEventTriggerBinding({ connectionName: ConnectionName, bindingName: BindingName, configurationUI: { units: [{
      id: 'taskEvents', unitId: 'taskEventPicker', label: 'Task events', required: false,
      description: 'Start at https://developer.clickup.com/docs/webhooks, then create a webhook with POST /team/{team_id}/webhook and subscribe its events to taskStatusUpdated. Select that event name in this picker to record task status changes; it stores a nonsecret list of event name strings. Leave it empty to accept every supported task event at this binding, while this Flow admits only status changes.',
      bindings: [{ port: 'events', jsonPointer: '/events' }],
    }] } })];
  }
}

/** Admit status changes only; ClickUp also sends taskCreated for a new task. */
export function acceptStatusChange(event: TriggerEvent<TaskEvent>): boolean { return event.payload.event === 'taskStatusUpdated'; }
/** Map redelivery to the same Flow ID. */
export function resolveFlowId(event: TriggerEvent<TaskEvent>): string { return `clickup-${event.id.replaceAll(':', '-')}`; }
/** Keep the verified event and no credentials in Flow input. */
export function mapToFlowInput(event: TriggerEvent<TaskEvent>): TaskEvent { return event.payload; }

/** Persist a verified status event. */
// dex:group group-id:clickup-task group-label:"ClickUp Task"
// dex:explanation text:"Persist the verified task status event and complete the Flow."
export class RecordTaskEvent implements Step<TaskEvent> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordClickUpTaskEvent'; }
  /** Store the event and complete. */
  execute(context: Context, event: TaskEvent): StepDecision { taskEventAttribute.set(context, event); return gracefulComplete(event); }
}
