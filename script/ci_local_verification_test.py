# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Test that only an admin or maintainer's Mac Studio verification of a single pull request is trusted."""

import os
import subprocess
import sys
import tempfile
import unittest
import urllib.error
from collections.abc import Mapping
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from script import ci_local_verification  # noqa: E402
from script.ci_local_verification import LocalVerificationDecider  # noqa: E402

HEAD_SHA = "a" * 40
QUEUE_BASE_SHA = "b" * 40
MERGE_GROUP_HEAD_REF = f"refs/heads/gh-readonly-queue/main/pr-42-{QUEUE_BASE_SHA}"


def verification_status(state: str, login: str = "owner", creator_type: str = "User",
                        context: str = "mac-studio-verified") -> dict[str, Any]:
    return {"context": context, "state": state, "creator": {"login": login, "type": creator_type}}


class FakeRepositoryReader:
    """Serves fixed statuses, roles, pull request heads, and compare results, and records the calls."""

    def __init__(self, statuses: Mapping[str, list[dict[str, Any]]] | None = None, roles: Mapping[str, str] | None = None,
                 pull_request_heads: Mapping[int, str] | None = None, compare_status: str = "identical") -> None:
        self.statuses = statuses or {}
        self.roles = roles or {}
        self.pull_request_heads = pull_request_heads or {}
        self.compare_status = compare_status
        self.calls: list[tuple[str, Any]] = []

    def list_commit_statuses(self, commit_sha: str) -> list[Mapping[str, Any]]:
        self.calls.append(("statuses", commit_sha))
        return self.statuses.get(commit_sha, [])

    def get_collaborator_role_name(self, login: str) -> str:
        self.calls.append(("role", login))
        return self.roles.get(login, "")

    def get_pull_request_head_sha(self, pull_request_number: int) -> str:
        self.calls.append(("pull_request", pull_request_number))
        return self.pull_request_heads[pull_request_number]

    def compare_commits_status(self, base: str, head: str) -> str:
        self.calls.append(("compare", (base, head)))
        return self.compare_status


class DecideCommitTest(unittest.TestCase):
    def test_success_from_an_admin_or_maintainer_is_trusted(self) -> None:
        for role_name in ("admin", "maintain"):
            with self.subTest(role=role_name):
                reader = FakeRepositoryReader({HEAD_SHA: [verification_status("success")]}, {"owner": role_name})
                decision = LocalVerificationDecider(reader).decide_commit(HEAD_SHA)
                self.assertTrue(decision.is_trusted, decision.reason)

    def test_success_from_any_other_role_or_a_bot_is_untrusted(self) -> None:
        for role_name in ("write", "triage", "read", "none", "", "custom-maintainer"):
            with self.subTest(role=role_name):
                reader = FakeRepositoryReader({HEAD_SHA: [verification_status("success")]}, {"owner": role_name})
                self.assertFalse(LocalVerificationDecider(reader).decide_commit(HEAD_SHA).is_trusted)
        bot = FakeRepositoryReader({HEAD_SHA: [verification_status("success", "github-actions[bot]", "Bot")]},
                                   {"github-actions[bot]": "admin"})
        self.assertFalse(LocalVerificationDecider(bot).decide_commit(HEAD_SHA).is_trusted)
        self.assertNotIn(("role", "github-actions[bot]"), bot.calls)

    def test_only_the_newest_verification_status_counts(self) -> None:
        newest_failure = [verification_status("failure"), verification_status("success")]
        reader = FakeRepositoryReader({HEAD_SHA: newest_failure}, {"owner": "admin"})
        self.assertFalse(LocalVerificationDecider(reader).decide_commit(HEAD_SHA).is_trusted)
        # A later success from a writer replaces an earlier admin success.
        newest_writer = [verification_status("success", "writer"), verification_status("success")]
        reader = FakeRepositoryReader({HEAD_SHA: newest_writer}, {"owner": "admin", "writer": "write"})
        self.assertFalse(LocalVerificationDecider(reader).decide_commit(HEAD_SHA).is_trusted)
        other_context_first = [verification_status("failure", context="build"), verification_status("success")]
        reader = FakeRepositoryReader({HEAD_SHA: other_context_first}, {"owner": "admin"})
        self.assertTrue(LocalVerificationDecider(reader).decide_commit(HEAD_SHA).is_trusted)

    def test_missing_or_pending_status_is_untrusted(self) -> None:
        for statuses in ([], [verification_status("success", context="mac-studio")], [verification_status("pending")],
                         [verification_status("error")]):
            with self.subTest(statuses=statuses):
                reader = FakeRepositoryReader({HEAD_SHA: statuses}, {"owner": "admin"})
                self.assertFalse(LocalVerificationDecider(reader).decide_commit(HEAD_SHA).is_trusted)


class DecideEventTest(unittest.TestCase):
    def test_pull_request_checks_its_head_sha(self) -> None:
        reader = FakeRepositoryReader({HEAD_SHA: [verification_status("success")]}, {"owner": "maintain"})
        decision = LocalVerificationDecider(reader).decide_event({"EVENT_NAME": "pull_request", "PULL_REQUEST_HEAD_SHA": HEAD_SHA})
        self.assertTrue(decision.is_trusted, decision.reason)
        self.assertEqual(("statuses", HEAD_SHA), reader.calls[0])

    def test_single_pull_request_merge_group_checks_that_pull_requests_head(self) -> None:
        for compare_status in ("identical", "ahead"):
            with self.subTest(compare_status=compare_status):
                reader = FakeRepositoryReader({HEAD_SHA: [verification_status("success")]}, {"owner": "admin"}, {42: HEAD_SHA},
                                              compare_status)
                decision = LocalVerificationDecider(reader).decide_event(
                    {"EVENT_NAME": "merge_group", "MERGE_GROUP_HEAD_REF": MERGE_GROUP_HEAD_REF, "MERGE_GROUP_BASE_SHA": QUEUE_BASE_SHA})
                self.assertTrue(decision.is_trusted, decision.reason)
                self.assertIn("pull request #42", decision.reason)
                self.assertEqual([("compare", (QUEUE_BASE_SHA, "main")), ("pull_request", 42), ("statuses", HEAD_SHA),
                                  ("role", "owner")], reader.calls)

    def test_merge_group_with_an_earlier_queue_entry_is_untrusted(self) -> None:
        for compare_status in ("behind", "diverged", ""):
            with self.subTest(compare_status=compare_status):
                reader = FakeRepositoryReader({HEAD_SHA: [verification_status("success")]}, {"owner": "admin"}, {42: HEAD_SHA},
                                              compare_status)
                decision = LocalVerificationDecider(reader).decide_event(
                    {"EVENT_NAME": "merge_group", "MERGE_GROUP_HEAD_REF": MERGE_GROUP_HEAD_REF, "MERGE_GROUP_BASE_SHA": QUEUE_BASE_SHA})
                self.assertFalse(decision.is_trusted)
                self.assertNotIn(("pull_request", 42), reader.calls)

    def test_merge_group_without_a_pull_request_ref_is_untrusted(self) -> None:
        for head_ref in ("", "refs/heads/main", "refs/heads/gh-readonly-queue/main/pr-0-" + QUEUE_BASE_SHA,
                         "refs/heads/gh-readonly-queue/main/pr-42-short"):
            with self.subTest(head_ref=head_ref):
                reader = FakeRepositoryReader()
                decision = LocalVerificationDecider(reader).decide_event(
                    {"EVENT_NAME": "merge_group", "MERGE_GROUP_HEAD_REF": head_ref, "MERGE_GROUP_BASE_SHA": QUEUE_BASE_SHA})
                self.assertFalse(decision.is_trusted)
                self.assertEqual([], reader.calls)

    def test_other_events_never_call_the_api(self) -> None:
        for event_name in ("push", "schedule", "workflow_dispatch", ""):
            with self.subTest(event=event_name):
                decision = ci_local_verification.decide_from_environment({"EVENT_NAME": event_name, "PULL_REQUEST_HEAD_SHA": HEAD_SHA})
                self.assertFalse(decision.is_trusted)


class APIErrorTest(unittest.TestCase):
    def test_api_errors_are_untrusted_instead_of_failing(self) -> None:
        def raise_http_error(*_arguments: object, **_keywords: object) -> None:
            raise urllib.error.HTTPError("https://api.github.invalid", 403, "Forbidden", {}, None)

        original = ci_local_verification.urllib.request.urlopen
        ci_local_verification.urllib.request.urlopen = raise_http_error
        try:
            decision = ci_local_verification.decide_from_environment({
                "EVENT_NAME": "pull_request", "PULL_REQUEST_HEAD_SHA": HEAD_SHA, "GITHUB_API_URL": "https://api.github.invalid",
                "GITHUB_REPOSITORY": "owner/repository", "GITHUB_TOKEN": "token"})
        finally:
            ci_local_verification.urllib.request.urlopen = original
        self.assertFalse(decision.is_trusted)
        self.assertIn("HTTPError", decision.reason)

    def test_missing_token_is_untrusted(self) -> None:
        decision = ci_local_verification.decide_from_environment({"EVENT_NAME": "pull_request", "PULL_REQUEST_HEAD_SHA": HEAD_SHA})
        self.assertFalse(decision.is_trusted)

    def test_main_writes_false_and_exits_zero_for_untrusted_events(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output_path = Path(directory) / "output"
            environment = {"PATH": os.environ.get("PATH", ""), "EVENT_NAME": "push", "GITHUB_OUTPUT": str(output_path)}
            completed = subprocess.run([sys.executable, ci_local_verification.__file__], env=environment, capture_output=True,
                                       text=True, check=False)
            self.assertEqual(0, completed.returncode, completed.stderr)
            self.assertEqual("trusted=false\n", output_path.read_text(encoding="utf-8"))
            self.assertIn("push runs never skip jobs", completed.stdout)


if __name__ == "__main__":
    unittest.main()
