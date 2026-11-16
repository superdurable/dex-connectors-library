// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  TaskChangeAction, defineTaskChangedTriggerBinding, newGetTaskStep, type Connection, type GetTaskInput, type Task, type TaskChangedEvent,
} from '@superdurable/connector-asana';

/** The static Dex Web connection for Asana. */
export const ConnectionName = 'asana-task-watcher';
/** The stable taskChanged binding that starts this Flow. */
export const TaskChangedTriggerBinding = 'project-task-changes';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered event finds its Flow. */
export const FlowIDPrefix = 'asana-task-change-';

/** One taskChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface TaskChangeInput { /** The Trigger event ID that also names the Flow. */ triggerEventId: string; /** The delivered task event. */ change: TaskChangedEvent }

/** The Flow result and the value of its outcome Attribute. */
export interface ChangeOutcome {
  /** The watched project. */ projectId: string;
  /** The changed task. */ taskId: string;
  /** The delivered action. */ action: string;
  /** The changed task field, set only for a changed action. */ changedField?: string;
  /** The task read back from Asana; absent for a deleted task, which is not read. */ current?: Task;
}

/** The delivered change. */
export const taskChangeAttribute = new Attribute<TaskChangeInput>('asana-task-change', jsonCodec<TaskChangeInput>());
/** The recorded outcome. */
export const taskChangeOutcomeAttribute = new Attribute<ChangeOutcome>('asana-task-change-outcome', jsonCodec<ChangeOutcome>());

/** Records one Asana task change, reading the task back unless it was deleted. */
export class TaskChangedFlow implements Flow<TaskChangeInput> {
  private readonly record = new RecordTaskChange();
  private readonly complete = new CompleteTaskChange();
  /** Bind the Asana connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('task change Flow requires the asana-task-watcher connection');
  }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'AsanaTaskChangeTypeScript'; }
  /** Record the change, read the task back, and complete. */
  getSteps(): StepList<TaskChangeInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetTaskStep<TaskChangeInput>({
        stepType: 'ReadChangedTask', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: "Read the changed task back, because Asana events name the change but not the task's current state." },
        mapToOperationInput: mapToGetTaskInput, found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [taskChangeAttribute, taskChangeOutcomeAttribute] }; }
  /** Declare the taskChanged binding and its project picker for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineTaskChangedTriggerBinding({ connectionName: ConnectionName, bindingName: TaskChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedProject', unitId: 'projectPicker', label: 'Watched project', required: true,
      description: "Choose the workspace, then the Asana project whose tasks start this Flow when they are added, changed, removed, deleted, or restored, subtasks included; the lists come from this connection's Asana user and save the workspace ID, project ID, and project name. Without the lists, enter the project gid, the long number after /project/ in the project's Asana web address. A project is required; there is no blank default. The first start records a baseline and starts no Flow for existing tasks. Restart the Worker after saving; a changed project records a new baseline.",
      bindings: [
        { port: 'workspaceId', jsonPointer: '/workspaceId' },
        { port: 'projectId', jsonPointer: '/projectId' },
        { port: 'projectName', jsonPointer: '/projectName' },
      ],
    }] } })];
  }
}

/** Name one Flow per task event; a redelivered event finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<TaskChangedEvent>): string { return FlowIDPrefix + event.id; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<TaskChangedEvent>): TaskChangeInput { return { triggerEventId: event.id, change: event.payload }; }

/** Admit every task event; an application takes the event and narrows it, for example to completions. */
export function acceptEveryTaskChange(): boolean { return true; }

/** Read the changed task. */
export function mapToGetTaskInput(input: TaskChangeInput): GetTaskInput { return { taskId: input.change.task.id }; }

/** Return why a delivered change cannot be recorded, or an empty string. */
export function validateTaskChangeInput(input: TaskChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.projectId?.trim() || !input.change.task?.id?.trim()) return 'change.projectId and change.task.id are required';
  if (!(Object.values(TaskChangeAction) as string[]).includes(input.change.action)) return 'change.action must be added, changed, removed, deleted, or undeleted';
  return '';
}

function newChangeOutcome(change: TaskChangedEvent, current?: Task): ChangeOutcome {
  return { projectId: change.projectId, taskId: change.task.id, action: change.action, ...(change.changedField ? { changedField: change.changedField } : {}), ...(current ? { current } : {}) };
}

/** Store the delivered change and decide whether Asana has a task to read. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the task event; complete a deleted task at once, otherwise read the task back."
export class RecordTaskChange implements Step<TaskChangeInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTaskChange'; }
  /** Fail an invalid delivery, complete a deletion, and read any other changed task. */
  execute(context: Context, input: TaskChangeInput): StepDecision {
    const problem = validateTaskChangeInput(input);
    if (problem) return forceFail(problem);
    taskChangeAttribute.set(context, input);
    if (input.change.action === TaskChangeAction.Deleted) {
      const outcome = newChangeOutcome(input.change);
      taskChangeOutcomeAttribute.set(context, outcome);
      return gracefulComplete(outcome);
    }
    return goTo(new StepRef('ReadChangedTask'), input);
  }
}

/** Complete with the change and the task's current state. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the task read back from Asana as the outcome and complete the Flow."
export class CompleteTaskChange implements Step<QueryResult<Task>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteTaskChange'; }
  /** Save and return the outcome. */
  execute(context: Context, result: QueryResult<Task>): StepDecision {
    const outcome = newChangeOutcome(taskChangeAttribute.get(context).change, result.value);
    taskChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
