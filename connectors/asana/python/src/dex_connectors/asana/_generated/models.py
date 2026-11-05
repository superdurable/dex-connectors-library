# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk.timestamps import parse_go_datetime_input

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListTasksInput selects one page of tasks. Set exactly one of ProjectID and SectionID, and at most one
# of IsIncompleteOnly and CompletedSince.
@dataclass(frozen=True, slots=True)
class ListTasksInput:
    # ProjectID lists the tasks of this project gid.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # SectionID lists the tasks of this section gid.
    section_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'sectionId', 'dex_json_omit_default': True})
    # IsIncompleteOnly lists only incomplete tasks.
    is_incomplete_only: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isIncompleteOnly', 'dex_json_omit_default': True})
    # CompletedSince lists incomplete tasks plus tasks completed at or after this time.
    completed_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'completedSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ModifiedSince lists only tasks that changed at or after this time. Asana counts assigning, renaming,
    # completing, commenting on, or adding the task to a project as a change.
    modified_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'modifiedSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # PageSize is the number of tasks to request, 1 to 100; zero requests 50.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Offset continues a previous list with the same filters; blank reads the first page. Asana expires
    # offset tokens after some time.
    offset: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.completed_since, str):
            object.__setattr__(self, 'completed_since', parse_go_datetime_input(self.completed_since, 'completed_since'))
        if isinstance(self.modified_since, str):
            object.__setattr__(self, 'modified_since', parse_go_datetime_input(self.modified_since, 'modified_since'))


setattr(ListTasksInput, '__module__', 'dex_connectors.asana.models')


# UserReference identifies an Asana user by gid and display name.
@dataclass(frozen=True, slots=True)
class UserReference:
    # ID is the user's gid.
    id: str = ''
    # Name is the user's display name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(UserReference, '__module__', 'dex_connectors.asana.models')


# ProjectReference identifies an Asana project.
@dataclass(frozen=True, slots=True)
class ProjectReference:
    # ID is the project's gid.
    id: str = ''
    # Name is the project name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ProjectReference, '__module__', 'dex_connectors.asana.models')


# SectionReference identifies a section of a project.
@dataclass(frozen=True, slots=True)
class SectionReference:
    # ID is the section's gid.
    id: str = ''
    # Name is the section name, such as Approved.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SectionReference, '__module__', 'dex_connectors.asana.models')


# TaskMembership is one project that holds a task, with the task's section in it.
@dataclass(frozen=True, slots=True)
class TaskMembership:
    # Project is the project.
    project: ProjectReference = _dataclass_field(default_factory=ProjectReference)
    # Section is the task's section in the project, or nil when Asana reports none.
    section: SectionReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(TaskMembership, '__module__', 'dex_connectors.asana.models')


# TaskReference identifies another task, such as a parent.
@dataclass(frozen=True, slots=True)
class TaskReference:
    # ID is the task's gid.
    id: str = ''
    # Name is the task name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(TaskReference, '__module__', 'dex_connectors.asana.models')


# WorkspaceReference identifies an Asana workspace or organization.
@dataclass(frozen=True, slots=True)
class WorkspaceReference:
    # ID is the workspace's gid.
    id: str = ''
    # Name is the workspace name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(WorkspaceReference, '__module__', 'dex_connectors.asana.models')


# EnumOptionReference identifies one option of an enum or multi-enum custom field.
@dataclass(frozen=True, slots=True)
class EnumOptionReference:
    # ID is the enum option's gid.
    id: str = ''
    # Name is the option name, such as High.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(EnumOptionReference, '__module__', 'dex_connectors.asana.models')


# CustomFieldValue is one custom field's value on a task. DisplayValue is Asana's text rendering for every
# field type; the typed value is set for text, number, enum, and multi-enum fields.
@dataclass(frozen=True, slots=True)
class CustomFieldValue:
    # ID is the custom field's gid.
    id: str = ''
    # Name is the custom field name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Type is Asana's resource_subtype, such as text, number, enum, multi_enum, date, or people.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DisplayValue is the value as Asana displays it, or empty for an unset field.
    display_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayValue', 'dex_json_omit_default': True})
    # TextValue is a text field's value.
    text_value: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'textValue', 'dex_json_omit_default': True})
    # NumberValue is a number field's value.
    number_value: float | None = _dataclass_field(default=None, metadata={'dex_json_name': 'numberValue', 'dex_json_omit_default': True})
    # EnumOption is an enum field's selected option.
    enum_option: EnumOptionReference | None = _dataclass_field(default=None, metadata={'dex_json_name': 'enumOption', 'dex_json_omit_default': True})
    # MultiEnumOptions lists a multi-enum field's selected options.
    multi_enum_options: tuple[EnumOptionReference, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'multiEnumOptions', 'dex_json_omit_default': True})


setattr(CustomFieldValue, '__module__', 'dex_connectors.asana.models')


# Task is the connector's view of one Asana task. Asana's own status vocabulary is the completed flag;
# section membership carries the workflow state, so moving a task between sections is its status change.
@dataclass(frozen=True, slots=True)
class Task:
    # ID is the task's Asana gid, a decimal string.
    id: str = ''
    # Name is the task name.
    name: str = ''
    # ResourceSubtype is default_task, milestone, approval, or another Asana task subtype.
    resource_subtype: str = _dataclass_field(default='', metadata={'dex_json_name': 'resourceSubtype', 'dex_json_omit_default': True})
    # IsCompleted reports a completed task.
    is_completed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCompleted'})
    # CompletedAt is when the task was completed, or nil while it is incomplete.
    completed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'completedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Assignee is the assigned user, or nil for an unassigned task. Email addresses are never read.
    assignee: UserReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # DueOn is the due date as YYYY-MM-DD, or empty.
    due_on: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueOn', 'dex_json_omit_default': True})
    # DueAt is the due date and time, set only for a task with a due time.
    due_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # StartOn is the start date as YYYY-MM-DD, or empty.
    start_on: str = _dataclass_field(default='', metadata={'dex_json_name': 'startOn', 'dex_json_omit_default': True})
    # Memberships lists up to 20 projects that hold the task and the section of each.
    memberships: tuple[TaskMembership, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Parent is the parent task of a subtask, or nil.
    parent: TaskReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Workspace is the task's workspace, returned by getTask and updateTask only.
    workspace: WorkspaceReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Notes is the plain-text description, returned by getTask and updateTask only, at most 65536 characters.
    notes: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsNotesTruncated reports that Notes stopped at 65536 characters.
    is_notes_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isNotesTruncated', 'dex_json_omit_default': True})
    # CustomFields lists up to 50 custom field values, returned by getTask and updateTask only.
    custom_fields: tuple[CustomFieldValue, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})
    # PermalinkURL is the task's Asana web URL.
    permalink_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'permalinkUrl', 'dex_json_omit_default': True})
    # CreatedAt is when the task was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # ModifiedAt is when the task, or an association such as a project or comment, last changed.
    modified_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'modifiedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.completed_at, str):
            object.__setattr__(self, 'completed_at', parse_go_datetime_input(self.completed_at, 'completed_at'))
        if isinstance(self.due_at, str):
            object.__setattr__(self, 'due_at', parse_go_datetime_input(self.due_at, 'due_at'))
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))
        if isinstance(self.modified_at, str):
            object.__setattr__(self, 'modified_at', parse_go_datetime_input(self.modified_at, 'modified_at'))


setattr(Task, '__module__', 'dex_connectors.asana.models')


# ListTasksOutput is one page of tasks in Asana's order.
@dataclass(frozen=True, slots=True)
class ListTasksOutput:
    # Tasks lists the page's tasks without notes or custom field values.
    tasks: tuple[Task, ...] = ()
    # NextOffset continues the list; it is empty on the last page.
    next_offset: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextOffset', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tasks', _replace_nil_slices(self.tasks, 1))


ListTasksOutput.__annotations__['tasks'] = tuple[Task, ...] | None
ListTasksOutput.__dataclass_fields__['tasks'].type = tuple[Task, ...] | None
setattr(ListTasksOutput, '__module__', 'dex_connectors.asana.models')


# GetTaskInput identifies one task.
@dataclass(frozen=True, slots=True)
class GetTaskInput:
    # TaskID is the task's gid, the number in its Asana URL, such as 1204567890123456.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})


setattr(GetTaskInput, '__module__', 'dex_connectors.asana.models')


# CustomFieldValueInput sets one custom field. Set exactly one of Text, Number, EnumOptionID, and
# MultiEnumOptionIDs, matching the field's type; other custom field types are not supported.
@dataclass(frozen=True, slots=True)
class CustomFieldValueInput:
    # CustomFieldID is the custom field's gid.
    custom_field_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customFieldId'})
    # Text sets a text field.
    text: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Number sets a number field.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # EnumOptionID selects an enum field's option by its gid, not its name.
    enum_option_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'enumOptionId', 'dex_json_omit_default': True})
    # MultiEnumOptionIDs selects a multi-enum field's options by gid, replacing the current selection.
    multi_enum_option_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'multiEnumOptionIds', 'dex_json_omit_default': True})


setattr(CustomFieldValueInput, '__module__', 'dex_connectors.asana.models')


# CreateTaskInput describes one task. Set exactly one of ProjectID and WorkspaceID; SectionID requires
# ProjectID.
@dataclass(frozen=True, slots=True)
class CreateTaskInput:
    # Name is the one-line task name, at most 1024 characters.
    name: str = ''
    # Notes is the plain-text description, at most 65536 characters; blank sends none.
    notes: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ProjectID adds the task to this project gid; its workspace becomes the task's workspace.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # SectionID places the task in this section of ProjectID; blank uses the project's default section.
    section_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'sectionId', 'dex_json_omit_default': True})
    # WorkspaceID creates the task in this workspace gid without a project.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId', 'dex_json_omit_default': True})
    # AssigneeID assigns the task to me, a user gid, or the email address of a workspace member; blank
    # leaves it unassigned.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # DueOn is the due date as YYYY-MM-DD; blank sends none.
    due_on: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueOn', 'dex_json_omit_default': True})
    # CustomFields sets up to 20 text, number, enum, or multi-enum custom field values.
    custom_fields: tuple[CustomFieldValueInput, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})


setattr(CreateTaskInput, '__module__', 'dex_connectors.asana.models')


# CreateTaskOutput identifies the created task. On providerRejected and uncertain, TaskID is empty and the
# requested name and container are echoed so the application can reconcile.
@dataclass(frozen=True, slots=True)
class CreateTaskOutput:
    # TaskID is the new task's gid.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId', 'dex_json_omit_default': True})
    # Name echoes the requested name.
    name: str = ''
    # ProjectID echoes the requested project.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # SectionID echoes the requested section.
    section_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'sectionId', 'dex_json_omit_default': True})
    # WorkspaceID echoes the requested workspace.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId', 'dex_json_omit_default': True})
    # PermalinkURL is the new task's Asana web URL.
    permalink_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'permalinkUrl', 'dex_json_omit_default': True})
    # CreatedAt is when Asana created the task.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))


setattr(CreateTaskOutput, '__module__', 'dex_connectors.asana.models')


# UpdateTaskInput changes one task. Every set field is an absolute value, so sending the same update twice
# leaves the same task. Set at least one change.
@dataclass(frozen=True, slots=True)
class UpdateTaskInput:
    # TaskID is the task's gid.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # IsCompleted completes the task when true and reopens it when false; nil leaves it.
    is_completed: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isCompleted', 'dex_json_omit_default': True})
    # AssigneeID assigns the task to me, a user gid, or a workspace member's email address; a pointer to
    # "" unassigns it, and nil leaves the assignee.
    assignee_id: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # DueOn sets the due date as YYYY-MM-DD; a pointer to "" clears it, and nil leaves it.
    due_on: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueOn', 'dex_json_omit_default': True})
    # CustomFields sets up to 20 text, number, enum, or multi-enum custom field values.
    custom_fields: tuple[CustomFieldValueInput, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})
    # SectionID moves the task to this section, removing it from the project's other sections; blank leaves
    # its sections. Asana places a moved task at the top of the section.
    section_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'sectionId', 'dex_json_omit_default': True})


setattr(UpdateTaskInput, '__module__', 'dex_connectors.asana.models')


# UpdateTaskOutput reports the update. On updated, Task is the task read back after every change.
@dataclass(frozen=True, slots=True)
class UpdateTaskOutput:
    # TaskID echoes the requested task.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # Task is the task after the update, set only on updated.
    task: Task | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsMovedToSection reports that Asana confirmed the section move, which happens before field changes.
    is_moved_to_section: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isMovedToSection', 'dex_json_omit_default': True})


setattr(UpdateTaskOutput, '__module__', 'dex_connectors.asana.models')


# AddCommentInput describes one comment on one task.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # TaskID is the task's gid.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # Text is the plain-text comment, at most 65536 characters; line breaks are kept and the text is never
    # read as markup.
    text: str = ''


setattr(AddCommentInput, '__module__', 'dex_connectors.asana.models')


# AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain, CommentID is
# empty and TaskID echoes the request.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # TaskID echoes the requested task.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # CommentID is the new comment story's gid.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # Author is the user that wrote the comment, the connection's user.
    author: UserReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # CreatedAt is when Asana stored the comment.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))


setattr(AddCommentOutput, '__module__', 'dex_connectors.asana.models')
