# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decide whether a Mac Studio verified pull request lets CI skip its heavy connector jobs.

The owner's Mac Studio tooling (`remote-test.py attest`) posts the commit status
`mac-studio-verified` = `success` on a pull request head after a passing full local run. A
pull request or merge group is trusted only when the newest such status on every included pull
request head is `success` and its creator holds the `admin` or `maintain` repository role. A push
to main is trusted only when it adds one squash commit of exactly one merged pull request whose
head passes the same check. Scheduled and manual runs, a missing status, an API error, or a merge
group or push that includes more than one pull request are untrusted, so CI runs every job. The script always exits 0 and writes
`trusted=true|false` to GITHUB_OUTPUT.
"""

import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any, Protocol

STATUS_CONTEXT = "mac-studio-verified"
TRUSTED_ROLE_NAMES = frozenset({"admin", "maintain"})
# GitHub caps statuses at 1000 per commit and context, so ten pages hold every one.
MAXIMUM_STATUS_PAGES = 10
STATUS_PAGE_SIZE = 100
API_TIMEOUT_SECONDS = 20
MERGE_GROUP_HEAD_REF_PATTERN = re.compile(
    r"^(?:refs/heads/)?gh-readonly-queue/(?P<base_branch>.+)/pr-(?P<pull_request_number>[1-9][0-9]*)-[0-9a-f]{40}$")
# Scheduled and manual runs are never trusted, so the daily run stays complete.
LOCALLY_VERIFIABLE_EVENTS = frozenset({"pull_request", "merge_group", "push"})
TRUSTED_PUSH_REF = "refs/heads/main"
# Compare statuses meaning the merge group base is already on its base branch.
BASE_ON_BRANCH_COMPARE_STATUSES = frozenset({"identical", "ahead"})


@dataclass(frozen=True)
class LocalVerificationDecision:
    """Whether CI may skip heavy jobs, and the reason printed in the job log."""

    is_trusted: bool
    reason: str


class GitHubRepositoryReader(Protocol):
    """Read-only GitHub REST calls the decision needs, scoped to one repository."""

    def list_commit_statuses(self, commit_sha: str) -> list[Mapping[str, Any]]:
        """Returns the commit's statuses, newest first."""

    def get_collaborator_role_name(self, login: str) -> str:
        """Returns the user's repository role name, such as admin, maintain, or write."""

    def get_pull_request_head_sha(self, pull_request_number: int) -> str:
        """Returns the pull request's current head commit SHA."""

    def compare_commits_status(self, base: str, head: str) -> str:
        """Returns the compare status of head relative to base: identical, ahead, behind, or diverged."""

    def get_commit_parent_shas(self, commit_sha: str) -> list[str]:
        """Returns the commit's parent SHAs in order."""

    def list_commit_pull_requests(self, commit_sha: str) -> list[Mapping[str, Any]]:
        """Returns the pull requests associated with the commit, including merged ones."""


class GitHubRESTRepositoryReader:
    """Calls the GitHub REST API with the job's token."""

    def __init__(self, api_url: str, repository: str, token: str) -> None:
        if not api_url or not repository or not token:
            raise ValueError("GITHUB_API_URL, GITHUB_REPOSITORY, and GITHUB_TOKEN are required")
        self._repository_url = f"{api_url.rstrip('/')}/repos/{repository}"
        self._token = token

    def list_commit_statuses(self, commit_sha: str) -> list[Mapping[str, Any]]:
        statuses: list[Mapping[str, Any]] = []
        for page in range(1, MAXIMUM_STATUS_PAGES + 1):
            page_statuses = self._get_json(f"/commits/{commit_sha}/statuses?per_page={STATUS_PAGE_SIZE}&page={page}")
            if not isinstance(page_statuses, list):
                raise ValueError("commit statuses response is not a list")
            statuses.extend(page_statuses)
            if len(page_statuses) < STATUS_PAGE_SIZE:
                break
        return statuses

    def get_collaborator_role_name(self, login: str) -> str:
        permission = self._get_json(f"/collaborators/{urllib.parse.quote(login, safe='')}/permission")
        return str(permission.get("role_name") or "")

    def get_pull_request_head_sha(self, pull_request_number: int) -> str:
        pull_request = self._get_json(f"/pulls/{pull_request_number}")
        return str(pull_request["head"]["sha"])

    def compare_commits_status(self, base: str, head: str) -> str:
        comparison = self._get_json(f"/compare/{base}...{urllib.parse.quote(head, safe='')}?per_page=1")
        return str(comparison.get("status") or "")

    def get_commit_parent_shas(self, commit_sha: str) -> list[str]:
        commit = self._get_json(f"/commits/{commit_sha}")
        return [str(parent["sha"]) for parent in commit["parents"]]

    def list_commit_pull_requests(self, commit_sha: str) -> list[Mapping[str, Any]]:
        pull_requests = self._get_json(f"/commits/{commit_sha}/pulls?per_page=100")
        if not isinstance(pull_requests, list):
            raise ValueError("commit pull requests response is not a list")
        return pull_requests

    def _get_json(self, path: str) -> Any:
        request = urllib.request.Request(self._repository_url + path, headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {self._token}",
            "X-GitHub-Api-Version": "2022-11-28",
        })
        with urllib.request.urlopen(request, timeout=API_TIMEOUT_SECONDS) as response:  # noqa: S310 - fixed API URL.
            return json.load(response)


class LocalVerificationDecider:
    """Decides trust for one workflow event from Mac Studio verification statuses."""

    def __init__(self, repository_reader: GitHubRepositoryReader) -> None:
        if repository_reader is None:
            raise ValueError("repository_reader is required")
        self._repository_reader = repository_reader

    def decide_event(self, environment: Mapping[str, str]) -> LocalVerificationDecision:
        """Returns the decision for the event that GitHub Actions describes in the environment."""
        event_name = environment.get("EVENT_NAME", "")
        if event_name == "pull_request":
            head_sha = environment.get("PULL_REQUEST_HEAD_SHA", "")
            if not head_sha:
                return LocalVerificationDecision(False, "the pull_request event has no head SHA")
            return self.decide_commit(head_sha)
        if event_name == "merge_group":
            return self.decide_merge_group(environment.get("MERGE_GROUP_HEAD_REF", ""), environment.get("MERGE_GROUP_BASE_SHA", ""))
        if event_name == "push":
            return self.decide_push(environment.get("PUSH_REF", ""), environment.get("PUSH_SHA", ""),
                                    environment.get("PUSH_BEFORE_SHA", ""))
        return LocalVerificationDecision(False, f"{event_name or 'an unknown event'} runs never skip jobs")

    def decide_push(self, ref: str, pushed_sha: str, before_sha: str) -> LocalVerificationDecision:
        """Trusts a push to main that adds one squash commit of one merged, trusted pull request."""
        if ref != TRUSTED_PUSH_REF:
            return LocalVerificationDecision(False, f"pushes to {ref or 'an unknown ref'} never skip jobs")
        if not pushed_sha or not before_sha:
            return LocalVerificationDecision(False, "the push event has no head or before SHA")
        # A push adding several commits lands several pull requests at once; never trust it.
        parent_shas = self._repository_reader.get_commit_parent_shas(pushed_sha)
        if parent_shas != [before_sha]:
            return LocalVerificationDecision(
                False, f"push {pushed_sha} does not add exactly one commit on top of {before_sha} (parents {parent_shas})")
        pull_requests = self._repository_reader.list_commit_pull_requests(pushed_sha)
        if len(pull_requests) != 1:
            return LocalVerificationDecision(False, f"push {pushed_sha} belongs to {len(pull_requests)} pull requests, not exactly one")
        pull_request = pull_requests[0]
        pull_request_number = pull_request.get("number")
        if (not pull_request.get("merged_at") or pull_request.get("merge_commit_sha") != pushed_sha
                or (pull_request.get("base") or {}).get("ref") != TRUSTED_PUSH_REF.removeprefix("refs/heads/")):
            return LocalVerificationDecision(
                False, f"pull request #{pull_request_number} was not merged into main as {pushed_sha}")
        # The squash commit never carries the status; the merged pull request head does.
        head_sha = str(pull_request["head"]["sha"])
        decision = self.decide_commit(head_sha)
        return LocalVerificationDecision(decision.is_trusted, f"merged pull request #{pull_request_number}: {decision.reason}")

    def decide_merge_group(self, head_ref: str, base_sha: str) -> LocalVerificationDecision:
        """Trusts a merge group only when it holds exactly one trusted pull request."""
        match = MERGE_GROUP_HEAD_REF_PATTERN.match(head_ref)
        if match is None:
            return LocalVerificationDecision(False, f"merge group head ref {head_ref!r} does not name a pull request")
        if not base_sha:
            return LocalVerificationDecision(False, "the merge_group event has no base SHA")
        base_branch = match.group("base_branch")
        pull_request_number = int(match.group("pull_request_number"))
        # A base still ahead of its branch is an earlier queue entry, so the group holds several pull requests.
        compare_status = self._repository_reader.compare_commits_status(base_sha, base_branch)
        if compare_status not in BASE_ON_BRANCH_COMPARE_STATUSES:
            return LocalVerificationDecision(
                False, f"merge group base {base_sha} is not on {base_branch} ({compare_status}); "
                       "groups with several pull requests are never trusted")
        head_sha = self._repository_reader.get_pull_request_head_sha(pull_request_number)
        decision = self.decide_commit(head_sha)
        return LocalVerificationDecision(decision.is_trusted, f"pull request #{pull_request_number}: {decision.reason}")

    def decide_commit(self, commit_sha: str) -> LocalVerificationDecision:
        """Trusts a commit whose newest verification status succeeded and was posted by an admin or maintainer."""
        latest_status = next((status for status in self._repository_reader.list_commit_statuses(commit_sha)
                              if status.get("context") == STATUS_CONTEXT), None)
        if latest_status is None:
            return LocalVerificationDecision(False, f"commit {commit_sha} has no {STATUS_CONTEXT} status")
        state = latest_status.get("state")
        if state != "success":
            return LocalVerificationDecision(False, f"the newest {STATUS_CONTEXT} status on {commit_sha} is {state}")
        creator = latest_status.get("creator") or {}
        login = str(creator.get("login") or "")
        if not login or creator.get("type") != "User":
            return LocalVerificationDecision(False, f"the newest {STATUS_CONTEXT} status on {commit_sha} has no user creator")
        role_name = self._repository_reader.get_collaborator_role_name(login)
        if role_name not in TRUSTED_ROLE_NAMES:
            return LocalVerificationDecision(
                False, f"{login} posted {STATUS_CONTEXT} on {commit_sha} with role {role_name or 'none'}, not admin or maintain")
        return LocalVerificationDecision(True, f"{login} ({role_name}) posted {STATUS_CONTEXT}=success on {commit_sha}")


def decide_from_environment(environment: Mapping[str, str]) -> LocalVerificationDecision:
    """Returns the decision, treating every API or response error as untrusted."""
    event_name = environment.get("EVENT_NAME", "")
    if event_name not in LOCALLY_VERIFIABLE_EVENTS:
        return LocalVerificationDecision(False, f"{event_name or 'an unknown event'} runs never skip jobs")
    try:
        reader = GitHubRESTRepositoryReader(environment.get("GITHUB_API_URL", ""), environment.get("GITHUB_REPOSITORY", ""),
                                            environment.get("GITHUB_TOKEN", ""))
        return LocalVerificationDecider(reader).decide_event(environment)
    except (urllib.error.URLError, OSError, ValueError, KeyError, TypeError, AttributeError) as error:
        return LocalVerificationDecision(False, f"could not read the verification status: {type(error).__name__}: {error}")


def main() -> int:
    decision = decide_from_environment(os.environ)
    trusted = "true" if decision.is_trusted else "false"
    print(f"trusted={trusted}: {decision.reason}")
    if decision.is_trusted:
        print("::notice title=Mac Studio verified::Heavy connector jobs are skipped; " + decision.reason)
    output_path = os.environ.get("GITHUB_OUTPUT", "")
    if output_path:
        with open(output_path, "a", encoding="utf-8") as output:
            output.write(f"trusted={trusted}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
