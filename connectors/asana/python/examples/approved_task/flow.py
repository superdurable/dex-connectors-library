# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Create, move, comment on, and read an approved Asana task in one Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.asana import (
    AddCommentInput,
    AddCommentOutput,
    Connection,
    CreateTaskInput,
    CreateTaskOutput,
    GetTaskInput,
    ListTasksInput,
    ListTasksOutput,
    Task,
    UpdateTaskInput,
    UpdateTaskOutput,
    new_add_comment_step,
    new_create_task_step,
    new_get_task_step,
    new_list_tasks_step,
    new_update_task_step,
)

CONNECTION_NAME = "asana-tasks"
FLOW_TYPE = "AsanaApprovedTaskPython"


@dataclass(frozen=True, slots=True)
class ApprovedTaskRequest:
    """Safe project, section, and task details supplied at Flow start."""

    project_id: str = field(metadata={"dex_json_name": "projectId"})
    section_id: str = field(metadata={"dex_json_name": "sectionId"})
    name: str
    comment_text: str = field(metadata={"dex_json_name": "commentText"})


_REQUEST = dex.Attribute("asana-approved-task-request", ApprovedTaskRequest)


# dex:group group-id:approved-task group-label:"Approved task"
# dex:explanation text:"Store the approved request before reading the project task page."
class RecordApprovedTask(dex.Step[ApprovedTaskRequest]):
    """Persist safe start input before the provider sequence."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordApprovedTask"

    def execute(self, context: dex.Context, input: ApprovedTaskRequest) -> dex.StepDecision:
        """Validate required fields and route to Asana task listing."""
        if not all((input.project_id, input.section_id, input.name, input.comment_text)):
            return dex.force_fail("projectId, sectionId, name, and commentText are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("ListApprovedProjectTasks"), input)


# dex:group group-id:approved-task group-label:"Approved task"
# dex:explanation text:"Avoid creating a duplicate task when the first project page already contains it."
class ProjectTasksListed(dex.Step[QueryResult[ListTasksOutput]]):
    """Check the project page for a task with the approved name."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "ProjectTasksListed"

    def execute(self, context: dex.Context, input: QueryResult[ListTasksOutput]) -> dex.StepDecision:
        """Create the task only when this page has no name match."""
        request = _REQUEST.get(context)
        if any(task.name == request.name for task in input.value.tasks):
            return dex.force_fail("a task with this name already appears on the first project page")
        return dex.go_to(dex.StepRef("CreateApprovedTask"), request)


# dex:group group-id:approved-task group-label:"Approved task"
# dex:explanation text:"Move the created task to the approved section."
class ApprovedTaskCreated(dex.Step[MutationResult[CreateTaskOutput]]):
    """Continue with an absolute section move after task creation."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "ApprovedTaskCreated"

    def execute(self, context: dex.Context, input: MutationResult[CreateTaskOutput]) -> dex.StepDecision:
        """Pass the new task ID to the repeatable update operation."""
        request = _REQUEST.get(context)
        return dex.go_to(
            dex.StepRef("MoveApprovedTask"),
            UpdateTaskInput(
                task_id=input.value.task_id,
                is_completed=False,
                section_id=request.section_id,
            ),
        )


# dex:group group-id:approved-task group-label:"Approved task"
# dex:explanation text:"Add the approval comment after Asana confirms the section move."
class ApprovedTaskMoved(dex.Step[MutationResult[UpdateTaskOutput]]):
    """Carry the moved task ID into the comment Mutation."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "ApprovedTaskMoved"

    def execute(self, context: dex.Context, input: MutationResult[UpdateTaskOutput]) -> dex.StepDecision:
        """Add the start request's approval text to the moved task."""
        request = _REQUEST.get(context)
        return dex.go_to(
            dex.StepRef("CommentOnApprovedTask"),
            AddCommentInput(
                task_id=input.value.task_id,
                text=request.comment_text,
            ),
        )


# dex:group group-id:approved-task group-label:"Approved task"
# dex:explanation text:"Read back the task after its approval comment is stored."
class ApprovalCommentAdded(dex.Step[MutationResult[AddCommentOutput]]):
    """Read the task one final time after the comment succeeds."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "ApprovalCommentAdded"

    def execute(self, context: dex.Context, input: MutationResult[AddCommentOutput]) -> dex.StepDecision:
        """Route the comment's task ID to the detail Query."""
        return dex.go_to(dex.StepRef("ReadApprovedTask"), GetTaskInput(input.value.task_id))


# dex:group group-id:approved-task group-label:"Approved task"
# dex:explanation text:"Return the final task state to the Flow caller."
class ApprovedTaskRead(dex.Step[QueryResult[Task]]):
    """Complete with the final, secret-free task snapshot."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "ApprovedTaskRead"

    def execute(self, context: dex.Context, input: QueryResult[Task]) -> dex.StepDecision:
        """Complete the Flow with the task returned by Asana."""
        return dex.graceful_complete(input.value)


class ApprovedTaskFlow(dex.Flow[ApprovedTaskRequest]):
    """List, create, move, comment, and read through one named Asana connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind every provider operation at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("approved task Flow requires the asana-tasks connection")
        annotations = {"groupId": "approved-task", "groupLabel": "Approved task"}
        self.record = RecordApprovedTask()
        self.listed = ProjectTasksListed()
        self.created = ApprovedTaskCreated()
        self.moved = ApprovedTaskMoved()
        self.commented = ApprovalCommentAdded()
        self.read = ApprovedTaskRead()
        self.list_tasks = new_list_tasks_step(
            step_type="ListApprovedProjectTasks",
            step_input_type=ApprovedTaskRequest,
            annotations={**annotations, "explanation": "Read one project task page before creation."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: ListTasksInput(
                project_id=request.project_id, page_size=100
            ),
            listed=self.listed,
        )
        self.create_task = new_create_task_step(
            step_type="CreateApprovedTask",
            step_input_type=ApprovedTaskRequest,
            annotations={**annotations, "explanation": "Create one task in the selected project."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: CreateTaskInput(
                name=request.name, project_id=request.project_id
            ),
            created=self.created,
        )
        self.update_task = new_update_task_step(
            step_type="MoveApprovedTask",
            step_input_type=UpdateTaskInput,
            annotations={**annotations, "explanation": "Move the task to the approved section."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            updated=self.moved,
        )
        self.add_comment = new_add_comment_step(
            step_type="CommentOnApprovedTask",
            step_input_type=AddCommentInput,
            annotations={**annotations, "explanation": "Add the approval comment once."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            added=self.commented,
        )
        self.get_task = new_get_task_step(
            step_type="ReadApprovedTask",
            step_input_type=GetTaskInput,
            annotations={**annotations, "explanation": "Read the final task details."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.read,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ApprovedTaskRequest]:
        """Register all five provider Steps and their happy-path transitions."""
        return dex.StepList.start_step(self.record).other_steps(
            self.list_tasks,
            self.listed,
            self.create_task,
            self.created,
            self.update_task,
            self.moved,
            self.add_comment,
            self.commented,
            self.get_task,
            self.read,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the only safe Attribute consumed across Steps."""
        return dex.PersistenceSchema.of(_REQUEST)
