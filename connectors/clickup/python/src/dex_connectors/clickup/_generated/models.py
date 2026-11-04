# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchTasksInput filters one Workspace's tasks. Every non-empty filter narrows the search, and the
# values of one list filter are alternatives.
@dataclass(frozen=True, slots=True)
class SearchTasksInput:
    # WorkspaceID is the Workspace (ClickUp API team) ID, the first number in a ClickUp web address.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId'})
    # ListIDs keeps tasks of these Lists.
    list_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'listIds', 'dex_json_omit_default': True})
    # FolderIDs keeps tasks of Lists in these Folders.
    folder_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'folderIds', 'dex_json_omit_default': True})
    # SpaceIDs keeps tasks of these Spaces.
    space_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'spaceIds', 'dex_json_omit_default': True})
    # Statuses keeps tasks in these status names, such as in progress.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # AssigneeIDs keeps tasks assigned to any of these user IDs.
    assignee_ids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'assigneeIds', 'dex_json_omit_default': True})
    # Tags keeps tasks with any of these tag names.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # UpdatedAfter keeps tasks updated after this time, for polling changes since a checkpoint.
    updated_after: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAfter', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # IsClosedIncluded also returns tasks in a closed status; ClickUp excludes them by default.
    is_closed_included: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isClosedIncluded', 'dex_json_omit_default': True})
    # AreSubtasksIncluded also returns subtasks; ClickUp excludes them by default.
    are_subtasks_included: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'areSubtasksIncluded', 'dex_json_omit_default': True})
    # OrderBy is created, updated, due_date, or id; empty uses ClickUp's default order by creation.
    order_by: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'orderBy', 'dex_json_omit_default': True})
    # IsReverse reverses ClickUp's order.
    is_reverse: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isReverse', 'dex_json_omit_default': True})
    # Page is the zero-based page to read; continue with SearchTasksOutput.NextPage.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchTasksInput, '__module__', 'dex_connectors.clickup.models')


# TaskStatus is a status of a List's workflow.
@dataclass(frozen=True, slots=True)
class TaskStatus:
    # Name is the status name as the List defines it, such as in progress.
    name: str = ''
    # Type is ClickUp's status type: open, custom, done, or closed.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(TaskStatus, '__module__', 'dex_connectors.clickup.models')


# User is a ClickUp user without contact details.
@dataclass(frozen=True, slots=True)
class User:
    # ID is ClickUp's numeric user ID, the value task assignees use.
    id: int = 0
    # Username is the user's display name.
    username: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(User, '__module__', 'dex_connectors.clickup.models')


# CustomFieldValue is one custom field of a task.
@dataclass(frozen=True, slots=True)
class CustomFieldValue:
    # ID is the custom field's ID, a UUID.
    id: str = ''
    # Name is the custom field's name.
    name: str = ''
    # Type is ClickUp's field type, such as short_text, number, drop_down, labels, or users.
    type: str = ''
    # Value is ClickUp's value passed through unchanged, or absent when the field is unset. A drop_down
    # value is an option ID or index and a users value is a list of users; see ClickUp's Custom Fields guide.
    value: Any = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(CustomFieldValue, '__module__', 'dex_connectors.clickup.models')


# Task is one ClickUp task as the connector returns it.
@dataclass(frozen=True, slots=True)
class Task:
    # ID is ClickUp's task ID, such as 86b2x4k7q.
    id: str = ''
    # CustomID is the Workspace's custom task ID, such as DEV-123, when custom task IDs are enabled.
    custom_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customId', 'dex_json_omit_default': True})
    # Name is the task name.
    name: str = ''
    # URL is the task's ClickUp web address.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is the task's status in its List's workflow.
    status: TaskStatus = _dataclass_field(default_factory=TaskStatus)
    # Priority is 1 urgent through 4 low, or zero when the task has no priority.
    priority: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # PriorityName is ClickUp's priority name, such as urgent, or empty without a priority.
    priority_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'priorityName', 'dex_json_omit_default': True})
    # DueAt is the due time in UTC, or nil without a due date.
    due_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # StartAt is the start time in UTC, or nil without a start date.
    start_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'startAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # CreatedAt is when the task was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the task last changed.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ClosedAt is when the task reached a closed status, or nil.
    closed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'closedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # IsArchived reports an archived task.
    is_archived: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isArchived', 'dex_json_omit_default': True})
    # Creator is the user who created the task, when ClickUp reports one.
    creator: User | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Assignees are the users the task is assigned to.
    assignees: tuple[User, ...] = ()
    # Tags are the task's tag names.
    tags: tuple[str, ...] = ()
    # ParentTaskID is the parent task of a subtask, or empty for a top-level task.
    parent_task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentTaskId', 'dex_json_omit_default': True})
    # WorkspaceID is the Workspace (ClickUp API team) the task belongs to.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId', 'dex_json_omit_default': True})
    # ListID is the task's home List.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId', 'dex_json_omit_default': True})
    # ListName is the home List's name.
    list_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'listName', 'dex_json_omit_default': True})
    # FolderID is the home List's Folder, or empty for a folderless List.
    folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'folderId', 'dex_json_omit_default': True})
    # SpaceID is the Space of the home List.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId', 'dex_json_omit_default': True})
    # MarkdownDescription is the description in Markdown; only getTask returns it.
    markdown_description: str = _dataclass_field(default='', metadata={'dex_json_name': 'markdownDescription', 'dex_json_omit_default': True})
    # IsDescriptionTruncated reports a description longer than MaxDescriptionBytes.
    is_description_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescriptionTruncated', 'dex_json_omit_default': True})
    # CustomFields are the task's applicable custom fields with their values; only getTask returns them.
    custom_fields: tuple[CustomFieldValue, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'assignees', _replace_nil_slices(self.assignees, 1))
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


Task.__annotations__['assignees'] = tuple[User, ...] | None
Task.__dataclass_fields__['assignees'].type = tuple[User, ...] | None
Task.__annotations__['tags'] = tuple[str, ...] | None
Task.__dataclass_fields__['tags'].type = tuple[str, ...] | None
setattr(Task, '__module__', 'dex_connectors.clickup.models')


# SearchTasksOutput is one page of matching tasks.
@dataclass(frozen=True, slots=True)
class SearchTasksOutput:
    # Tasks are the page's tasks without descriptions or custom fields; read those with getTask.
    tasks: tuple[Task, ...] = ()
    # Page is the page that was read.
    page: int = 0
    # NextPage is the next page to read, or nil on the last page.
    next_page: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'nextPage', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tasks', _replace_nil_slices(self.tasks, 1))


SearchTasksOutput.__annotations__['tasks'] = tuple[Task, ...] | None
SearchTasksOutput.__dataclass_fields__['tasks'].type = tuple[Task, ...] | None
setattr(SearchTasksOutput, '__module__', 'dex_connectors.clickup.models')


# GetTaskInput names one task.
@dataclass(frozen=True, slots=True)
class GetTaskInput:
    # TaskID is ClickUp's task ID, the code after /t/ in the task's web address, or a custom task ID
    # such as DEV-123 when IsCustomTaskID is set.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # IsCustomTaskID reads TaskID as the Workspace's custom task ID; it requires WorkspaceID.
    is_custom_task_id: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCustomTaskId', 'dex_json_omit_default': True})
    # WorkspaceID is the Workspace of a custom task ID; leave it empty for an ordinary task ID.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId', 'dex_json_omit_default': True})


setattr(GetTaskInput, '__module__', 'dex_connectors.clickup.models')


# CustomFieldInput sets one custom field on a new task.
@dataclass(frozen=True, slots=True)
class CustomFieldInput:
    # ID is the custom field's ID, from getTask or ClickUp's Get Accessible Custom Fields.
    id: str = ''
    # Value is the JSON value ClickUp expects for the field's type, such as "text", 5, or an option ID.
    value: Any = None


setattr(CustomFieldInput, '__module__', 'dex_connectors.clickup.models')


# CreateTaskInput is one task to create in a List.
@dataclass(frozen=True, slots=True)
class CreateTaskInput:
    # ListID is the List to create the task in, the number after /li/ in the List's web address.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # Name is the task name. A retried attempt finds its earlier task by this name, so a name that
    # carries the application's own request key, such as [REQ-1042], makes reconciliation unambiguous.
    name: str = ''
    # MarkdownDescription is the description in Markdown, at most MaxDescriptionBytes; empty sets none.
    markdown_description: str = _dataclass_field(default='', metadata={'dex_json_name': 'markdownDescription', 'dex_json_omit_default': True})
    # Status is a status name of the List's workflow, such as in progress; empty uses the List's first status.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Priority is 1 urgent through 4 low; zero sets no priority.
    priority: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # DueAt is the due date and time; nil sets none.
    due_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # AssigneeIDs are the users to assign, such as a findMemberByEmail result's ID.
    assignee_ids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'assigneeIds', 'dex_json_omit_default': True})
    # Tags are tag names of the List's Space to add.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ParentTaskID creates a subtask of this task, which must be in the same List; empty creates a task.
    parent_task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentTaskId', 'dex_json_omit_default': True})
    # CustomFields set custom field values; ClickUp ignores fields that do not apply to the task.
    custom_fields: tuple[CustomFieldInput, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})
    # IsCreatorNotified also notifies the token's user; assignees and watchers are always notified.
    is_creator_notified: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCreatorNotified', 'dex_json_omit_default': True})


setattr(CreateTaskInput, '__module__', 'dex_connectors.clickup.models')


# CreateTaskOutput is the created task.
@dataclass(frozen=True, slots=True)
class CreateTaskOutput:
    # ListID echoes the requested List.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # Name echoes the requested name.
    name: str = ''
    # Task is the task ClickUp created, or empty on every branch except created.
    task: Task = _dataclass_field(default_factory=Task)
    # WasAlreadyApplied reports that an earlier attempt of this Step created the task without a
    # confirmed outcome and this attempt found it in the List, so nothing was sent again.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(CreateTaskOutput, '__module__', 'dex_connectors.clickup.models')


# UpdateTaskInput changes one task. Only set fields change; every change is an absolute value or a set
# membership, so a repeated update leaves the same task.
@dataclass(frozen=True, slots=True)
class UpdateTaskInput:
    # TaskID is ClickUp's task ID.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # Name renames the task when set.
    name: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # MarkdownDescription replaces the description with Markdown when set; a pointer to "" clears it.
    markdown_description: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'markdownDescription', 'dex_json_omit_default': True})
    # Status moves the task to a status name of its List's workflow, such as in progress; empty keeps it.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Priority sets 1 urgent through 4 low; zero keeps the current priority.
    priority: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # DueAt sets the due date and time; nil keeps it.
    due_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # AddAssigneeIDs assigns these users; an existing assignee stays assigned.
    add_assignee_ids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addAssigneeIds', 'dex_json_omit_default': True})
    # RemoveAssigneeIDs unassigns these users; a user who is not assigned is ignored.
    remove_assignee_ids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeAssigneeIds', 'dex_json_omit_default': True})


setattr(UpdateTaskInput, '__module__', 'dex_connectors.clickup.models')


# UpdateTaskOutput is the task after the update.
@dataclass(frozen=True, slots=True)
class UpdateTaskOutput:
    # Task is the task ClickUp returned after applying the change.
    task: Task = _dataclass_field(default_factory=Task)


setattr(UpdateTaskOutput, '__module__', 'dex_connectors.clickup.models')


# UpdateTaskTagsInput adds tags to and removes tags from one task.
@dataclass(frozen=True, slots=True)
class UpdateTaskTagsInput:
    # TaskID is ClickUp's task ID.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # AddTags are tag names to add; a tag the task already has stays.
    add_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addTags', 'dex_json_omit_default': True})
    # RemoveTags are tag names to remove from the task; the tag stays in the Space.
    remove_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTags', 'dex_json_omit_default': True})


setattr(UpdateTaskTagsInput, '__module__', 'dex_connectors.clickup.models')


# UpdateTaskTagsOutput is the task's tags after the update.
@dataclass(frozen=True, slots=True)
class UpdateTaskTagsOutput:
    # TaskID is the updated task.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # Tags are the task's tag names read back after every change, as ClickUp stores them.
    tags: tuple[str, ...] = ()
    # AppliedChanges counts the tag requests ClickUp accepted, also on providerRejected.
    applied_changes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'appliedChanges'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


UpdateTaskTagsOutput.__annotations__['tags'] = tuple[str, ...] | None
UpdateTaskTagsOutput.__dataclass_fields__['tags'].type = tuple[str, ...] | None
setattr(UpdateTaskTagsOutput, '__module__', 'dex_connectors.clickup.models')


# AddCommentInput is one plain-text comment to add to a task.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # TaskID is ClickUp's task ID.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # Text is the comment's plain text, at most MaxCommentBytes; ClickUp shows it as written.
    text: str = ''
    # IsCreatorNotified also notifies the token's user; assignees and watchers are always notified.
    is_creator_notified: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCreatorNotified', 'dex_json_omit_default': True})


setattr(AddCommentInput, '__module__', 'dex_connectors.clickup.models')


# AddCommentOutput is the comment on the task.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # TaskID is the task the comment belongs to.
    task_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'taskId'})
    # CommentID is ClickUp's comment ID, or empty on every branch except added.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # CreatedAt is when ClickUp recorded the comment, when it reported the time.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WasAlreadyApplied reports that an earlier attempt of this Step added the comment without a
    # confirmed outcome and this attempt found it on the task, so nothing was sent again.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(AddCommentOutput, '__module__', 'dex_connectors.clickup.models')


# FindMemberByEmailInput names one Workspace and one email address.
@dataclass(frozen=True, slots=True)
class FindMemberByEmailInput:
    # WorkspaceID is the Workspace (ClickUp API team) ID, the first number in a ClickUp web address.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId'})
    # Email is one bare email address, compared without regard to case.
    email: str = ''


setattr(FindMemberByEmailInput, '__module__', 'dex_connectors.clickup.models')


# Member is one member of a Workspace.
@dataclass(frozen=True, slots=True)
class Member:
    # ID is ClickUp's numeric user ID, the value task assignees use.
    id: int = 0
    # Username is the member's display name.
    username: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the member's email address as ClickUp stores it.
    email: str = ''
    # Role is 1 owner, 2 admin, 3 member, or 4 guest, or zero when ClickUp does not report it.
    role: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # WorkspaceID is the Workspace the member belongs to.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId'})


setattr(Member, '__module__', 'dex_connectors.clickup.models')
