# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import json
import re
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
HOOKS = (
    ROOT / ".codex" / "hooks" / "validate-branch-off-main.sh",
    ROOT / ".cursor" / "hooks" / "reject-cursor-coauthor.sh",
    ROOT / ".cursor" / "hooks" / "validate-branch-off-main.sh",
    ROOT / ".githooks" / "commit-msg",
    ROOT / "script" / "install-githooks",
)


class AgentRulesTest(unittest.TestCase):
    def test_hook_json_is_valid(self) -> None:
        for path in (ROOT / ".codex" / "hooks.json", ROOT / ".cursor" / "hooks.json"):
            with self.subTest(path=path):
                with path.open(encoding="utf-8") as handle:
                    self.assertIsInstance(json.load(handle), dict)

    def test_codex_hook_resolves_from_a_repository_subdirectory(self) -> None:
        with (ROOT / ".codex" / "hooks.json").open(encoding="utf-8") as handle:
            configuration = json.load(handle)
        command = configuration["hooks"]["PreToolUse"][0]["hooks"][0]["command"]

        subprocess.run(
            command,
            check=True,
            cwd=ROOT / "sdkgo",
            input=json.dumps({"tool_input": {"command": "go test ./..."}}),
            shell=True,
            text=True,
        )

    def test_hook_shell_is_valid(self) -> None:
        for path in HOOKS:
            with self.subTest(path=path):
                subprocess.run(["sh", "-n", path], check=True, cwd=ROOT)

    def test_agent_rules_exclude_inapplicable_implementation_rules(self) -> None:
        paths = [ROOT / "AGENTS.md", ROOT / "CLAUDE.md"]
        paths.extend((ROOT / ".cursor" / "rules").glob("*.mdc"))
        paths.extend((ROOT / ".codex" / "rules").glob("*.md"))
        forbidden = re.compile(
            r"\b(?:Temporal|Cadence|Docusaurus|interpreter|Gin|Rust|Java|proto)\b"
            r"|server/\*\*",
            re.IGNORECASE,
        )
        for path in paths:
            with self.subTest(path=path):
                contents = path.read_text(encoding="utf-8")
                self.assertIsNone(forbidden.search(contents))

    def test_claude_uses_the_canonical_rules(self) -> None:
        self.assertEqual((ROOT / "CLAUDE.md").read_text(encoding="utf-8").strip(), "@AGENTS.md")

    def test_rules_reject_vague_normalize_and_runtime_names(self) -> None:
        paths = (
            ROOT / "AGENTS.md",
            ROOT / ".cursor" / "rules" / "project-core.mdc",
        )
        required_fragments = (
            "`normalize`, `normalizer`, or `runtime`",
            "packages, directories, files, classes, structs, interfaces, fields,",
            "parameters, variables, or helpers",
        )
        for path in paths:
            with self.subTest(path=path):
                contents = path.read_text(encoding="utf-8")
                for fragment in required_fragments:
                    self.assertIn(fragment, contents)

    def test_oauth_authorization_evidence_rules_are_synchronized(self) -> None:
        paths = (
            ROOT / "AGENTS.md",
            ROOT / ".cursor" / "rules" / "oauth-authorization-evidence.mdc",
            ROOT / ".codex" / "rules" / "oauth-authorization-evidence.md",
        )
        required_fragments = (
            "`spec.auth.type: oauth2`",
            "including OAuth 2.0 and OpenID Connect",
            "API keys, manually entered tokens, webhook secrets",
            "attach\n  a screenshot to the pull request description",
            "Do not commit authorization screenshots to the repository",
            "skip the screenshot only when the pull request gives a specific\n  reason",
            "Never fabricate authorization evidence",
            "Reviewers confirm exactly one evidence option is selected",
        )
        for path in paths:
            with self.subTest(path=path):
                contents = path.read_text(encoding="utf-8")
                for fragment in required_fragments:
                    self.assertIn(fragment, contents)

    def test_pull_request_template_requires_evidence_or_a_skip_reason(self) -> None:
        template = (ROOT / ".github" / "PULL_REQUEST_TEMPLATE.md").read_text(
            encoding="utf-8"
        )
        required_fragments = (
            "`spec.auth.type: oauth2`",
            "- [ ] Screenshot attached",
            "- [ ] Screenshot skipped",
            'Skip reason: <!-- Required when "Screenshot skipped" is selected. -->',
            "### Reviewer checklist",
            "Exactly one evidence option is selected",
            "documented skip reason is specific and acceptable",
            "Sensitive credentials and unrelated personal information are not exposed",
        )
        for fragment in required_fragments:
            self.assertIn(fragment, template)

    def test_readme_documents_oauth_authorization_evidence(self) -> None:
        contents = (ROOT / "README.md").read_text(encoding="utf-8")
        for fragment in (
            "`spec.auth.type: oauth2`",
            "This applies to OAuth 2.0 and OpenID Connect, not API",
            "specific reason",
            "Reviewers decide whether the screenshot or reason is",
        ):
            self.assertIn(fragment, contents)

    def test_cursor_hook_rejects_skipping_git_hooks(self) -> None:
        payload = json.dumps({"command": "git commit --no-verify -m test"})

        result = subprocess.run(
            ["sh", ROOT / ".cursor" / "hooks" / "reject-cursor-coauthor.sh"],
            check=True,
            cwd=ROOT,
            input=payload,
            capture_output=True,
            text=True,
        )

        response = json.loads(result.stdout)
        self.assertEqual(response["permission"], "deny")
        self.assertIn("Do not skip git hooks", response["agent_message"])

    def test_commit_message_hook_removes_only_cursor_attribution(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            message_path = Path(directory) / "COMMIT_EDITMSG"
            message_path.write_text(
                "tooling: test hooks\n\n"
                "Co-authored-by: Cursor <cursoragent@cursor.com>\n"
                "Co-authored-by: Human <human@example.com>\n"
                "Made-with: Cursor\n",
                encoding="utf-8",
            )

            subprocess.run(
                ["sh", ROOT / ".githooks" / "commit-msg", message_path],
                check=True,
                cwd=ROOT,
            )

            message = message_path.read_text(encoding="utf-8")
            self.assertNotIn("Cursor", message)
            self.assertNotIn("cursoragent@cursor.com", message)
            self.assertIn("Co-authored-by: Human <human@example.com>", message)


if __name__ == "__main__":
    unittest.main()
