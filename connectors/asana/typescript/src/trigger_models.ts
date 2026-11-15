// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import type { TaskReference, UserReference } from './generated/models.js';

/** The Asana Events API actions of one taskChanged delivery, the values of TaskChangedEvent.action. */
export const TaskChangeAction = {
  /** A task added to the project, one of its sections, or a parent task; parent names the container. */ Added: 'added',
  /** A field change, such as completed, assignee, due_on, or name; changedField names the field. */ Changed: 'changed',
  /** A task removed from the project, one of its sections, or a parent task; parent names the container. */ Removed: 'removed',
  /** A task moved to the Asana trash. */ Deleted: 'deleted',
  /** A task restored from the Asana trash. */ Undeleted: 'undeleted',
} as const;
/** One of the five TaskChangeAction values. */
export type TaskChangeAction = (typeof TaskChangeAction)[keyof typeof TaskChangeAction];

/** The container of an added or removed task. */
export interface EventParentReference {
  /** The container's gid. */ id: string;
  /** project, section, or task. */ resourceType: string;
  /** The container's name, as of the poll. */ name?: string;
}

/**
 * One task event from the watched project's Asana event stream; field names match the Go connector, so Go
 * and TypeScript replicas share one inbox. Asana events name the task and what happened, not the task's
 * current state, so a Flow reads the task with getTask when it needs more.
 */
export interface TaskChangedEvent {
  /** The watched project. */ projectId: string;
  /** What happened to the task; see TaskChangeAction. */ action: string;
  /** The task or subtask the event is about, with its name as of the poll. */ task: TaskReference;
  /** Asana's resource_subtype of the task, such as default_task, milestone, or approval. */ taskSubtype?: string;
  /** The changed task field, such as completed or assignee; set only for changed. */ changedField?: string;
  /** How changedField changed: changed for a value, or added or removed for a list entry such as a follower; set only for changed. */ changeAction?: string;
  /** The project, section, or task the task was added to or removed from; absent for other actions. */ parent?: EventParentReference;
  /** The Asana user who made the change; absent for a change Asana made itself, such as a rule. */ user?: UserReference;
  /** When Asana recorded the change, as an RFC 3339 timestamp. */ occurredAt: string;
}

const goString = z.string().nullish().transform(value => value ?? '');
const optionalGoString = z.string().nullish().transform(value => value || undefined);

/** Decodes a stored TaskChangedEvent as Go's json.Unmarshal does: unknown fields are ignored. */
export const TaskChangedEventSchema = z.object({
  projectId: goString,
  action: goString,
  task: z.object({ id: goString, name: optionalGoString }).transform(({ id, name }) => ({ id, ...(name ? { name } : {}) })),
  taskSubtype: optionalGoString,
  changedField: optionalGoString,
  changeAction: optionalGoString,
  parent: z.object({ id: goString, resourceType: goString, name: optionalGoString }).nullish(),
  user: z.object({ id: goString, name: optionalGoString }).nullish(),
  occurredAt: goString,
}).transform((event): TaskChangedEvent => ({
  projectId: event.projectId, action: event.action, task: event.task,
  ...(event.taskSubtype ? { taskSubtype: event.taskSubtype } : {}),
  ...(event.changedField ? { changedField: event.changedField } : {}),
  ...(event.changeAction ? { changeAction: event.changeAction } : {}),
  ...(event.parent ? { parent: { id: event.parent.id, resourceType: event.parent.resourceType, ...(event.parent.name ? { name: event.parent.name } : {}) } } : {}),
  ...(event.user ? { user: { id: event.user.id, ...(event.user.name ? { name: event.user.name } : {}) } } : {}),
  occurredAt: event.occurredAt,
}));

/**
 * The taskChanged binding configuration that the projectPicker unit saves in Dex Web. Changing the project of
 * a binding that already polled makes its next poll record a new baseline for the new project.
 */
export interface TaskChangedTriggerConfiguration {
  /** The gid of the project's workspace, kept for the picker only. */ workspaceId?: string;
  /** The gid of the watched project, a decimal string such as 1201000000000001; required. */ projectId: string;
  /** The picked project's name, kept for display only. */ projectName?: string;
}

/** Decodes a saved binding configuration strictly, as Go does: unknown fields are refused, null is blank. */
export const TaskChangedTriggerConfigurationSchema = z.strictObject({
  workspaceId: optionalGoString,
  projectId: goString,
  projectName: optionalGoString,
}).transform((configuration): TaskChangedTriggerConfiguration => ({
  ...(configuration.workspaceId ? { workspaceId: configuration.workspaceId } : {}),
  projectId: configuration.projectId,
  ...(configuration.projectName ? { projectName: configuration.projectName } : {}),
}));
