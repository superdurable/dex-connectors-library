# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Create, read, update, list, and delete one disposable Todoist task."""

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.doist_todoist import (
    Connection,
    CreateTaskInput,
    DeleteTaskInput,
    DeleteTaskOutcome,
    GetTaskInput,
    ListTasksInput,
    Task,
    TaskPage,
    UpdateTaskInput,
    new_create_task_step,
    new_delete_task_step,
    new_get_task_step,
    new_list_tasks_step,
    new_update_task_content_step,
)

CONNECTION_NAME = "todoist"
FLOW_TYPE = "TodoistPythonTaskCRUD"
_CREATED_TASK = dex.Attribute("todoist-python-created-task", Task)


# dex:group group-id:todoist-crud group-label:"Todoist task CRUD"
# dex:explanation text:"Save the created task ID and read back the active task."
class _AfterCreate(dex.Step[MutationResult[Task]]):
    def get_step_type(self) -> str:
        return "TodoistAfterCreate"

    def execute(self, context: dex.Context, input: MutationResult[Task]) -> dex.StepDecision:
        _CREATED_TASK.set(context, input.value)
        return dex.go_to(dex.StepRef("TodoistReadTask"), GetTaskInput(input.value.id))


# dex:group group-id:todoist-crud group-label:"Todoist task CRUD"
# dex:explanation text:"Replace the title of the task just read."
class _AfterRead(dex.Step[QueryResult[Task]]):
    def get_step_type(self) -> str:
        return "TodoistAfterRead"

    def execute(self, context: dex.Context, input: QueryResult[Task]) -> dex.StepDecision:
        return dex.go_to(
            dex.StepRef("TodoistUpdateTask"),
            UpdateTaskInput(input.value.id, content=input.value.content + " - updated"),
        )


# dex:group group-id:todoist-crud group-label:"Todoist task CRUD"
# dex:explanation text:"List the project after a confirmed absolute title replacement."
class _AfterUpdate(dex.Step[MutationResult[Task]]):
    def get_step_type(self) -> str:
        return "TodoistAfterUpdate"

    def execute(self, context: dex.Context, input: MutationResult[Task]) -> dex.StepDecision:
        return dex.go_to(
            dex.StepRef("TodoistListTasks"),
            ListTasksInput(project_id=input.value.project_id),
        )


# dex:group group-id:todoist-crud group-label:"Todoist task CRUD"
# dex:explanation text:"Confirm the updated task appears in the project page, then delete it."
class _AfterList(dex.Step[QueryResult[TaskPage]]):
    def get_step_type(self) -> str:
        return "TodoistAfterList"

    def execute(self, context: dex.Context, input: QueryResult[TaskPage]) -> dex.StepDecision:
        task = _CREATED_TASK.get(context)
        if not any(listed.id == task.id for listed in input.value.tasks):
            return dex.force_fail("The created task was missing from the project page")
        return dex.go_to(dex.StepRef("TodoistDeleteTask"), DeleteTaskInput(task.id))


# dex:group group-id:todoist-crud group-label:"Todoist task CRUD"
# dex:explanation text:"Complete after Todoist acknowledges deletion of the disposable task."
class _AfterDelete(dex.Step[MutationResult[DeleteTaskOutcome]]):
    def get_step_type(self) -> str:
        return "TodoistAfterDelete"

    def execute(self, context: dex.Context, input: MutationResult[DeleteTaskOutcome]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class TaskCRUDFlow(dex.Flow[CreateTaskInput]):
    """Run all five Todoist operations with one static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Todoist CRUD requires the static todoist connection")
        annotations = {
            "groupId": "todoist-crud",
            "groupLabel": "Todoist task CRUD",
            "explanation": "Use the named Todoist connection for this lifecycle operation.",
        }
        self.after_create = _AfterCreate()
        self.after_read = _AfterRead()
        self.after_update = _AfterUpdate()
        self.after_list = _AfterList()
        self.after_delete = _AfterDelete()
        self.create = new_create_task_step(
            step_type="TodoistCreateTask",
            step_input_type=CreateTaskInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            created=self.after_create,
        )
        self.read = new_get_task_step(
            step_type="TodoistReadTask",
            step_input_type=GetTaskInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.after_read,
        )
        self.update = new_update_task_content_step(
            step_type="TodoistUpdateTask",
            step_input_type=UpdateTaskInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            updated=self.after_update,
        )
        self.list = new_list_tasks_step(
            step_type="TodoistListTasks",
            step_input_type=ListTasksInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            listed=self.after_list,
        )
        self.delete = new_delete_task_step(
            step_type="TodoistDeleteTask",
            step_input_type=DeleteTaskInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            deleted=self.after_delete,
        )

    def get_flow_type(self) -> str:
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[CreateTaskInput]:
        return dex.StepList.start_step(self.create).other_steps(
            self.after_create,
            self.read,
            self.after_read,
            self.update,
            self.after_update,
            self.list,
            self.after_list,
            self.delete,
            self.after_delete,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        return dex.PersistenceSchema.of(_CREATED_TASK)
