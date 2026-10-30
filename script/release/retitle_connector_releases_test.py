# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import contextlib
import io
import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent))

import retitle_connector_releases as retitle  # noqa: E402


class RetitleConnectorReleasesTest(unittest.TestCase):
    def test_parses_legacy_root_tag_as_go(self) -> None:
        self.assertEqual(
            retitle.parse_connector_release_tag("connectors/google/gmail/v0.3.0"),
            retitle.ConnectorReleaseTag("connectors/google/gmail/v0.3.0", "connectors/google/gmail", "go", "v0.3.0"),
        )

    def test_parses_language_tags_of_nested_directories(self) -> None:
        for language in ("go", "python", "typescript"):
            tag = f"connectors/google/gmail/{language}/v0.4.0-rc.2"
            self.assertEqual(
                retitle.parse_connector_release_tag(tag),
                retitle.ConnectorReleaseTag(tag, "connectors/google/gmail", language, "v0.4.0-rc.2"),
            )

    def test_parses_single_segment_directory(self) -> None:
        self.assertEqual(retitle.parse_connector_release_tag("connectors/zoom/v0.24.0").directory, "connectors/zoom")
        self.assertEqual(retitle.parse_connector_release_tag("connectors/stripe/python/v0.1.0").language, "python")

    def test_ignores_sdk_and_malformed_tags(self) -> None:
        for tag in ("sdkgo/v0.5.0", "sdkpython/v0.1.0", "connectors/zoom/v0.24", "connectors/zoom/0.24.0",
                    "connectors/zoom/v0.24.0-rc.0", "connectors/zoom/v0.24.0-beta.1", "connectors/v0.1.0"):
            self.assertIsNone(retitle.parse_connector_release_tag(tag), tag)

    def test_reads_metadata_display_name_only(self) -> None:
        manifest = textwrap.dedent("""\
            apiVersion: connectors.dex.dev/v1alpha1
            spec:
              displayName: Wrong
            metadata:
              name: gmail
              displayName: "Gmail"
            """)
        self.assertEqual(retitle.read_manifest_display_name(manifest), "Gmail")
        with self.assertRaises(ValueError):
            retitle.read_manifest_display_name("metadata:\n  name: gmail\n")

    def test_plans_titles_and_skips_unknown_directories(self) -> None:
        releases = [
            {"tag_name": "connectors/zoom/v0.24.0", "name": "Zoom v0.24.0"},
            {"tag_name": "connectors/stripe/typescript/v0.1.0", "name": "Stripe typescript v0.1.0"},
            {"tag_name": "connectors/stripe/go/v0.25.0-rc.1", "name": "Stripe Go v0.25.0-rc.1"},
            {"tag_name": "connectors/removed/v0.1.0", "name": "Removed v0.1.0"},
            {"tag_name": "connectors/removed/python/v0.2.0", "name": "Removed python v0.2.0"},
            {"tag_name": "connectors/unknown/v0.1.0", "name": "Custom name"},
            {"tag_name": "sdkgo/v0.5.0", "name": "Go SDK v0.5.0"},
        ]
        display_names = {"connectors/zoom": "Zoom", "connectors/stripe": "Stripe", "connectors/removed": None,
                         "connectors/unknown": None}
        plans = {plan.tag: plan for plan in retitle.plan_release_retitles(releases, display_names)}
        self.assertEqual(set(plans), {release["tag_name"] for release in releases[:6]})
        self.assertEqual(plans["connectors/zoom/v0.24.0"].new_title, "Zoom Go v0.24.0")
        self.assertTrue(plans["connectors/zoom/v0.24.0"].is_change_required)
        self.assertEqual(plans["connectors/stripe/typescript/v0.1.0"].new_title, "Stripe TypeScript v0.1.0")
        self.assertFalse(plans["connectors/stripe/go/v0.25.0-rc.1"].is_change_required)
        self.assertEqual(plans["connectors/removed/v0.1.0"].new_title, "Removed Go v0.1.0")
        self.assertEqual(plans["connectors/removed/python/v0.2.0"].new_title, "Removed Python v0.2.0")
        self.assertFalse(plans["connectors/unknown/v0.1.0"].is_change_required)
        self.assertIn("no manifest", plans["connectors/unknown/v0.1.0"].skip_reason)


class MainRefRepositoryTest(unittest.TestCase):
    """Runs the script's Git reads and `main()` against a real temporary repository."""

    def setUp(self) -> None:
        temporary_directory = tempfile.TemporaryDirectory()
        self.addCleanup(temporary_directory.cleanup)
        self.repository = Path(temporary_directory.name)
        self.git("init", "--quiet", "--initial-branch=main")
        self.write_manifest("connectors/zoom", 'metadata:\n  name: zoom\n  displayName: "Zoom"\n')
        self.write_manifest("connectors/stripe", "metadata:\n  name: stripe\n  displayName: Stripe\n")
        self.git("add", ".")
        self.git("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "init")
        self.real_subprocess_run = subprocess.run
        self.release_edits: list[list[str]] = []
        self.failing_release_tags: set[str] = set()

    def git(self, *arguments: str) -> None:
        subprocess.run(["git", *arguments], cwd=self.repository, check=True, capture_output=True)

    def write_manifest(self, directory: str, content: str) -> None:
        manifest_path = self.repository / directory / "connector.yaml"
        manifest_path.parent.mkdir(parents=True, exist_ok=True)
        manifest_path.write_text(content)

    def fake_subprocess_run(self, command: list[str], **keyword_arguments):
        if command[0] == "git":
            return self.real_subprocess_run(command, cwd=self.repository, **keyword_arguments)
        if command[:3] == ["gh", "release", "edit"]:
            self.release_edits.append(command)
            return_code = 1 if command[3] in self.failing_release_tags else 0
            return subprocess.CompletedProcess(command, return_code, "", "edit failed" if return_code else "")
        raise AssertionError(f"unexpected command {command}")

    def run_main(self, releases: list[dict[str, str]], *arguments: str) -> tuple[int, str, str, mock.Mock, mock.Mock]:
        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.object(retitle.subprocess, "run", side_effect=self.fake_subprocess_run), \
                mock.patch.object(retitle, "list_releases", return_value=releases) as list_releases, \
                mock.patch.object(retitle.time, "sleep") as sleep, \
                mock.patch.object(sys, "argv", ["retitle", "--main-ref", "main", *arguments]), \
                contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            exit_code = retitle.main()
        return exit_code, stdout.getvalue(), stderr.getvalue(), list_releases, sleep

    def read_display_name(self, main_ref: str, directory: str) -> str | None:
        with mock.patch.object(retitle.subprocess, "run", side_effect=self.fake_subprocess_run):
            return retitle.read_main_display_name(main_ref, directory)

    def test_reads_display_name_from_main_manifest(self) -> None:
        self.assertEqual(self.read_display_name("main", "connectors/zoom"), "Zoom")

    def test_missing_manifest_on_main_returns_none(self) -> None:
        self.assertIsNone(self.read_display_name("main", "connectors/removed"))

    def test_invalid_ref_raises(self) -> None:
        with self.assertRaisesRegex(ValueError, "nonexistent-ref"):
            self.read_display_name("nonexistent-ref", "connectors/zoom")

    def test_manifest_without_display_name_raises(self) -> None:
        self.write_manifest("connectors/broken", "metadata:\n  name: broken\n")
        self.git("add", ".")
        self.git("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "broken")
        with self.assertRaisesRegex(ValueError, "invalid connectors/broken/connector.yaml"):
            self.read_display_name("main", "connectors/broken")

    def test_invalid_main_ref_stops_before_listing_or_editing(self) -> None:
        exit_code, stdout, stderr, list_releases, _ = self.run_main([], "--main-ref", "nonexistent-ref", "--apply")
        self.assertEqual(exit_code, 1)
        self.assertIn("nonexistent-ref", stderr)
        self.assertEqual(stdout, "")
        list_releases.assert_not_called()
        self.assertEqual(self.release_edits, [])

    def test_invalid_manifest_stops_before_editing(self) -> None:
        self.write_manifest("connectors/zoom", "metadata:\n  name: zoom\n")
        self.git("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "-am", "broken")
        releases = [{"tag_name": "connectors/stripe/v0.1.0", "name": "Stripe v0.1.0"},
                    {"tag_name": "connectors/zoom/v0.24.0", "name": "Zoom v0.24.0"}]
        exit_code, stdout, stderr, _, _ = self.run_main(releases, "--apply")
        self.assertEqual(exit_code, 1)
        self.assertIn("invalid connectors/zoom/connector.yaml", stderr)
        self.assertEqual(stdout, "")
        self.assertEqual(self.release_edits, [])

    def test_dry_run_lists_current_title_fallbacks_separately_and_edits_nothing(self) -> None:
        releases = [{"tag_name": "connectors/zoom/v0.24.0", "name": "Zoom v0.24.0"},
                    {"tag_name": "connectors/removed/python/v0.2.0", "name": "Removed python v0.2.0"}]
        exit_code, stdout, _, _, _ = self.run_main(releases)
        self.assertEqual(exit_code, 0)
        manifest_section, fallback_section = stdout.split("# No connector.yaml on main")
        self.assertIn("change\tconnectors/zoom/v0.24.0\tZoom v0.24.0\tZoom Go v0.24.0", manifest_section)
        self.assertNotIn("connectors/removed", manifest_section)
        self.assertIn("change\tconnectors/removed/python/v0.2.0\tRemoved python v0.2.0\tRemoved Python v0.2.0",
                      fallback_section)
        self.assertEqual(self.release_edits, [])

    def test_apply_edits_only_changed_titles_with_pacing(self) -> None:
        releases = [{"tag_name": "connectors/stripe/v0.1.0", "name": "Stripe v0.1.0"},
                    {"tag_name": "connectors/stripe/go/v0.2.0", "name": "Stripe Go v0.2.0"},
                    {"tag_name": "connectors/zoom/v0.24.0", "name": "Zoom v0.24.0"}]
        exit_code, _, _, _, sleep = self.run_main(releases, "--apply", "--delay-seconds", "2.5")
        self.assertEqual(exit_code, 0)
        self.assertEqual([(edit[3], edit[-1]) for edit in self.release_edits],
                         [("connectors/stripe/v0.1.0", "Stripe Go v0.1.0"),
                          ("connectors/zoom/v0.24.0", "Zoom Go v0.24.0")])
        sleep.assert_called_once_with(2.5)

    def test_apply_stops_at_first_failed_edit(self) -> None:
        self.failing_release_tags = {"connectors/stripe/v0.1.0"}
        releases = [{"tag_name": "connectors/stripe/v0.1.0", "name": "Stripe v0.1.0"},
                    {"tag_name": "connectors/zoom/v0.24.0", "name": "Zoom v0.24.0"}]
        exit_code, _, stderr, _, sleep = self.run_main(releases, "--apply")
        self.assertEqual(exit_code, 1)
        self.assertIn("stopped at connectors/stripe/v0.1.0: edit failed", stderr)
        self.assertEqual([edit[3] for edit in self.release_edits], ["connectors/stripe/v0.1.0"])
        sleep.assert_not_called()


if __name__ == "__main__":
    unittest.main()
