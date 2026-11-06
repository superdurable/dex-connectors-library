# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GitHub Query invocation, validation, and provider response mapping."""

import json
import re
import unicodedata
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Any, cast
from urllib.parse import parse_qs, quote, urlsplit, urlunsplit

from dex_connectors.sdk import Call, FailureKind, QueryAttempt

from ._generated import definitions as definitions
from .client import _LOGIN, _REPOSITORY, Client, _decode, _path_escape, _Response
from .models import (
    AuthenticatedProfile,
    CommitPage,
    CommitSummary,
    GetAuthenticatedProfileInput,
    ListCommitsInput,
    ListMergedPullRequestsInput,
    ListPublicRepositoriesInput,
    ListPullRequestFilesInput,
    ListReleasesInput,
    MergedPullRequest,
    MergedPullRequestPage,
    PublicRepositories,
    PullRequestFile,
    PullRequestFilePage,
    Release,
    ReleasePage,
    Repository,
)


def _string(value: Any, maximum: int = 0) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("GitHub response string type differs")
    value = value.strip()
    if maximum:
        value = value.encode("utf-8")[:maximum].decode("utf-8", errors="ignore")
    return cast(str, value)


def _raw_string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("GitHub response string type differs")
    return value


def _integer(value: Any) -> int:
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError("GitHub response integer type differs")
    if not -(1 << 63) <= value < (1 << 63):
        raise ValueError("GitHub response integer exceeds Go int64")
    return value


def _boolean(value: Any) -> bool:
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("GitHub response boolean type differs")
    return value


def _time(value: Any, required: bool = False) -> datetime | None:
    if value is None:
        if required:
            raise ValueError("GitHub response time is absent")
        return None
    if not isinstance(value, str):
        raise ValueError("GitHub response time type differs")
    match = re.fullmatch(
        r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})",
        value,
    )
    if match is None:
        raise ValueError("GitHub response time is invalid")
    year, month, day, hour, minute, second = (int(match.group(index)) for index in range(1, 7))
    raw_fraction = match.group(7) or ""
    if len(raw_fraction) > 6 and raw_fraction[6:].strip("0"):
        raise ValueError("GitHub response time cannot be represented exactly as datetime")
    fraction = raw_fraction[:6].ljust(6, "0")
    zone = match.group(8)
    try:
        parsed = datetime(year, month, day, hour, minute, second, int(fraction), tzinfo=UTC)
        if zone != "Z":
            zone_hours, zone_minutes = int(zone[1:3]), int(zone[4:6])
            if zone_hours > 99 or zone_minutes > 59:
                raise ValueError("GitHub response zone is invalid")
            offset = timedelta(hours=zone_hours, minutes=zone_minutes)
            parsed -= offset if zone[0] == "+" else -offset
        return parsed
    except (ValueError, OverflowError) as error:
        raise ValueError("GitHub response time is invalid") from error


def _url(value: Any) -> str:
    candidate = _string(value, 2048)
    if re.search(r"%(?![0-9A-Fa-f]{2})", candidate):
        return ""
    try:
        parsed = urlsplit(candidate)
        if parsed.scheme not in ("http", "https") or not parsed.hostname:
            return ""
    except ValueError:
        return ""
    path = quote(parsed.path, safe="/!$&'()*+,;=:@[]-._~%")
    fragment = quote(parsed.fragment, safe="!$&()*+,;=:@/?-._~%")
    result = urlunsplit((parsed.scheme, parsed.netloc, path, parsed.query, fragment))
    if not parsed.query and candidate.split("#", 1)[0].endswith("?"):
        position = result.find("#")
        result = result + "?" if position < 0 else result[:position] + "?" + result[position:]
    return result


def _truncate(value: Any, maximum: int) -> tuple[str, bool]:
    if value is None:
        return "", False
    if not isinstance(value, str):
        raise ValueError("GitHub text type differs")
    return value[:maximum], len(value) > maximum


def _is_go_mail_address(value: str) -> bool:
    if not value or len(value) > 320 or any(character in value for character in "<>\r\n()"):
        return False
    if value.startswith('"'):
        closing_quote = value.find('"', 1)
        if closing_quote < 2 or value[closing_quote + 1 : closing_quote + 2] != "@":
            return False
        local = value[: closing_quote + 1]
        domain = value[closing_quote + 2 :]
        if any(ord(character) < 32 or ord(character) == 127 for character in local[1:-1]):
            return False
    else:
        local, separator, domain = value.rpartition("@")
        if not separator or not local or any(not segment for segment in local.split(".")):
            return False
        atom_special = "!#$%&'*+-/=?^_`{|}~"
        if any(
            not (
                character.isalnum()
                or character in atom_special
                or character == "."
                or (ord(character) > 127 and not character.isspace())
            )
            for character in local
        ):
            return False
    if domain.startswith("[") and domain.endswith("]"):
        return bool(domain[1:-1]) and all(
            32 < ord(character) < 127 and character != "]" for character in domain[1:-1]
        )
    if not domain or any(not segment for segment in domain.split(".")):
        return False
    return all(
        character.isalnum() or character == "-" or (ord(character) > 127 and not character.isspace())
        for character in domain
        if character != "."
    )


def _validate_repository_payload(item: Any) -> None:
    if not isinstance(item, dict):
        raise ValueError("repository object differs")
    for name in ("id", "stargazers_count", "forks_count", "open_issues_count"):
        _integer(item.get(name))
    for name in ("private", "fork", "archived", "disabled"):
        _boolean(item.get(name))
    for name in ("name", "full_name", "visibility", "html_url", "description", "homepage", "language"):
        _string(item.get(name))
    for name in ("created_at", "updated_at", "pushed_at"):
        _time(item.get(name))
    topics = item.get("topics") or []
    if not isinstance(topics, list):
        raise ValueError("repository topics differ")
    for topic in topics:
        _string(topic)


def _page(response: _Response, current: int) -> int:
    for match in re.finditer(r'<([^>]*)>\s*;\s*rel="([^"]*)"', response.headers.get("Link", "")):
        if "next" not in match.group(2).lower().split():
            continue
        values = parse_qs(urlsplit(match.group(1)).query)
        try:
            number = int(values.get("page", [""])[0])
        except ValueError as error:
            raise ValueError("GitHub pagination link is invalid") from error
        if number <= current:
            raise ValueError("GitHub pagination link does not advance")
        return number
    return 0


def _window(start: datetime, end: datetime) -> bool:
    return (
        isinstance(start, datetime)
        and isinstance(end, datetime)
        and start.year != 1
        and end.year != 1
        and start < end
    )


def _provider_time(value: datetime) -> str:
    return value.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def _repository_input(
    client: Client, operation: str, value: Any, empty: Any
) -> tuple[str, str, QueryAttempt[Any] | None]:
    owner = value.owner.strip()
    repository = value.repository.strip()
    if not _LOGIN.fullmatch(owner):
        return (
            "",
            "",
            client._branch(
                operation, "defect", empty, FailureKind.VALIDATION, "GitHub repository owner is invalid"
            ),
        )
    if not _REPOSITORY.fullmatch(repository) or repository in (".", ".."):
        return (
            "",
            "",
            client._branch(
                operation, "defect", empty, FailureKind.VALIDATION, "GitHub repository name is invalid"
            ),
        )
    return owner, repository, None


def _page_input(
    client: Client, operation: str, value: Any, empty: Any, default: int
) -> tuple[int, int, QueryAttempt[Any] | None]:
    size, page = value.page_size or default, value.page or 1
    if not (1 <= size <= 100 and page >= 1):
        return (
            0,
            0,
            client._branch(
                operation,
                "defect",
                empty,
                FailureKind.VALIDATION,
                "page size must be 1 through 100 and page must be positive",
            ),
        )
    return size, page, None


class _Operation:
    definition_value: Any
    operation_id: str
    empty: Any
    success_branch: str

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated immutable operation definition."""
        return self.definition_value

    async def _credential(self, call: Call) -> tuple[Any, QueryAttempt[Any] | None]:
        return await self.client._credential(call, self.operation_id, self.empty)

    async def _fetch(
        self, call: Call, credential: Any, path: str, query: dict[str, str] | None = None
    ) -> tuple[_Response | None, QueryAttempt[Any] | None]:
        return await self.client._request(
            call,
            self.operation_id,
            credential,
            path,
            query or {},
            self.empty,
            "GitHub response exceeds the configured size limit",
        )

    def _invalid(self, response: _Response, message: str) -> QueryAttempt[Any]:
        return self.client._branch(
            self.operation_id,
            "invalidResponse",
            self.empty,
            FailureKind.PROTOCOL,
            message,
            response.receipt(),
        )

    def _success(
        self, value: Any, response: _Response, metadata: dict[str, str] | None = None
    ) -> QueryAttempt[Any]:
        return QueryAttempt.branch_result(self.success_branch, value, response.receipt(metadata))


class GetAuthenticatedProfileOperation(_Operation):
    """Read the account and verified primary email."""

    operation_id = "getAuthenticatedProfile"
    definition_value = definitions.GET_AUTHENTICATED_PROFILE_DEFINITION
    success_branch = "profileLoaded"
    empty = AuthenticatedProfile()

    async def invoke(
        self, call: Call, input: GetAuthenticatedProfileInput
    ) -> QueryAttempt[AuthenticatedProfile]:
        """Read both profile and email endpoints in one Query attempt."""
        credential, early = await self._credential(call)
        if early:
            return early
        profile_response, early = await self._fetch(call, credential, "/user")
        if early:
            return early
        assert profile_response is not None
        try:
            profile = _decode(profile_response.body, dict)
            if _integer(profile.get("id")) <= 0 or not _string(profile.get("login")):
                raise ValueError("invalid profile")
            value = AuthenticatedProfile(
                subject=str(profile["id"]),
                login=_string(profile["login"], 100),
                name=_string(profile.get("name"), 256),
                verified_email="",
                avatar_url=_url(profile.get("avatar_url")),
                profile_url=_url(profile.get("html_url")),
                company=_string(profile.get("company"), 256),
                blog_url=_url(profile.get("blog")),
                location=_string(profile.get("location"), 256),
                bio=_string(profile.get("bio"), 1024),
                twitter_username=_string(profile.get("twitter_username"), 100),
                public_repos=max(0, _integer(profile.get("public_repos"))),
                followers=max(0, _integer(profile.get("followers"))),
                following=max(0, _integer(profile.get("following"))),
                created_at=_time(profile.get("created_at")),
                updated_at=_time(profile.get("updated_at")),
            )
        except (ValueError, json.JSONDecodeError):
            return self._invalid(profile_response, "GitHub returned an invalid profile response")
        email_response, early = await self._fetch(call, credential, "/user/emails", {"per_page": "100"})
        if early:
            if early.kind == "branch" and early.receipt is not None and email_response is not None:
                early.receipt.metadata.update(
                    {
                        "profileRequestId": profile_response.request_id,
                        "emailRequestId": email_response.request_id,
                    }
                )
            return early
        assert email_response is not None
        metadata = {
            "profileRequestId": profile_response.request_id,
            "emailRequestId": email_response.request_id,
        }
        try:
            emails = _decode(email_response.body, list)
            verified = ""
            for candidate in emails:
                if not isinstance(candidate, dict):
                    raise ValueError("invalid email")
                email = _string(candidate.get("email")).lower()
                is_primary = _boolean(candidate.get("primary"))
                is_verified = _boolean(candidate.get("verified"))
                if is_primary and is_verified:
                    if _is_go_mail_address(email):
                        verified = email
                        break
        except (ValueError, json.JSONDecodeError):
            return self.client._branch(
                self.operation_id,
                "invalidResponse",
                self.empty,
                FailureKind.PROTOCOL,
                "GitHub returned an invalid email response",
                email_response.receipt(metadata),
            )
        if not verified:
            return self.client._branch(
                self.operation_id,
                "verifiedEmailRequired",
                self.empty,
                FailureKind.AUTHENTICATION,
                "GitHub primary verified email is required",
                email_response.receipt(metadata),
            )
        return self._success(replace(value, verified_email=verified), email_response, metadata)


class ListPublicRepositoriesOperation(_Operation):
    """List bounded public repositories for one login."""

    operation_id = "listPublicRepositories"
    definition_value = definitions.LIST_PUBLIC_REPOSITORIES_DEFINITION
    success_branch = "repositoriesLoaded"
    empty = PublicRepositories()

    async def invoke(
        self, call: Call, input: ListPublicRepositoriesInput
    ) -> QueryAttempt[PublicRepositories]:
        """Follow GitHub pages until the configured result limit is reached."""
        login = input.login.strip()
        if not _LOGIN.fullmatch(login):
            return self.client._branch(
                self.operation_id, "defect", self.empty, FailureKind.VALIDATION, "GitHub login is invalid"
            )
        limit = input.limit or self.client.config.default_repository_limit
        if limit < 1 or limit > self.client.config.max_repositories:
            return self.client._branch(
                self.operation_id,
                "defect",
                self.empty,
                FailureKind.VALIDATION,
                "repository limit is outside the configured range",
            )
        credential, early = await self._credential(call)
        if early:
            return early
        repositories: dict[int, Repository] = {}
        page = 1
        truncated = False
        while True:
            response, early = await self._fetch(
                call,
                credential,
                "/users/" + _path_escape(login) + "/repos",
                {
                    "type": "owner",
                    "sort": "pushed",
                    "direction": "desc",
                    "per_page": "100",
                    "page": str(page),
                },
            )
            if early:
                if early.kind == "branch" and early.receipt is not None and response is not None:
                    early.receipt.metadata["pagesRead"] = str(page)
                return early
            assert response is not None
            try:
                values = _decode(response.body, list)
                for item in values:
                    _validate_repository_payload(item)
                for item in values:
                    identifier = _integer(item.get("id"))
                    if (
                        identifier <= 0
                        or _boolean(item.get("private"))
                        or _string(item.get("visibility")) not in ("", "public")
                        or identifier in repositories
                    ):
                        continue
                    if len(repositories) == limit:
                        truncated = True
                        break
                    topics = item.get("topics") or []
                    if not isinstance(topics, list):
                        raise ValueError("repository topics differ")
                    repositories[identifier] = Repository(
                        id=identifier,
                        name=_string(item.get("name"), 256),
                        full_name=_string(item.get("full_name"), 512),
                        description=_string(item.get("description"), 1024),
                        profile_url=_url(item.get("html_url")),
                        homepage_url=_url(item.get("homepage")),
                        language=_string(item.get("language"), 128),
                        topics=tuple(
                            value for value in (_string(topic, 64) for topic in topics[:100]) if value
                        ),
                        fork=_boolean(item.get("fork")),
                        archived=_boolean(item.get("archived")),
                        disabled=_boolean(item.get("disabled")),
                        stargazers_count=max(0, _integer(item.get("stargazers_count"))),
                        forks_count=max(0, _integer(item.get("forks_count"))),
                        open_issues_count=max(0, _integer(item.get("open_issues_count"))),
                        created_at=_time(item.get("created_at")),
                        updated_at=_time(item.get("updated_at")),
                        pushed_at=_time(item.get("pushed_at")),
                    )
            except (ValueError, json.JSONDecodeError):
                return self.client._branch(
                    self.operation_id,
                    "invalidResponse",
                    self.empty,
                    FailureKind.PROTOCOL,
                    "GitHub returned an invalid repository response",
                    response.receipt({"pagesRead": str(page)}),
                )
            has_next = 'rel="next"' in response.headers.get("Link", "")
            if len(repositories) == limit:
                truncated = truncated or has_next
                break
            if not has_next or not values:
                break
            page += 1
        ordered = sorted(
            repositories.values(),
            key=lambda value: (
                value.pushed_at is None,
                -(value.pushed_at.timestamp() if value.pushed_at else 0),
                value.id,
            ),
        )
        return self._success(
            PublicRepositories(tuple(ordered), truncated), response, {"pagesRead": str(page)}
        )


class _RepositoryPageOperation(_Operation):
    default_page_size = 30

    def _validate(self, input: Any) -> tuple[str, str, int, int, QueryAttempt[Any] | None]:
        owner, repository, early = _repository_input(self.client, self.operation_id, input, self.empty)
        if early:
            return owner, repository, 0, 0, early
        size, page, early = _page_input(
            self.client, self.operation_id, input, self.empty, self.default_page_size
        )
        return owner, repository, size, page, early

    async def _repository_fetch(
        self, call: Call, credential: Any, owner: str, repository: str, suffix: str, query: dict[str, str]
    ) -> tuple[_Response | None, QueryAttempt[Any] | None]:
        return await self._fetch(
            call, credential, "/repos/" + _path_escape(owner) + "/" + _path_escape(repository) + suffix, query
        )


class ListMergedPullRequestsOperation(_RepositoryPageOperation):
    """Read one page of merged pull requests."""

    operation_id = "listMergedPullRequests"
    definition_value = definitions.LIST_MERGED_PULL_REQUESTS_DEFINITION
    success_branch = "listed"
    empty = MergedPullRequestPage()

    async def invoke(
        self, call: Call, input: ListMergedPullRequestsInput
    ) -> QueryAttempt[MergedPullRequestPage]:
        """Search merged pull requests in a bounded time window."""
        owner, repository, size, page, early = self._validate(input)
        if early:
            return early
        if not _window(input.merged_after, input.merged_before):
            return self.client._branch(
                self.operation_id,
                "defect",
                self.empty,
                FailureKind.VALIDATION,
                "merged pull request window must have a start before its end",
            )
        if page > 1000 or (page - 1) * size >= 1000:
            return self.client._branch(
                self.operation_id,
                "defect",
                self.empty,
                FailureKind.VALIDATION,
                "search page starts after GitHub's first 1000 results",
            )
        credential, early = await self._credential(call)
        if early:
            return early
        search = (
            f"repo:{owner}/{repository} is:pr is:merged "
            f"merged:{_provider_time(input.merged_after)}..{_provider_time(input.merged_before)}"
        )
        response, early = await self._fetch(
            call,
            credential,
            "/search/issues",
            {"q": search, "sort": "created", "order": "desc", "per_page": str(size), "page": str(page)},
        )
        if early:
            return early
        assert response is not None
        try:
            data = _decode(response.body, dict)
            if data.get("total_count") is None or not isinstance(data.get("items"), list):
                raise ValueError("invalid search")
            items = []
            for item in data["items"]:
                request = item.get("pull_request")
                if (
                    _integer(item.get("number")) <= 0
                    or not isinstance(request, dict)
                    or request.get("merged_at") is None
                ):
                    raise ValueError("invalid merged pull request")
                body, truncated = _truncate(
                    item.get("body"), self.client.config.max_pull_request_body_characters
                )
                labels = item.get("labels") or []
                author = item.get("user") or {}
                if not isinstance(labels, list) or not isinstance(author, dict):
                    raise ValueError("invalid labels or author")
                items.append(
                    MergedPullRequest(
                        number=_integer(item["number"]),
                        title=_string(item.get("title"), 1024),
                        body=body,
                        body_truncated=truncated,
                        url=_url(item.get("html_url")),
                        author_login=_string(author.get("login"), 100),
                        merged_at=cast(datetime, _time(request["merged_at"], True)),
                        labels=tuple(
                            value
                            for value in (_string(label.get("name"), 256) for label in labels[:100])
                            if value
                        ),
                    )
                )
            next_page = _page(response, page)
            return self._success(
                MergedPullRequestPage(
                    tuple(items),
                    max(0, _integer(data["total_count"])),
                    _boolean(data.get("incomplete_results")),
                    next_page,
                ),
                response,
            )
        except (ValueError, json.JSONDecodeError, AttributeError):
            return self._invalid(response, "GitHub returned an invalid search response")


class ListPullRequestFilesOperation(_RepositoryPageOperation):
    """Read one page of changed files for a pull request."""

    operation_id = "listPullRequestFiles"
    definition_value = definitions.LIST_PULL_REQUEST_FILES_DEFINITION
    success_branch = "listed"
    empty = PullRequestFilePage()
    default_page_size = 50

    async def invoke(self, call: Call, input: ListPullRequestFilesInput) -> QueryAttempt[PullRequestFilePage]:
        """Read changed file metadata with bounded patch text."""
        owner, repository, size, page, early = self._validate(input)
        if early:
            return early
        if input.number <= 0:
            return self.client._branch(
                self.operation_id,
                "defect",
                self.empty,
                FailureKind.VALIDATION,
                "pull request number must be positive",
            )
        credential, early = await self._credential(call)
        if early:
            return early
        response, early = await self._repository_fetch(
            call,
            credential,
            owner,
            repository,
            f"/pulls/{input.number}/files",
            {"per_page": str(size), "page": str(page)},
        )
        if early:
            return early
        assert response is not None
        try:
            data = _decode(response.body, list)
            files = []
            for item in data:
                filename = _raw_string(item.get("filename"))
                previous = _raw_string(item.get("previous_filename"))
                status = _string(item.get("status"), 32)
                if (
                    not filename
                    or len(filename.encode()) > 4096
                    or "\0" in filename
                    or (previous and (len(previous.encode()) > 4096 or "\0" in previous))
                    or not status
                ):
                    raise ValueError("invalid file")
                patch, truncated = _truncate(item.get("patch"), self.client.config.max_patch_characters)
                files.append(
                    PullRequestFile(
                        filename=filename,
                        previous_filename=previous,
                        status=status,
                        additions=max(0, _integer(item.get("additions"))),
                        deletions=max(0, _integer(item.get("deletions"))),
                        changes=max(0, _integer(item.get("changes"))),
                        patch=patch,
                        patch_truncated=truncated,
                    )
                )
            return self._success(PullRequestFilePage(tuple(files), _page(response, page)), response)
        except (ValueError, json.JSONDecodeError, AttributeError):
            return self._invalid(response, "GitHub returned an invalid changed-file response")


class ListReleasesOperation(_RepositoryPageOperation):
    """Read one page of repository releases."""

    operation_id = "listReleases"
    definition_value = definitions.LIST_RELEASES_DEFINITION
    success_branch = "listed"
    empty = ReleasePage()

    async def invoke(self, call: Call, input: ListReleasesInput) -> QueryAttempt[ReleasePage]:
        """Read releases with bounded body text."""
        owner, repository, size, page, early = self._validate(input)
        if early:
            return early
        maximum = input.max_body_characters or 4000
        if not 1 <= maximum <= 16384:
            return self.client._branch(
                self.operation_id,
                "defect",
                self.empty,
                FailureKind.VALIDATION,
                "maxBodyCharacters must be between 1 and 16384",
            )
        credential, early = await self._credential(call)
        if early:
            return early
        response, early = await self._repository_fetch(
            call, credential, owner, repository, "/releases", {"per_page": str(size), "page": str(page)}
        )
        if early:
            return early
        assert response is not None
        try:
            data = _decode(response.body, list)
            if len(data) > size:
                raise ValueError("release page is too large")
            releases = []
            for item in data:
                created = _time(item.get("created_at"), True)
                published = _time(item.get("published_at"))
                draft = _boolean(item.get("draft"))
                tag = _raw_string(item.get("tag_name"))
                if (
                    _integer(item.get("id")) <= 0
                    or not tag
                    or len(tag.encode()) > 255
                    or any(
                        character.isspace() or unicodedata.category(character) == "Cc"
                        for character in tag
                    )
                    or (not draft and published is None)
                ):
                    raise ValueError("invalid release")
                body, truncated = _truncate(item.get("body"), maximum)
                releases.append(
                    Release(
                        id=_integer(item["id"]),
                        tag_name=tag,
                        name=_string(item.get("name"), 1024),
                        url=_url(item.get("html_url")),
                        body=body,
                        body_truncated=truncated,
                        is_draft=draft,
                        is_prerelease=_boolean(item.get("prerelease")),
                        created_at=cast(datetime, created),
                        published_at=published,
                    )
                )
            return self._success(ReleasePage(tuple(releases), _page(response, page)), response)
        except (ValueError, json.JSONDecodeError, AttributeError):
            return self._invalid(response, "GitHub returned an invalid release page")


class ListCommitsOperation(_RepositoryPageOperation):
    """Read one page of repository commits."""

    operation_id = "listCommits"
    definition_value = definitions.LIST_COMMITS_DEFINITION
    success_branch = "listed"
    empty = CommitPage()

    async def invoke(self, call: Call, input: ListCommitsInput) -> QueryAttempt[CommitPage]:
        """Read commits in a bounded window and classify empty repositories."""
        owner, repository, size, page, early = self._validate(input)
        if early:
            return early
        if not _window(input.since, input.until):
            return self.client._branch(
                self.operation_id,
                "defect",
                self.empty,
                FailureKind.VALIDATION,
                "commit window must have a start before its end",
            )
        for field, value, limit, spaces in (("path", input.path, 4096, True), ("ref", input.ref, 255, False)):
            if len(value.encode()) > limit or any(
                unicodedata.category(character) == "Cc" or (not spaces and character.isspace())
                for character in value
            ):
                return self.client._branch(
                    self.operation_id,
                    "defect",
                    self.empty,
                    FailureKind.VALIDATION,
                    f"commit {field} is invalid",
                )
        credential, early = await self._credential(call)
        if early:
            return early
        query = {
            "since": _provider_time(input.since),
            "until": _provider_time(input.until),
            "per_page": str(size),
            "page": str(page),
        }
        if input.path:
            query["path"] = input.path
        if input.ref:
            query["sha"] = input.ref
        response, early = await self._repository_fetch(call, credential, owner, repository, "/commits", query)
        if early:
            return early
        assert response is not None
        if response.status == 409:
            return self._success(CommitPage(), response, {"repositoryEmpty": "true"})
        try:
            data = _decode(response.body, list)
            commits = []
            for item in data:
                sha = _raw_string(item.get("sha"))
                commit = item.get("commit") or {}
                author = commit.get("author") or {}
                committer = commit.get("committer") or {}
                user = item.get("author") or {}
                if (
                    not re.fullmatch(r"(?:[0-9a-f]{40}|[0-9a-f]{64})", sha)
                    or not author.get("date")
                    or not committer.get("date")
                ):
                    raise ValueError("invalid commit")
                message, truncated = _truncate(
                    commit.get("message"), self.client.config.max_commit_message_characters
                )
                commits.append(
                    CommitSummary(
                        sha=sha,
                        message=message,
                        message_truncated=truncated,
                        author_login=_string(user.get("login"), 100),
                        author_name=_string(author.get("name"), 256),
                        authored_at=cast(datetime, _time(author["date"], True)),
                        committed_at=cast(datetime, _time(committer["date"], True)),
                        url=_url(item.get("html_url")),
                    )
                )
            return self._success(CommitPage(tuple(commits), _page(response, page)), response)
        except (ValueError, json.JSONDecodeError, AttributeError):
            return self._invalid(response, "GitHub returned an invalid commit response")
