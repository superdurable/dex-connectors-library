// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newAddCommentStep, newCreateTaskStep, newGetTaskStep, newListTasksStep, newUpdateTaskStep, type AddCommentOutput, type Connection, type CreateTaskOutput, type ListTasksOutput, type Task, type UpdateTaskOutput } from '../../src/index.js';

/** Static name of the Asana connection configured in Dex Web. */
export const ConnectionName = 'asana-approved-requests';
/** One approved request that should be materialized as a completed Asana task. */
export interface ApprovedRequest { /** Target project GID. */ projectId: string; /** Unique task title. */ name: string; /** Plain-text approval comment. */ comment: string }
/** Public result after the comment is confirmed. */
export interface ApprovedRequestResult { /** Existing or created task GID. */ taskId: string; /** Comment story GID. */ commentId: string }

const requestAttribute = new Attribute<ApprovedRequest>('asana-approved-request', jsonCodec<ApprovedRequest>());
const resultAttribute = new Attribute<ApprovedRequestResult>('asana-approved-result', jsonCodec<ApprovedRequestResult>());

/** List, create or reconcile, read, update, and comment on one approved task. */
export class ApprovedRequestFlow implements Flow<ApprovedRequest> {
  private readonly start: PrepareApprovedRequest;
  private readonly steps: Array<Step<ApprovedRequest> | Step<QueryResult<ListTasksOutput>> | Step<MutationResult<CreateTaskOutput>> | Step<{ taskId: string }> | Step<{ taskId: string; text: string }> | Step<QueryResult<Task>> | Step<MutationResult<UpdateTaskOutput>> | Step<MutationResult<AddCommentOutput>>>;
  /** Bind every Asana operation at Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('approved request requires its static Asana connection');
    const completed = new CompleteApprovedRequest();
    const comment = newAddCommentStep<{ taskId: string; text: string }>({
      stepType: 'AddApprovalComment', annotations: { groupId: 'asana-approval', groupLabel: 'Asana approval', explanation: 'Add the approval note once to the completed task.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input, added: completed,
    });
    const updated = new TaskUpdated(comment);
    const update = newUpdateTaskStep<{ taskId: string }>({
      stepType: 'CompleteApprovedTask', annotations: { groupId: 'asana-approval', groupLabel: 'Asana approval', explanation: 'Set the task completed flag to true.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => ({ taskId: input.taskId, isCompleted: true }), updated,
    });
    const found = new TaskFound(update);
    const get = newGetTaskStep<{ taskId: string }>({
      stepType: 'ReadApprovedTask', annotations: { groupId: 'asana-approval', groupLabel: 'Asana approval', explanation: 'Read the task before applying the approval.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input, found,
    });
    const created = new TaskCreated(get);
    const create = newCreateTaskStep<ApprovedRequest>({
      stepType: 'CreateApprovedTask', annotations: { groupId: 'asana-approval', groupLabel: 'Asana approval', explanation: 'Create the approved request as one task.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => ({ name: input.name, projectId: input.projectId }), created,
    });
    const listed = new TasksListed(create, get);
    const list = newListTasksStep<ApprovedRequest>({
      stepType: 'ListApprovedProjectTasks', annotations: { groupId: 'asana-approval', groupLabel: 'Asana approval', explanation: 'Find an existing task with the approved request title.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => ({ projectId: input.projectId }), listed,
    });
    this.start = new PrepareApprovedRequest(list);
    this.steps = [list, listed, create, created, get, found, update, updated, comment, completed];
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'AsanaApprovedRequest'; }
  /** Register the start, connector, and completion Steps. */ getSteps(): StepList<ApprovedRequest> { return StepList.startStep(this.start).otherSteps(...this.steps); }
  /** Register every durable, nonsecret application Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [requestAttribute, resultAttribute] }; }
}

// dex:group group-id:asana-approval group-label:"Asana approval"
// dex:explanation text:"Validate and retain the approved request before provider calls."
class PrepareApprovedRequest implements Step<ApprovedRequest> {
  constructor(private readonly list: Step<ApprovedRequest>) {}
  getStepType(): string { return 'PrepareApprovedRequest'; }
  execute(context: Context, input: ApprovedRequest): StepDecision {
    if (!/^[0-9]{1,64}$/.test(input.projectId) || !input.name.trim() || !input.comment.trim()) return forceFail('projectId, name, and comment are required');
    context.setAttribute(requestAttribute, input);
    return goTo(this.list, input);
  }
}

// dex:group group-id:asana-approval group-label:"Asana approval"
// dex:explanation text:"Reuse the matching task or create the approved request."
class TasksListed implements Step<QueryResult<ListTasksOutput>> {
  constructor(private readonly create: Step<ApprovedRequest>, private readonly get: Step<{ taskId: string }>) {}
  getStepType(): string { return 'TasksListed'; }
  execute(context: Context, result: QueryResult<ListTasksOutput>): StepDecision {
    const request = context.getAttribute(requestAttribute);
    const existing = result.value.tasks?.find(task => task.name === request.name);
    return existing ? goTo(this.get, { taskId: existing.id }) : goTo(this.create, request);
  }
}

// dex:group group-id:asana-approval group-label:"Asana approval"
// dex:explanation text:"Read the newly created task by its returned GID."
class TaskCreated implements Step<MutationResult<CreateTaskOutput>> {
  constructor(private readonly get: Step<{ taskId: string }>) {}
  getStepType(): string { return 'TaskCreated'; }
  execute(_context: Context, result: MutationResult<CreateTaskOutput>): StepDecision { return goTo(this.get, { taskId: result.value.taskId! }); }
}

// dex:group group-id:asana-approval group-label:"Asana approval"
// dex:explanation text:"Carry the confirmed task GID into the absolute update."
class TaskFound implements Step<QueryResult<Task>> {
  constructor(private readonly update: Step<{ taskId: string }>) {}
  getStepType(): string { return 'TaskFound'; }
  execute(_context: Context, result: QueryResult<Task>): StepDecision { return goTo(this.update, { taskId: result.value.id }); }
}

// dex:group group-id:asana-approval group-label:"Asana approval"
// dex:explanation text:"Attach the approval comment after completion is confirmed."
class TaskUpdated implements Step<MutationResult<UpdateTaskOutput>> {
  constructor(private readonly comment: Step<{ taskId: string; text: string }>) {}
  getStepType(): string { return 'TaskUpdated'; }
  execute(context: Context, result: MutationResult<UpdateTaskOutput>): StepDecision { return goTo(this.comment, { taskId: result.value.taskId, text: context.getAttribute(requestAttribute).comment }); }
}

// dex:group group-id:asana-approval group-label:"Asana approval"
// dex:explanation text:"Persist the confirmed task and comment IDs."
class CompleteApprovedRequest implements Step<MutationResult<AddCommentOutput>> {
  getStepType(): string { return 'CompleteApprovedRequest'; }
  execute(context: Context, result: MutationResult<AddCommentOutput>): StepDecision {
    const outcome = { taskId: result.value.taskId, commentId: result.value.commentId! };
    context.setAttribute(resultAttribute, outcome);
    return gracefulComplete(outcome);
  }
}
