# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import RFC3339NanoTimestamp, format_rfc3339_nano

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# IssueSearchFilter selects issues; every set field must match, and an empty filter matches every issue
# the connection can see. Lists match any of their values.
@dataclass(frozen=True, slots=True)
class IssueSearchFilter:
    # TeamID limits the search to one team's UUID, such as the teamPicker unit's teamId.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId', 'dex_json_omit_default': True})
    # TeamKey limits the search to the team with this key, such as ENG; set at most one of TeamID and TeamKey.
    team_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamKey', 'dex_json_omit_default': True})
    # StateTypes keeps issues whose workflow state has one of these types, such as started.
    state_types: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'stateTypes', 'dex_json_omit_default': True})
    # StateIDs keeps issues in one of these workflow states.
    state_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'stateIds', 'dex_json_omit_default': True})
    # AssigneeID keeps issues assigned to this user UUID.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # AssigneeEmail keeps issues assigned to the user with this email, compared without case.
    assignee_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeEmail', 'dex_json_omit_default': True})
    # IsUnassigned keeps only unassigned issues; it excludes AssigneeID and AssigneeEmail.
    unassigned: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # LabelIDs keeps issues that carry at least one of these labels.
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})
    # LabelNames keeps issues that carry a label with one of these exact names, such as Bug.
    label_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelNames', 'dex_json_omit_default': True})
    # ProjectID keeps issues of this project UUID.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # Title keeps issues whose title is exactly this text.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TitleContains keeps issues whose title contains this text, compared without case.
    title_contains: str = _dataclass_field(default='', metadata={'dex_json_name': 'titleContains', 'dex_json_omit_default': True})
    # UpdatedAfter keeps issues changed strictly after this time; it is the changed-since watermark.
    updated_after: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAfter', 'dex_json_omit_default': True})
    # CreatedAfter keeps issues created strictly after this time.
    created_after: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAfter', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.updated_after is not None:
            timestamp = self.updated_after
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'updated_after', canonical)
        if self.created_after is not None:
            timestamp = self.created_after
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_after', canonical)


setattr(IssueSearchFilter, '__module__', 'dex_connectors.linear.models')


# SearchIssuesInput requests one page. A later page repeats the same Filter, Order, and IncludesArchived
# with the previous page's NextCursor.
@dataclass(frozen=True, slots=True)
class SearchIssuesInput:
    # Filter selects the issues.
    filter: IssueSearchFilter = _dataclass_field(default_factory=IssueSearchFilter)
    # Order is createdAt or updatedAt; blank uses createdAt.
    order: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # PageSize is 1 to MaxSearchPageSize; zero requests DefaultSearchPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Cursor continues after a previous page's NextCursor; blank reads the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IncludesArchived also returns archived issues.
    includes_archived: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesArchived', 'dex_json_omit_default': True})


setattr(SearchIssuesInput, '__module__', 'dex_connectors.linear.models')


# TeamReference identifies the team that owns an issue or workflow state.
@dataclass(frozen=True, slots=True)
class TeamReference:
    # ID is the team's UUID.
    id: str = ''
    # Key is the team's identifier prefix, such as ENG in ENG-123.
    key: str = ''
    # Name is the team's display name.
    name: str = ''


setattr(TeamReference, '__module__', 'dex_connectors.linear.models')


# WorkflowStateReference identifies an issue's workflow state, Linear's issue status.
@dataclass(frozen=True, slots=True)
class WorkflowStateReference:
    # ID is the workflow state's UUID.
    id: str = ''
    # Name is the team's own name for the state, such as In Progress.
    name: str = ''
    # Type is the state's category, such as started.
    type: str = _dataclass_field(kw_only=True)


setattr(WorkflowStateReference, '__module__', 'dex_connectors.linear.models')


# IssueSummary is the partial issue that searches and writes return.
@dataclass(frozen=True, slots=True)
class IssueSummary:
    # ID is the issue's UUID, which never changes.
    id: str = ''
    # Identifier is the team key and number, such as ENG-123; it changes when the issue moves to another team.
    identifier: str = ''
    # Title is the issue's title.
    title: str = ''
    # URL is the issue's page in Linear.
    url: str = ''
    # Team is the team that owns the issue.
    team: TeamReference = _dataclass_field(default_factory=TeamReference)
    # State is the issue's current workflow state.
    state: WorkflowStateReference = _dataclass_field(kw_only=True)
    # AssigneeID is the assigned user's UUID, or blank when the issue is unassigned.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # Priority is 0 for no priority, 1 urgent, 2 high, 3 medium, or 4 low.
    priority: int = 0
    # LabelIDs are the UUIDs of the issue's labels.
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})
    # DueDate is the YYYY-MM-DD due date, or blank.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # CreatedAt is when the issue was created.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt'})
    # UpdatedAt is when the issue last changed; searches compare UpdatedAfter with it.
    updated_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'updatedAt'})
    # ArchivedAt is when the issue was archived, or nil.
    archived_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'archivedAt', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_at', canonical)
        if self.updated_at is not None:
            timestamp = self.updated_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'updated_at', canonical)
        if self.archived_at is not None:
            timestamp = self.archived_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'archived_at', canonical)


setattr(IssueSummary, '__module__', 'dex_connectors.linear.models')


# SearchIssuesOutput is one page of issue summaries.
@dataclass(frozen=True, slots=True)
class SearchIssuesOutput:
    # Issues are the page's issue summaries in Linear's order.
    issues: tuple[IssueSummary, ...] = ()
    # NextCursor continues the search; blank means this was the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'issues', _replace_nil_slices(self.issues, 1))


SearchIssuesOutput.__annotations__['issues'] = tuple[IssueSummary, ...] | None
SearchIssuesOutput.__dataclass_fields__['issues'].type = tuple[IssueSummary, ...] | None
setattr(SearchIssuesOutput, '__module__', 'dex_connectors.linear.models')


# GetIssueInput names the issue to read.
@dataclass(frozen=True, slots=True)
class GetIssueInput:
    # IssueID is the issue's UUID or its identifier, such as ENG-123. An identifier finds the issue only
    # under its current team key; after a move to another team, use the UUID.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId'})


setattr(GetIssueInput, '__module__', 'dex_connectors.linear.models')


# UserReference identifies a Linear user without the user's email address.
@dataclass(frozen=True, slots=True)
class UserReference:
    # ID is the user's UUID.
    id: str = ''
    # Name is the user's full name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DisplayName is the user's short display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})


setattr(UserReference, '__module__', 'dex_connectors.linear.models')


# IssueLabel is one label applied to an issue.
@dataclass(frozen=True, slots=True)
class IssueLabel:
    # ID is the label's UUID.
    id: str = ''
    # Name is the label's name, such as Bug.
    name: str = ''
    # Color is the label's hex color, such as #eb5757.
    color: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(IssueLabel, '__module__', 'dex_connectors.linear.models')


# ProjectReference identifies the project an issue belongs to.
@dataclass(frozen=True, slots=True)
class ProjectReference:
    # ID is the project's UUID.
    id: str = ''
    # Name is the project's name.
    name: str = ''


setattr(ProjectReference, '__module__', 'dex_connectors.linear.models')


# CycleReference identifies the cycle an issue is planned in.
@dataclass(frozen=True, slots=True)
class CycleReference:
    # ID is the cycle's UUID.
    id: str = ''
    # Number is the cycle's number within its team.
    number: int = 0
    # Name is the cycle's optional name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(CycleReference, '__module__', 'dex_connectors.linear.models')


# IssueReference identifies another issue, such as a parent.
@dataclass(frozen=True, slots=True)
class IssueReference:
    # ID is the issue's UUID.
    id: str = ''
    # Identifier is the issue's team-scoped identifier, such as ENG-123.
    identifier: str = ''


setattr(IssueReference, '__module__', 'dex_connectors.linear.models')


# Issue is one issue as getIssue reads it: the summary fields plus its description and relations.
@dataclass(frozen=True, slots=True)
class Issue:
    # ID is the issue's UUID, which never changes.
    id: str = ''
    # Identifier is the team key and number, such as ENG-123; it changes when the issue moves to another team.
    identifier: str = ''
    # Title is the issue's title.
    title: str = ''
    # URL is the issue's page in Linear.
    url: str = ''
    # Team is the team that owns the issue.
    team: TeamReference = _dataclass_field(default_factory=TeamReference)
    # State is the issue's current workflow state.
    state: WorkflowStateReference = _dataclass_field(kw_only=True)
    # AssigneeID is the assigned user's UUID, or blank when the issue is unassigned.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # Priority is 0 for no priority, 1 urgent, 2 high, 3 medium, or 4 low.
    priority: int = 0
    # LabelIDs are the UUIDs of the issue's labels.
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})
    # DueDate is the YYYY-MM-DD due date, or blank.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # CreatedAt is when the issue was created.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt'})
    # UpdatedAt is when the issue last changed; searches compare UpdatedAfter with it.
    updated_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'updatedAt'})
    # ArchivedAt is when the issue was archived, or nil.
    archived_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'archivedAt', 'dex_json_omit_default': True})
    # Number is the issue's number within its team, such as 123 in ENG-123.
    number: int = 0
    # PriorityLabel is Linear's name for Priority, such as High.
    priority_label: str = _dataclass_field(default='', metadata={'dex_json_name': 'priorityLabel', 'dex_json_omit_default': True})
    # Estimate is the issue's estimate in the team's points, or nil.
    estimate: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Description is the issue's Markdown description, at most MaxDescriptionCharacters characters.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsDescriptionTruncated reports that Description was cut at MaxDescriptionCharacters.
    description_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'descriptionTruncated', 'dex_json_omit_default': True})
    # BranchName is the Git branch name Linear suggests for the issue.
    branch_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'branchName', 'dex_json_omit_default': True})
    # IsTrashed reports that the issue is in the trash.
    trashed: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # Assignee is the assigned user, or nil.
    assignee: UserReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Creator is the user who created the issue, or nil for an integration.
    creator: UserReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Labels are the issue's labels, at most 50.
    labels: tuple[IssueLabel, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Project is the issue's project, or nil.
    project: ProjectReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Cycle is the issue's cycle, or nil.
    cycle: CycleReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Parent is the parent issue of a sub-issue, or nil.
    parent: IssueReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # StartedAt is when the issue entered a started state, or nil.
    started_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'startedAt', 'dex_json_omit_default': True})
    # CompletedAt is when the issue was completed, or nil.
    completed_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'completedAt', 'dex_json_omit_default': True})
    # CanceledAt is when the issue was canceled, or nil.
    canceled_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'canceledAt', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_at', canonical)
        if self.updated_at is not None:
            timestamp = self.updated_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'updated_at', canonical)
        if self.archived_at is not None:
            timestamp = self.archived_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'archived_at', canonical)
        if self.started_at is not None:
            timestamp = self.started_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'started_at', canonical)
        if self.completed_at is not None:
            timestamp = self.completed_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'completed_at', canonical)
        if self.canceled_at is not None:
            timestamp = self.canceled_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'canceled_at', canonical)


setattr(Issue, '__module__', 'dex_connectors.linear.models')


# CreateIssueInput describes one new issue.
@dataclass(frozen=True, slots=True)
class CreateIssueInput:
    # TeamID is the UUID of the team that owns the issue, such as the teamPicker unit's teamId.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # Title is the issue title, 1 to MaxIssueTitleCharacters characters on one line.
    title: str = ''
    # Description is optional Markdown of at most MaxMarkdownCharacters characters.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # StateID is a workflow state of the team from listWorkflowStates; blank uses the team's default state.
    state_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'stateId', 'dex_json_omit_default': True})
    # AssigneeID is the assignee's user UUID from findUserByEmail; blank leaves the issue unassigned.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # LabelIDs are up to MaxLabelIDs label UUIDs of the team or workspace.
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})
    # Priority is 0 (none), 1 (urgent), 2 (high), 3 (medium), or 4 (low); nil leaves Linear's default.
    priority: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # DueDate is an optional YYYY-MM-DD date.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # ProjectID is an optional project UUID.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # ParentID makes the issue a sub-issue of this issue UUID or identifier, such as ENG-123.
    parent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentId', 'dex_json_omit_default': True})


setattr(CreateIssueInput, '__module__', 'dex_connectors.linear.models')


# CreateIssueOutput is the created issue. On every other branch IssueID is blank and the requested team
# and title are echoed so the application can reconcile.
@dataclass(frozen=True, slots=True)
class CreateIssueOutput:
    # TeamID echoes the requested team.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # Title echoes the requested title.
    title: str = ''
    # IssueID is the created issue's UUID, the client-supplied ID every attempt of the Step sent.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId', 'dex_json_omit_default': True})
    # Issue is the created issue as Linear returned or read it.
    issue: IssueSummary | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsReplayed reports that an earlier attempt of this Step had already created the issue, which this
    # attempt read back by its UUID.
    replayed: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(CreateIssueOutput, '__module__', 'dex_connectors.linear.models')


# UpdateIssueInput changes one issue. Every field is optional, but at least one change is required.
# Values are absolute and labels are added or removed as sets, so repeating the update changes nothing.
@dataclass(frozen=True, slots=True)
class UpdateIssueInput:
    # IssueID is the issue's UUID or its identifier, such as ENG-123.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId'})
    # StateID moves the issue to this workflow state of its team, from listWorkflowStates.
    state_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'stateId', 'dex_json_omit_default': True})
    # AssigneeID assigns the issue to this user UUID, from findUserByEmail.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # Unassigns removes the assignee; it excludes AssigneeID.
    unassigns: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # AddedLabelIDs adds these label UUIDs and keeps the issue's other labels.
    added_label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addedLabelIds', 'dex_json_omit_default': True})
    # RemovedLabelIDs removes these label UUIDs; it cannot repeat an added label.
    removed_label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removedLabelIds', 'dex_json_omit_default': True})
    # Priority sets 0 (none), 1 (urgent), 2 (high), 3 (medium), or 4 (low); nil keeps it.
    priority: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # DueDate sets a YYYY-MM-DD due date; blank keeps it.
    due_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'dueDate', 'dex_json_omit_default': True})
    # ClearsDueDate removes the due date; it excludes DueDate.
    clears_due_date: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'clearsDueDate', 'dex_json_omit_default': True})
    # Title replaces the title with 1 to MaxIssueTitleCharacters characters on one line; blank keeps it.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(UpdateIssueInput, '__module__', 'dex_connectors.linear.models')


# UpdateIssueOutput is the issue after the change.
@dataclass(frozen=True, slots=True)
class UpdateIssueOutput:
    # IssueID echoes the requested issue UUID or identifier.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId'})
    # Issue is the issue as Linear returned it after the change; nil on every other branch.
    issue: IssueSummary | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(UpdateIssueOutput, '__module__', 'dex_connectors.linear.models')


# AddCommentInput describes one comment.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # IssueID is the issue's UUID or its identifier, such as ENG-123.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId'})
    # Body is the comment's Markdown, 1 to MaxMarkdownCharacters characters.
    body: str = ''


setattr(AddCommentInput, '__module__', 'dex_connectors.linear.models')


# AddCommentOutput is the added comment. On every other branch only IssueID is set.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # IssueID echoes the requested issue UUID or identifier.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId'})
    # CommentID is the comment's UUID, the client-supplied ID every attempt of the Step sent.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # URL is the comment's link in Linear.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CreatedAt is when Linear created the comment.
    created_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True})
    # IsReplayed reports that an earlier attempt of this Step had already added the comment, which this
    # attempt read back by its UUID.
    replayed: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_at', canonical)


setattr(AddCommentOutput, '__module__', 'dex_connectors.linear.models')


# FindUserByEmailInput names the email address to resolve.
@dataclass(frozen=True, slots=True)
class FindUserByEmailInput:
    # Email is the user's address, such as alice@example.com, compared without case.
    email: str = ''
    # IncludesDisabled also finds a suspended or deactivated user; false finds only active users.
    includes_disabled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesDisabled', 'dex_json_omit_default': True})


setattr(FindUserByEmailInput, '__module__', 'dex_connectors.linear.models')


# User is one workspace member.
@dataclass(frozen=True, slots=True)
class User:
    # ID is the user's UUID, which createIssue and updateIssue accept as AssigneeID.
    id: str = ''
    # Name is the user's full name.
    name: str = ''
    # DisplayName is the user's short display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # Email is the user's email address.
    email: str = ''
    # IsActive reports that the user is not suspended or deactivated.
    active: bool = False
    # IsAdmin reports a workspace admin.
    admin: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # IsGuest reports a guest limited to some teams.
    guest: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # URL is the user's profile in Linear.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(User, '__module__', 'dex_connectors.linear.models')


# ListWorkflowStatesInput names the team whose workflow states to list.
@dataclass(frozen=True, slots=True)
class ListWorkflowStatesInput:
    # TeamID is the team's UUID, such as the teamPicker unit's teamId.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})


setattr(ListWorkflowStatesInput, '__module__', 'dex_connectors.linear.models')


# WorkflowState is one of a team's workflow states, Linear's issue statuses.
@dataclass(frozen=True, slots=True)
class WorkflowState:
    # ID is the state's UUID, which createIssue and updateIssue accept as StateID.
    id: str = ''
    # Name is the team's own name for the state, such as In Progress.
    name: str = ''
    # Type is the state's category, such as started.
    type: str = _dataclass_field(kw_only=True)
    # Position orders the states of one type on the team's board.
    position: float = 0.0
    # Color is the state's hex color.
    color: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(WorkflowState, '__module__', 'dex_connectors.linear.models')


# ListWorkflowStatesOutput is a team's active workflow states, ordered by type from triage to canceled and
# then by position.
@dataclass(frozen=True, slots=True)
class ListWorkflowStatesOutput:
    # TeamID echoes the requested team.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId'})
    # States are the team's workflow states.
    states: tuple[WorkflowState, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'states', _replace_nil_slices(self.states, 1))


ListWorkflowStatesOutput.__annotations__['states'] = tuple[WorkflowState, ...] | None
ListWorkflowStatesOutput.__dataclass_fields__['states'].type = tuple[WorkflowState, ...] | None
setattr(ListWorkflowStatesOutput, '__module__', 'dex_connectors.linear.models')
