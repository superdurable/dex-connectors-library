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


# GetAuthenticatedProfileInput contains the provider request fields for get authenticated profile.
@dataclass(frozen=True, slots=True)
class GetAuthenticatedProfileInput:
    pass


setattr(GetAuthenticatedProfileInput, '__module__', 'dex_connectors.github.models')


# AuthenticatedProfile represents the connector's authenticated profile data.
@dataclass(frozen=True, slots=True)
class AuthenticatedProfile:
    # Subject is the message subject.
    subject: str = ''
    # Login is the login for authenticated profile.
    login: str = ''
    # Name is the name for authenticated profile.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # VerifiedEmail is the verified email for authenticated profile.
    verified_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'verifiedEmail'})
    # AvatarURL is the avatar URL for authenticated profile.
    avatar_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'avatarUrl', 'dex_json_omit_default': True})
    # ProfileURL is the profile URL for authenticated profile.
    profile_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'profileUrl', 'dex_json_omit_default': True})
    # Company is the company for authenticated profile.
    company: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # BlogURL is the blog URL for authenticated profile.
    blog_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'blogUrl', 'dex_json_omit_default': True})
    # Location is the location for authenticated profile.
    location: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Bio is the bio for authenticated profile.
    bio: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TwitterUsername is the twitter username for authenticated profile.
    twitter_username: str = _dataclass_field(default='', metadata={'dex_json_name': 'twitterUsername', 'dex_json_omit_default': True})
    # PublicRepos is the public repos for authenticated profile.
    public_repos: int = _dataclass_field(default=0, metadata={'dex_json_name': 'publicRepos'})
    # Followers is the followers for authenticated profile.
    followers: int = 0
    # Following is the following for authenticated profile.
    following: int = 0
    # CreatedAt is the created at for authenticated profile.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is the updated at for authenticated profile.
    updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(AuthenticatedProfile, '__module__', 'dex_connectors.github.models')


# ListPublicRepositoriesInput contains the provider request fields for list public repositories.
@dataclass(frozen=True, slots=True)
class ListPublicRepositoriesInput:
    # Login specifies login for list public repositories input.
    login: str = ''
    # Limit specifies limit for list public repositories input.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListPublicRepositoriesInput, '__module__', 'dex_connectors.github.models')


# Repository represents the connector's repository data.
@dataclass(frozen=True, slots=True)
class Repository:
    # ID is the stable provider identifier.
    id: int = 0
    # Name is the name for repository.
    name: str = ''
    # FullName is the full name for repository.
    full_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullName'})
    # Description is the description for repository.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ProfileURL is the profile URL for repository.
    profile_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'profileUrl', 'dex_json_omit_default': True})
    # HomepageURL is the homepage URL for repository.
    homepage_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'homepageUrl', 'dex_json_omit_default': True})
    # Language is the language for repository.
    language: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Topics is the topics for repository.
    topics: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Fork is the fork for repository.
    fork: bool = False
    # Archived is the archived for repository.
    archived: bool = False
    # Disabled is the disabled for repository.
    disabled: bool = False
    # StargazersCount is the stargazers count for repository.
    stargazers_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'stargazersCount'})
    # ForksCount is the forks count for repository.
    forks_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'forksCount'})
    # OpenIssuesCount is the open issues count for repository.
    open_issues_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'openIssuesCount'})
    # CreatedAt is the created at for repository.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is the updated at for repository.
    updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # PushedAt is the pushed at for repository.
    pushed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'pushedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Repository, '__module__', 'dex_connectors.github.models')


# PublicRepositories represents the connector's public repositories data.
@dataclass(frozen=True, slots=True)
class PublicRepositories:
    # Repositories is the repositories for public repositories.
    repositories: tuple[Repository, ...] = ()
    # Truncated is the truncated for public repositories.
    truncated: bool = False

    def __post_init__(self) -> None:
        object.__setattr__(self, 'repositories', _replace_nil_slices(self.repositories, 1))


PublicRepositories.__annotations__['repositories'] = tuple[Repository, ...] | None
PublicRepositories.__dataclass_fields__['repositories'].type = tuple[Repository, ...] | None
setattr(PublicRepositories, '__module__', 'dex_connectors.github.models')


# ListMergedPullRequestsInput selects one page of pull requests merged within an inclusive time window.
@dataclass(frozen=True, slots=True)
class ListMergedPullRequestsInput:
    # Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
    owner: str = ''
    # Repository is the repository name without its owner, such as hello-world.
    repository: str = ''
    # MergedAfter is the inclusive window start. GitHub search receives it in UTC with second precision.
    merged_after: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'mergedAfter', 'dex_json_format': 'rfc3339'})
    # MergedBefore is the inclusive window end and must be after MergedAfter. GitHub search receives it in
    # UTC with second precision.
    merged_before: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'mergedBefore', 'dex_json_format': 'rfc3339'})
    # PageSize is 1 through 100. Zero selects 30.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Page is one-based. Zero selects 1.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListMergedPullRequestsInput, '__module__', 'dex_connectors.github.models')


# MergedPullRequest is one merged pull request with bounded text.
@dataclass(frozen=True, slots=True)
class MergedPullRequest:
    # Number is the pull request number within its repository.
    number: int = 0
    # Title is the trimmed pull request title, bounded to 1024 bytes.
    title: str = ''
    # Body is the beginning of the pull request body, bounded by the maxPullRequestBodyCharacters
    # configuration field.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # BodyTruncated reports that Body omits the end of the pull request body.
    body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'bodyTruncated'})
    # URL is the pull request's GitHub web URL, or empty when GitHub returns an unsafe URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # AuthorLogin is the author's GitHub login, or empty when GitHub omits the author.
    author_login: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorLogin', 'dex_json_omit_default': True})
    # MergedAt is the merge time in UTC.
    merged_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'mergedAt', 'dex_json_format': 'rfc3339'})
    # Labels holds up to 100 non-empty label names, each bounded to 256 bytes.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(MergedPullRequest, '__module__', 'dex_connectors.github.models')


# MergedPullRequestPage is one bounded GitHub search page. NextPage is zero when no page follows.
@dataclass(frozen=True, slots=True)
class MergedPullRequestPage:
    # PullRequests holds this page's merged pull requests, newest created first.
    pull_requests: tuple[MergedPullRequest, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'pullRequests'})
    # TotalCount is GitHub's count of every matching pull request. It can exceed the 1000 results that
    # GitHub search serves; narrow the window to read them all.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # IncompleteResults reports that GitHub search timed out before it found every match.
    incomplete_results: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'incompleteResults'})
    # NextPage is the one-based page that follows, or zero when this is the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'pull_requests', _replace_nil_slices(self.pull_requests, 1))


MergedPullRequestPage.__annotations__['pull_requests'] = tuple[MergedPullRequest, ...] | None
MergedPullRequestPage.__dataclass_fields__['pull_requests'].type = tuple[MergedPullRequest, ...] | None
setattr(MergedPullRequestPage, '__module__', 'dex_connectors.github.models')


# ListPullRequestFilesInput selects one page of files changed by one pull request.
@dataclass(frozen=True, slots=True)
class ListPullRequestFilesInput:
    # Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
    owner: str = ''
    # Repository is the repository name without its owner, such as hello-world.
    repository: str = ''
    # Number is the positive pull request number.
    number: int = 0
    # PageSize is 1 through 100. Zero selects 50.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Page is one-based. Zero selects 1.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListPullRequestFilesInput, '__module__', 'dex_connectors.github.models')


# PullRequestFile is one file changed by a pull request, with a bounded patch.
@dataclass(frozen=True, slots=True)
class PullRequestFile:
    # Filename is the file's repository path after the change.
    filename: str = ''
    # PreviousFilename is the file's path before a rename, or empty.
    previous_filename: str = _dataclass_field(default='', metadata={'dex_json_name': 'previousFilename', 'dex_json_omit_default': True})
    # Status is GitHub's change status, such as added, modified, removed, or renamed.
    status: str = ''
    # Additions is the number of added lines.
    additions: int = 0
    # Deletions is the number of removed lines.
    deletions: int = 0
    # Changes is the number of changed lines.
    changes: int = 0
    # Patch is the beginning of the file's unified diff, bounded by the maxPatchCharacters configuration
    # field. GitHub omits the patch for a binary or very large file, which leaves Patch empty.
    patch: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PatchTruncated reports that Patch omits the end of GitHub's patch.
    patch_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'patchTruncated'})


setattr(PullRequestFile, '__module__', 'dex_connectors.github.models')


# PullRequestFilePage is one bounded page of changed files. NextPage is zero when no page follows.
@dataclass(frozen=True, slots=True)
class PullRequestFilePage:
    # Files holds this page's changed files in GitHub's order.
    files: tuple[PullRequestFile, ...] = ()
    # NextPage is the one-based page that follows, or zero when this is the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'files', _replace_nil_slices(self.files, 1))


PullRequestFilePage.__annotations__['files'] = tuple[PullRequestFile, ...] | None
PullRequestFilePage.__dataclass_fields__['files'].type = tuple[PullRequestFile, ...] | None
setattr(PullRequestFilePage, '__module__', 'dex_connectors.github.models')


# ListReleasesInput selects a single bounded page; date filtering is performed by the caller.
@dataclass(frozen=True, slots=True)
class ListReleasesInput:
    # Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
    owner: str = ''
    # Repository is the repository name without its owner.
    repository: str = ''
    # PageSize is 1 through 100. Zero selects 30.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Page is one-based. Zero selects 1.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # MaxBodyCharacters bounds each release's notes to 1 through 16384 Unicode characters. Zero selects 4000.
    max_body_characters: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxBodyCharacters', 'dex_json_omit_default': True})


setattr(ListReleasesInput, '__module__', 'dex_connectors.github.models')


# Release contains provider identity and bounded release notes, never credentials or asset contents.
@dataclass(frozen=True, slots=True)
class Release:
    # ID is the stable GitHub release ID; a tag or title can be edited without changing it.
    id: int = 0
    # TagName is the release's tag, bounded to 255 bytes.
    tag_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'tagName'})
    # Name is the optional release title, bounded to 1024 bytes.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is the safe public web URL, or empty if GitHub returned an unsafe URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Body is the beginning of the release notes, limited by MaxBodyCharacters.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # BodyTruncated reports whether the notes were shortened.
    body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'bodyTruncated'})
    # IsDraft reports an unpublished draft visible to this grant.
    is_draft: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDraft'})
    # IsPrerelease reports GitHub's prerelease flag.
    is_prerelease: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPrerelease'})
    # CreatedAt is GitHub's creation timestamp; it must not be used as publication time.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # PublishedAt is the publication time in UTC, absent for an unpublished draft.
    published_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'publishedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Release, '__module__', 'dex_connectors.github.models')


# ReleasePage contains one page in provider order, which is not necessarily publication-date order.
@dataclass(frozen=True, slots=True)
class ReleasePage:
    # Releases holds the page, including drafts visible to the grant and prereleases.
    releases: tuple[Release, ...] = ()
    # NextPage is the following page number, or zero when GitHub provides no next link.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'releases', _replace_nil_slices(self.releases, 1))


ReleasePage.__annotations__['releases'] = tuple[Release, ...] | None
ReleasePage.__dataclass_fields__['releases'].type = tuple[Release, ...] | None
setattr(ReleasePage, '__module__', 'dex_connectors.github.models')


# ListCommitsInput selects one page of commits within a time window, optionally from one ref and path.
@dataclass(frozen=True, slots=True)
class ListCommitsInput:
    # Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
    owner: str = ''
    # Repository is the repository name without its owner, such as hello-world.
    repository: str = ''
    # Since is the window start. GitHub receives it as since, in UTC with second precision.
    since: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_format': 'rfc3339'})
    # Until is the window end and must be after Since. GitHub receives it as until, in UTC with second
    # precision.
    until: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_format': 'rfc3339'})
    # Path limits commits to those that changed one file or directory path.
    path: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Ref is a branch, tag, or commit SHA. Empty selects the repository default branch.
    ref: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PageSize is 1 through 100. Zero selects 30.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Page is one-based. Zero selects 1.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListCommitsInput, '__module__', 'dex_connectors.github.models')


# CommitSummary is one commit with a bounded message. It never contains commit email addresses.
@dataclass(frozen=True, slots=True)
class CommitSummary:
    # SHA is the commit's lowercase hexadecimal SHA-1 or SHA-256 object name.
    sha: str = ''
    # Message is the beginning of the commit message, bounded by the maxCommitMessageCharacters
    # configuration field.
    message: str = ''
    # MessageTruncated reports that Message omits the end of the commit message.
    message_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'messageTruncated'})
    # AuthorLogin is the GitHub login linked to the commit author, or empty when GitHub links none.
    author_login: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorLogin', 'dex_json_omit_default': True})
    # AuthorName is the trimmed Git author name, bounded to 256 bytes.
    author_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorName', 'dex_json_omit_default': True})
    # AuthoredAt is the Git author time in UTC.
    authored_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'authoredAt', 'dex_json_format': 'rfc3339'})
    # CommittedAt is the Git committer time in UTC.
    committed_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'committedAt', 'dex_json_format': 'rfc3339'})
    # URL is the commit's GitHub web URL, or empty when GitHub returns an unsafe URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(CommitSummary, '__module__', 'dex_connectors.github.models')


# CommitPage is one bounded page of commits. NextPage is zero when no page follows.
@dataclass(frozen=True, slots=True)
class CommitPage:
    # Commits holds this page's commits in GitHub's order, newest first.
    commits: tuple[CommitSummary, ...] = ()
    # NextPage is the one-based page that follows, or zero when this is the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'commits', _replace_nil_slices(self.commits, 1))


CommitPage.__annotations__['commits'] = tuple[CommitSummary, ...] | None
CommitPage.__dataclass_fields__['commits'].type = tuple[CommitSummary, ...] | None
setattr(CommitPage, '__module__', 'dex_connectors.github.models')
