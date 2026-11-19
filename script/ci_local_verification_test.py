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
                 pull_request_heads: Mapping[int, str] | None = None, compare_status: str = "identical",
                 commit_parents: Mapping[str, list[str]] | None = None,
                 commit_pull_requests: Mapping[str, list[dict[str, Any]]] | None = None) -> None:
        self.statuses = statuses or {}
        self.roles = roles or {}
        self.pull_request_heads = pull_request_heads or {}
        self.compare_status = compare_status
        self.commit_parents = commit_parents or {}
        self.commit_pull_requests = commit_pull_requests or {}
        self.calls: list[tuple[str, Any]] = []

    def get_commit_parent_shas(self, commit_sha: str) -> list[str]:
        self.calls.append(("parents", commit_sha))
        return self.commit_parents.get(commit_sha, [])

    def list_commit_pull_requests(self, commit_sha: str) -> list[Mapping[str, Any]]:
        self.calls.append(("commit_pull_requests", commit_sha))
        return self.commit_pull_requests.get(commit_sha, [])

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
        for event_name in ("schedule", "workflow_dispatch", ""):
            with self.subTest(event=event_name):
                decision = ci_local_verification.decide_from_environment({"EVENT_NAME": event_name, "PULL_REQUEST_HEAD_SHA": HEAD_SHA})
                self.assertFalse(decision.is_trusted)


SQUASH_SHA = "c" * 40
BEFORE_SHA = "d" * 40
OLDER_HEAD_SHA = "e" * 40


def merged_pull_request(number: int = 42, head_sha: str = HEAD_SHA, merge_commit_sha: str = SQUASH_SHA,
                        base_ref: str = "main", merged_at: str | None = "2026-11-19T06:26:23Z") -> dict[str, Any]:
    return {"number": number, "merged_at": merged_at, "merge_commit_sha": merge_commit_sha,
            "head": {"sha": head_sha}, "base": {"ref": base_ref}}


class DecidePushTest(unittest.TestCase):
    push_environment = {"EVENT_NAME": "push", "PUSH_REF": "refs/heads/main", "PUSH_SHA": SQUASH_SHA, "PUSH_BEFORE_SHA": BEFORE_SHA}

    def reader(self, pull_requests: list[dict[str, Any]], statuses: Mapping[str, list[dict[str, Any]]] | None = None,
               roles: Mapping[str, str] | None = None, parents: list[str] | None = None) -> FakeRepositoryReader:
        return FakeRepositoryReader(statuses if statuses is not None else {HEAD_SHA: [verification_status("success")]},
                                    roles if roles is not None else {"owner": "admin"},
                                    commit_parents={SQUASH_SHA: parents if parents is not None else [BEFORE_SHA]},
                                    commit_pull_requests={SQUASH_SHA: pull_requests})

    def decide(self, reader: FakeRepositoryReader, **overrides: str) -> ci_local_verification.LocalVerificationDecision:
        return LocalVerificationDecider(reader).decide_event(self.push_environment | overrides)

    def test_squash_commit_of_a_verified_pull_request_is_trusted_by_its_head_sha(self) -> None:
        for role_name in ("admin", "maintain"):
            with self.subTest(role=role_name):
                reader = self.reader([merged_pull_request()], roles={"owner": role_name})
                decision = self.decide(reader)
                self.assertTrue(decision.is_trusted, decision.reason)
                self.assertIn("merged pull request #42", decision.reason)
                self.assertIn(("statuses", HEAD_SHA), reader.calls)
                self.assertNotIn(("statuses", SQUASH_SHA), reader.calls)

    def test_push_without_a_pull_request_is_untrusted(self) -> None:
        reader = self.reader([])
        decision = self.decide(reader)
        self.assertFalse(decision.is_trusted)
        self.assertIn("0 pull requests", decision.reason)

    def test_push_with_several_pull_requests_is_untrusted(self) -> None:
        decision = self.decide(self.reader([merged_pull_request(), merged_pull_request(number=43)]))
        self.assertFalse(decision.is_trusted)

    def test_admin_merge_without_a_status_is_untrusted(self) -> None:
        decision = self.decide(self.reader([merged_pull_request()], statuses={}))
        self.assertFalse(decision.is_trusted)
        self.assertIn("has no mac-studio-verified status", decision.reason)

    def test_status_by_a_writer_is_untrusted(self) -> None:
        decision = self.decide(self.reader([merged_pull_request()], roles={"owner": "write"}))
        self.assertFalse(decision.is_trusted)

    def test_status_on_an_older_head_than_the_merged_one_is_untrusted(self) -> None:
        reader = self.reader([merged_pull_request()], statuses={OLDER_HEAD_SHA: [verification_status("success")]})
        decision = self.decide(reader)
        self.assertFalse(decision.is_trusted)
        self.assertNotIn(("statuses", OLDER_HEAD_SHA), reader.calls)

    def test_pull_request_not_merged_as_the_pushed_commit_is_untrusted(self) -> None:
        for pull_request in (merged_pull_request(merged_at=None), merged_pull_request(merge_commit_sha="f" * 40),
                             merged_pull_request(base_ref="release")):
            with self.subTest(pull_request=pull_request):
                self.assertFalse(self.decide(self.reader([pull_request])).is_trusted)

    def test_missing_or_malformed_head_sha_or_number_never_reaches_the_statuses_api(self) -> None:
        malformed = []
        for head_sha in (None, "", "None", "main", HEAD_SHA[:39], HEAD_SHA.upper()):
            pull_request = merged_pull_request()
            pull_request["head"] = {"sha": head_sha}
            malformed.append(pull_request)
        missing_head = merged_pull_request()
        del missing_head["head"]
        malformed.append(missing_head)
        for number in (None, 0, -1, "42", True):
            pull_request = merged_pull_request()
            pull_request["number"] = number
            malformed.append(pull_request)
        missing_number = merged_pull_request()
        del missing_number["number"]
        malformed.append(missing_number)
        for pull_request in malformed:
            with self.subTest(pull_request=pull_request):
                # A ref named like the bad value carries a trusted status; it must stay unread.
                statuses = {str(value): [verification_status("success")]
                            for value in ((pull_request.get("head") or {}).get("sha"), "None", "main")}
                reader = self.reader([pull_request], statuses=statuses)
                self.assertFalse(self.decide(reader).is_trusted)
                self.assertFalse([call for call in reader.calls if call[0] == "statuses"])

    def test_push_that_adds_several_commits_is_untrusted(self) -> None:
        for parents in (["f" * 40], [BEFORE_SHA, "f" * 40], []):
            with self.subTest(parents=parents):
                reader = self.reader([merged_pull_request()], parents=parents)
                self.assertFalse(self.decide(reader).is_trusted)
                self.assertNotIn(("commit_pull_requests", SQUASH_SHA), reader.calls)

    def test_push_to_another_ref_or_without_shas_never_calls_the_api(self) -> None:
        for overrides in ({"PUSH_REF": "refs/heads/feature"}, {"PUSH_REF": ""}, {"PUSH_SHA": ""}, {"PUSH_BEFORE_SHA": ""}):
            with self.subTest(overrides=overrides):
                reader = self.reader([merged_pull_request()])
                self.assertFalse(self.decide(reader, **overrides).is_trusted)
                self.assertEqual([], reader.calls)


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
            environment = {"PATH": os.environ.get("PATH", ""), "EVENT_NAME": "schedule", "GITHUB_OUTPUT": str(output_path)}
            completed = subprocess.run([sys.executable, ci_local_verification.__file__], env=environment, capture_output=True,
                                       text=True, check=False)
            self.assertEqual(0, completed.returncode, completed.stderr)
            self.assertEqual("trusted=false\n", output_path.read_text(encoding="utf-8"))
            self.assertIn("schedule runs never skip jobs", completed.stdout)


if __name__ == "__main__":
    unittest.main()
