# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import contextlib
import importlib.util
import io
import shutil
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "script" / "studio_bundle_theme_check.py"
SPEC = importlib.util.spec_from_file_location("studio_bundle_theme_check", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
CHECK = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = CHECK
SPEC.loader.exec_module(CHECK)
CONTRACT_CLASS_NAMES = CHECK.read_studio_class_contract(ROOT / CHECK.STUDIO_THEME_SOURCE)

THEMED_MAIN = """\
import { useEffect } from "react";
import {
  applyConnectorStudioTheme,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";

export function App() {
  const {ready} = useConnectorStudioClient("fixture");
  useEffect(() => { if (ready) applyConnectorStudioTheme(ready); }, [ready]);
  return <p className="studio-muted">Flow's connection</p>;
}
"""
INDEX_HTML = """\
<!-- Copyright (c) 2026 Super Durable -->
<!doctype html>
<html><body><div id="root"></div><script type="module" src="/src/main.tsx"></script></body></html>
"""
VITE_CONFIG = """\
export default defineConfig({
  plugins: [react(), viteSingleFile()],
  resolve: { dedupe: ["react", "react-dom"] },
});
"""


class StudioBundleThemeCheckTest(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.repository_root = Path(self.directory.name)
        studio_theme = self.repository_root / CHECK.STUDIO_THEME_SOURCE
        studio_theme.parent.mkdir(parents=True)
        shutil.copyfile(ROOT / CHECK.STUDIO_THEME_SOURCE, studio_theme)

    def write_user_interface(
        self,
        connector: str,
        sources: dict[str, str],
        index_html: str = INDEX_HTML,
        vite_config: str | None = VITE_CONFIG,
    ) -> Path:
        user_interface = self.repository_root / "connectors" / connector / "ui"
        (user_interface / "src").mkdir(parents=True)
        (user_interface / "package.json").write_text("{}", encoding="utf-8")
        (user_interface / "index.html").write_text(index_html, encoding="utf-8")
        if vite_config is not None:
            (user_interface / "vite.config.ts").write_text(vite_config, encoding="utf-8")
        for name, contents in sources.items():
            (user_interface / "src" / name).write_text(contents, encoding="utf-8")
        return user_interface

    def reasons(self) -> list[str]:
        return [violation.reason for violation in CHECK.find_studio_bundle_theme_violations(self.repository_root)]

    def user_interface_violations(self, user_interface: Path) -> list:
        return CHECK.find_user_interface_violations(user_interface, CONTRACT_CLASS_NAMES)

    def test_repository_bundles_use_the_shared_theme(self) -> None:
        violations = CHECK.find_studio_bundle_theme_violations(ROOT)

        self.assertEqual([violation.describe(ROOT) for violation in violations], [])
        self.assertGreaterEqual(len(CHECK.find_studio_user_interface_directories(ROOT)), 4)

    def test_accepts_theme_calls_and_the_model_picker_bundle(self) -> None:
        self.write_user_interface("acme/mail", {"main.tsx": THEMED_MAIN})
        self.write_user_interface(
            "acme/llm",
            {"main.tsx": 'import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";\n'
             'mountModelPickerBundle({connectorId: "llm", providerName: "LLM", loadModels});\n'},
        )
        self.write_user_interface(
            "acme/alias",
            {"main.tsx": 'import { applyConnectorStudioTheme as applyTheme } from "@superdurable/dex-connectors-react";\n'
             "useEffect(() => { if (ready) applyTheme(ready); }, [ready]);\n"},
        )
        self.write_user_interface(
            "acme/namespace",
            {"main.tsx": 'import * as React from "react";\nimport * as studio from "@superdurable/dex-connectors-react";\n'
             "React.useEffect(() => { if (client.ready) studio.applyConnectorStudioTheme(client.ready!); }, [client.ready]);\n"},
        )
        self.write_user_interface(
            "acme/reexport",
            {
                "theme.ts": 'export { applyConnectorStudioTheme } from "@superdurable/dex-connectors-react";\n',
                "main.tsx": 'import { applyConnectorStudioTheme } from "./theme.js";\n'
                "useLayoutEffect(() => {\n  applyConnectorStudioTheme(ready);\n}, [ready]);\n",
            },
        )

        self.assertEqual(self.reasons(), [])

    def test_rejects_style_elements_injected_by_source_files(self) -> None:
        cases = {
            "create-element": 'const style = document.createElement("style");\n',
            "create-element-template": "document.createElement(`style`);\n",
            "jsx": "export const Styles = () => <style>{`.card{padding:24px}`}</style>;\n",
            "inner-html": 'document.head.innerHTML += "<style>body{margin:0}</style>";\n',
            "create-element-uppercase": 'document.createElement("STYLE");\n',
            "create-link": 'const link = document.createElement("link");\n',
            "jsx-link": 'export const Styles = () => <link href="/app.css" rel="stylesheet"/>;\n',
            "stylesheet": "document.adoptedStyleSheets = [new CSSStyleSheet()];\n",
            "insert-rule": 'document.styleSheets[0].insertRule("body { margin: 0 }");\n',
            "shared-theme-element": 'document.getElementById("dex-connector-studio-theme")!.textContent += "body{margin:0}";\n',
            "jsx-style-prop": 'export const Avatar = () => <img alt="" style={{width: 20, borderRadius: 5}}/>;\n',
            "style-property": 'document.body.style.background = "#fff";\n',
            "css-text": 'document.body.style.cssText = "margin: 0";\n',
            "style-attribute": 'document.body.setAttribute("style", "margin: 0");\n',
            "css-import": 'import "./app.css";\n',
            "css-inline-import": 'import styles from "./app.css?inline";\n',
            "css-dynamic-import": 'void import("./app.css");\n',
        }
        for connector, injection in cases.items():
            with self.subTest(connector=connector):
                user_interface = self.write_user_interface(f"acme/{connector}", {"main.tsx": THEMED_MAIN, "styles.tsx": injection})
                violations = self.user_interface_violations(user_interface)
                self.assertEqual([violation.path.name for violation in violations], ["styles.tsx"])

    def test_rejects_styles_in_the_bundle_entrypoint(self) -> None:
        for connector, head in {
            "inline": "<style>:root{color:#241524}</style>",
            "linked": '<link rel="stylesheet" href="/app.css">',
            "style-attribute": '</head><body style="margin:0"><div id="root"></div></body><head>',
        }.items():
            with self.subTest(connector=connector):
                user_interface = self.write_user_interface(
                    f"acme/{connector}", {"main.tsx": THEMED_MAIN}, f"<!doctype html><html><head>{head}</head></html>"
                )
                violations = self.user_interface_violations(user_interface)
                self.assertEqual([violation.path.name for violation in violations], ["index.html"])

    def test_ignores_style_mentions_in_comments(self) -> None:
        self.write_user_interface(
            "acme/mail",
            {"main.tsx": THEMED_MAIN + "// Never add a <style> element; see https://example.com/theme.\n"
             "/* document.createElement(\"style\") */\n"},
            INDEX_HTML.replace("<!doctype html>", "<!-- no <style> here --><!doctype html>"),
        )

        self.assertEqual(self.reasons(), [])

    def test_ignores_style_mentions_in_tests_and_string_text(self) -> None:
        self.write_user_interface(
            "acme/mail",
            {
                "main.tsx": THEMED_MAIN + 'export const pasteHint = "Do not paste <style> tags";\n',
                "units.test.tsx": 'expect(markup).not.toContain("<style>");\ndocument.createElement("style");\n',
            },
        )

        self.assertEqual(self.reasons(), [])

    def test_rejects_a_theme_call_that_does_not_follow_ready_messages(self) -> None:
        shared_import = 'import { useEffect } from "react";\nimport { applyConnectorStudioTheme } from "@superdurable/dex-connectors-react";\n'
        cases = {
            "module-scope": "applyConnectorStudioTheme();\n",
            "module-scope-argument": "applyConnectorStudioTheme(window.ready);\n",
            "no-argument": "useEffect(() => { applyConnectorStudioTheme(); }, [ready]);\n",
            "missing-dependency": "useEffect(() => { applyConnectorStudioTheme(ready); }, []);\n",
            "no-dependency-list": "useEffect(() => { applyConnectorStudioTheme(ready); });\n",
            "other-dependency": "useEffect(() => { applyConnectorStudioTheme(ready); }, [client]);\n",
        }
        for connector, body in cases.items():
            with self.subTest(connector=connector):
                user_interface = self.write_user_interface(f"acme/{connector}", {"main.tsx": shared_import + body})
                reasons = [violation.reason for violation in self.user_interface_violations(user_interface)]
                self.assertEqual(len(reasons), 1)
                self.assertIn("outside a React effect", reasons[0])

    def test_rejects_a_bundle_that_never_applies_the_theme(self) -> None:
        cases = {
            "missing": {"main.tsx": 'import { useConnectorStudioClient } from "@superdurable/dex-connectors-react";\n'},
            "comment-only": {"main.tsx": 'import { applyConnectorStudioTheme } from "@superdurable/dex-connectors-react";\n'
                             "// applyConnectorStudioTheme(ready);\n"},
            "local-function": {"main.tsx": "function applyConnectorStudioTheme() {}\napplyConnectorStudioTheme();\n"},
            "test-only": {"main.tsx": "export {};\n", "main.test.tsx": THEMED_MAIN},
        }
        for connector, sources in cases.items():
            with self.subTest(connector=connector):
                user_interface = self.write_user_interface(f"acme/{connector}", sources)
                reasons = [violation.reason for violation in self.user_interface_violations(user_interface)]
                self.assertEqual(len(reasons), 1)
                self.assertIn("never calls applyConnectorStudioTheme", reasons[0])

    def test_rejects_a_bundle_that_can_inline_a_second_react(self) -> None:
        cases = {
            "missing-config": None,
            "no-dedupe": "export default defineConfig({plugins: [react()]});\n",
            "react-only": 'export default defineConfig({resolve: {dedupe: ["react"]}});\n',
            "commented-out": 'export default defineConfig({\n  // resolve: {dedupe: ["react", "react-dom"]},\n});\n',
        }
        for connector, vite_config in cases.items():
            with self.subTest(connector=connector):
                user_interface = self.write_user_interface(f"acme/{connector}", {"main.tsx": THEMED_MAIN}, vite_config=vite_config)
                reasons = [violation.reason for violation in self.user_interface_violations(user_interface)]
                self.assertEqual(len(reasons), 1)
                self.assertIn("react-dom", reasons[0])

    def test_reads_the_class_contract_from_sdk_react(self) -> None:
        self.assertIn("studio-surface", CONTRACT_CLASS_NAMES)
        self.assertIn("studio-button-primary", CONTRACT_CLASS_NAMES)
        self.assertGreaterEqual(len(CONTRACT_CLASS_NAMES), 20)
        self.assertTrue(all(class_name.startswith("studio-") for class_name in CONTRACT_CLASS_NAMES))

    def test_accepts_class_name_literals_from_the_contract(self) -> None:
        self.write_user_interface(
            "acme/mail",
            {"main.tsx": THEMED_MAIN
             + 'export const Actions = () => <div className="studio-actions"><span className={"studio-muted"}/></div>;\n'
             + "export const Option = () => <label className={`studio-option`}><span className='studio-option-label'/></label>;\n"
             + 'export const Save = () => <StudioButton className="studio-button-primary">Save</StudioButton>;\n'
             + 'const className = "unrelated";\n'
             + 'export const Hint = () => createElement("p", {className: "studio-muted", id: "hint"});\n'
             + "export const Notice = () => <p {...{'className': `studio-notice studio-notice-info`}}/>;\n"},
        )

        self.assertEqual(self.reasons(), [])

    def test_rejects_classes_outside_the_contract(self) -> None:
        cases = {
            "plain": ('export const Card = () => <div className="card"/>;\n', "card"),
            "mixed": ('export const Card = () => <div className="studio-surface studio-card"/>;\n', "studio-card"),
            "braced": ("export const Card = () => <div className={'studio-panel'}/>;\n", "studio-panel"),
            "template": ("export const Card = () => <div className={`studio-surface grid`}/>;\n", "grid"),
            "create-element": ('export const Hero = () => createElement("div", {className: "acme-hero"});\n', "acme-hero"),
            "jsx-spread": ('export const Card = () => <div {...{className: "acme-card"}}/>;\n', "acme-card"),
            "quoted-key": ('export const cardProps = {"className": "studio-surface card"};\n', "card"),
            "computed-key": ("export const cardProps = {['className']: `studio-panel`};\n", "studio-panel"),
        }
        for connector, (source, class_name) in cases.items():
            with self.subTest(connector=connector):
                user_interface = self.write_user_interface(f"acme/{connector}", {"main.tsx": THEMED_MAIN, "card.tsx": source})
                violations = self.user_interface_violations(user_interface)
                self.assertEqual([violation.path.name for violation in violations], ["card.tsx"])
                self.assertIn(f'renders class "{class_name}"', violations[0].reason)

    def test_rejects_class_names_the_check_cannot_read(self) -> None:
        cases = {
            "conditional": 'export const Card = ({isOpen}) => <div className={isOpen ? "studio-surface" : ""}/>;\n',
            "variable": "export const Card = ({classes}) => <div className={classes}/>;\n",
            "interpolated": "export const Card = ({tone}) => <p className={`studio-notice-${tone}`}/>;\n",
            "dom-class-name": 'document.body.className = "studio-surface";\n',
            "dom-class-list": 'document.body.classList.add("studio-surface");\n',
            "dom-class-attribute": 'document.body.setAttribute("class", "studio-surface");\n',
            "object-conditional": 'export const Card = ({isOpen}) => createElement("div", {className: isOpen ? "studio-surface" : ""});\n',
            "object-concatenated": 'export const Card = ({tone}) => createElement("p", {className: "studio-notice " + tone});\n',
            "object-shorthand": 'const className = "acme-card";\nexport const Card = () => createElement("div", {id: "card", className});\n',
        }
        for connector, source in cases.items():
            with self.subTest(connector=connector):
                user_interface = self.write_user_interface(f"acme/{connector}", {"main.tsx": THEMED_MAIN, "card.tsx": source})
                violations = self.user_interface_violations(user_interface)
                self.assertEqual([violation.path.name for violation in violations], ["card.tsx"])
                self.assertRegex(violations[0].reason, "className")

    def test_rejects_entrypoint_classes_outside_the_contract(self) -> None:
        user_interface = self.write_user_interface(
            "acme/mail", {"main.tsx": THEMED_MAIN}, INDEX_HTML.replace('<div id="root">', '<div class="app studio-surface" id="root">')
        )

        violations = self.user_interface_violations(user_interface)
        self.assertEqual([(violation.path.name, violation.reason.split(",")[0]) for violation in violations],
                         [("index.html", 'renders class "app"')])

    def test_fails_without_the_class_contract(self) -> None:
        self.write_user_interface("acme/mail", {"main.tsx": THEMED_MAIN})
        (self.repository_root / CHECK.STUDIO_THEME_SOURCE).write_text("export const connectorStudioStyles = '';\n", encoding="utf-8")

        self.assertEqual(self.reasons(), ["does not declare the connectorStudioClassNames contract"])

    def test_skips_installed_dependencies(self) -> None:
        user_interface = self.write_user_interface("acme/mail", {"main.tsx": THEMED_MAIN})
        dependency = user_interface / "node_modules" / "styled" / "ui"
        (dependency / "src").mkdir(parents=True)
        (dependency / "package.json").write_text("{}", encoding="utf-8")
        (dependency / "src" / "index.js").write_text('document.createElement("style");\n', encoding="utf-8")

        self.assertEqual(CHECK.find_studio_user_interface_directories(self.repository_root), [user_interface])
        self.assertEqual(self.reasons(), [])

    def test_main_reports_violations_and_fails(self) -> None:
        self.write_user_interface("acme/mail", {"main.tsx": 'const style = document.createElement("style");\n'})
        errors = io.StringIO()

        with contextlib.redirect_stderr(errors), contextlib.redirect_stdout(io.StringIO()):
            status = CHECK.main([str(self.repository_root)])

        self.assertEqual(status, 1)
        self.assertIn("connectors/acme/mail/ui/src/main.tsx: creates a <style> element", errors.getvalue())
        self.assertIn("connectors/acme/mail/ui/src: never calls applyConnectorStudioTheme", errors.getvalue())

    def test_main_fails_when_no_user_interfaces_are_found(self) -> None:
        with contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(CHECK.main([str(self.repository_root)]), 1)

    def test_main_passes_for_themed_bundles(self) -> None:
        self.write_user_interface("acme/mail", {"main.tsx": THEMED_MAIN})
        output = io.StringIO()

        with contextlib.redirect_stdout(output):
            status = CHECK.main([str(self.repository_root)])

        self.assertEqual(status, 0)
        self.assertIn("1 Connector Studio UIs use the shared theme", output.getvalue())


if __name__ == "__main__":
    unittest.main()
