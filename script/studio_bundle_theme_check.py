# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Check that every Connector Studio bundle uses the shared sdk/react theme and Studio class contract."""

from __future__ import annotations

import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SHARED_PACKAGE = "@superdurable/dex-connectors-react"
THEME_ENTRY_POINT = "applyConnectorStudioTheme"
MODEL_PICKER_ENTRY_POINT = "mountModelPickerBundle"
THEME_ENTRY_POINTS = (THEME_ENTRY_POINT, MODEL_PICKER_ENTRY_POINT)
SHARED_THEME_STYLE_ELEMENT_ID = "dex-connector-studio-theme"
SOURCE_SUFFIXES = {".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}
SKIPPED_DIRECTORIES = {"node_modules", "dist", ".git"}
TEST_FILE_PATTERN = re.compile(r"\.(?:test|spec)\.[cm]?[jt]sx?$")
STYLESHEET_MODULE = r"""["'`][^"'`]+\.(?:css|scss|sass|less)(?:\?[^"'`]*)?["'`]"""
STUDIO_THEME_SOURCE = Path("sdk") / "react" / "src" / "studio-theme.ts"
CLASS_CONTRACT_PATTERN = re.compile(r"connectorStudioClassNames\s*=\s*Object\.freeze\(\s*\[(?P<class_names>[^\]]*)\]")
JSX_CLASS_NAME_PATTERN = re.compile(r"(?<![\w$.-])(?<!const )(?<!let )(?<!var )className\s*=\s*")
LITERAL_CLASS_NAME_PATTERN = re.compile(r"""(["'])(?P<quoted>[^"'\n]*)\1|\{\s*(["'])(?P<braced>[^"'\n]*)\3\s*\}|\{\s*`(?P<template>[^`$]*)`\s*\}""")
# A className property key, as in createElement props or a JSX spread: className, "className", or ["className"].
OBJECT_CLASS_NAME_PATTERN = re.compile(
    r"""(?<![\w$.-])(?<!const )(?<!let )(?<!var )(?:className|(["'])className\1|\[\s*(["'`])className\2\s*\])\s*\??\s*:\s*"""
)
OBJECT_LITERAL_CLASS_NAME_PATTERN = re.compile(r"""(?:(["'])(?P<quoted>[^"'\n]*)\1|`(?P<template>[^`$]*)`)(?=\s*[,}])""")
OBJECT_SHORTHAND_CLASS_NAME_PATTERN = re.compile(r"[{,]\s*className\s*(?=[,}])")
INDEX_CLASS_PATTERN = re.compile(r"""<[a-z][\w-]*\b[^>]*\sclass\s*=\s*["'](?P<classes>[^"']*)["']""", re.IGNORECASE)

# Matched against source whose comments are removed and whose string literal text is kept.
STRING_STYLE_INJECTION_PATTERNS = (
    (re.compile(r"""createElement(?:NS)?\s*\([^)]*?["'`]style["'`]""", re.IGNORECASE), "creates a <style> element"),
    (
        re.compile(r"""createElement(?:NS)?\s*\([^)]*?["'`]link["'`]""", re.IGNORECASE),
        "creates a <link> element, which can load a stylesheet",
    ),
    (re.compile(r"""setAttribute(?:NS)?\s*\([^)]*?["'`]style["'`]""", re.IGNORECASE), "writes an inline style attribute"),
    (
        re.compile(r"""setAttribute(?:NS)?\s*\([^)]*?["'`]class["'`]""", re.IGNORECASE),
        "sets classes through the DOM instead of a className string literal",
    ),
    (
        re.compile(r"""(?:querySelector(?:All)?|getElementsByTagName)\s*\(\s*["'`]\s*style\b""", re.IGNORECASE),
        "reaches into a <style> element",
    ),
    (re.compile(r"""["'`]#?""" + SHARED_THEME_STYLE_ELEMENT_ID + r"""["'`]"""), "edits the shared theme <style> element"),
    (
        re.compile(r"""^\s*(?:import|export)\s+(?:[\w$*{}\s,]+\s+from\s+)?""" + STYLESHEET_MODULE, re.MULTILINE),
        "imports a stylesheet, which the build injects as a <style> element",
    ),
    (re.compile(r"""\b(?:import|require)\s*\(\s*""" + STYLESHEET_MODULE), "loads a stylesheet at run time"),
)
# Matched against source whose comments and string literal text are removed.
CODE_STYLE_INJECTION_PATTERNS = (
    (re.compile(r"<style[\s>/]", re.IGNORECASE), "renders a <style> element"),
    (re.compile(r"<link\b", re.IGNORECASE), "renders a <link> element, which can load a stylesheet"),
    (
        re.compile(r"\bnew\s+CSSStyleSheet\s*\(|\badoptedStyleSheets\b|\bstyleSheets\b|\b(?:insertRule|replaceSync)\s*\("),
        "constructs or edits a stylesheet",
    ),
    (re.compile(r"(?<![\w$.])(?<!const )(?<!let )(?<!var )style\s*=\s*\{"), "sets an inline style prop"),
    (re.compile(r"\.style\s*(?:[.\[]|=(?!=))|\bcssText\b"), "writes inline styles"),
    (
        re.compile(r"\.className\s*=(?!=)|\.classList\b"),
        "sets classes through the DOM instead of a className string literal",
    ),
    (
        re.compile(
            r"\b(?:innerHTML|outerHTML|insertAdjacentHTML|dangerouslySetInnerHTML|createContextualFragment)\b"
            r"|\bdocument\s*\.\s*write(?:ln)?\s*\("
        ),
        "writes raw HTML, which can inject a <style> element",
    ),
)
INDEX_STYLE_PATTERNS = (
    (re.compile(r"<style[\s>/]", re.IGNORECASE), "declares a <style> element"),
    (re.compile(r"""<link\b[^>]*\brel\s*=\s*["']?stylesheet""", re.IGNORECASE), "links its own stylesheet"),
    (re.compile(r"""<[a-z][\w-]*\b[^>]*\sstyle\s*=""", re.IGNORECASE), "sets an inline style attribute"),
)
NAMED_IMPORT_PATTERN = re.compile(
    r"""import\s+(?:type\s+)?(?:[\w$]+\s*,\s*)?\{(?P<specifiers>[^}]*)\}\s*from\s*["'](?P<module>[^"']+)["']"""
)
NAMESPACE_IMPORT_PATTERN = re.compile(
    r"""import\s+\*\s+as\s+(?P<namespace>[\w$]+)\s+from\s*["']""" + re.escape(SHARED_PACKAGE) + r"""["']"""
)
REEXPORT_PATTERN = re.compile(
    r"""export\s*\{(?P<specifiers>[^}]*)\}\s*from\s*["']""" + re.escape(SHARED_PACKAGE) + r"""["']"""
)
EFFECT_CALL_PATTERN = re.compile(r"(?<![\w$.])(?:[\w$]+\s*\.\s*)?use(?:Layout)?Effect\s*\(")
VITE_CONFIG_NAMES = ("vite.config.ts", "vite.config.mts", "vite.config.js", "vite.config.mjs")
DEDUPE_PATTERN = re.compile(r"\bdedupe\s*:\s*\[(?P<packages>[^\]]*)\]")
DEDUPED_PACKAGES = ("react", "react-dom")
OPENING_BRACKETS = {"(": ")", "[": "]", "{": "}"}


@dataclass(frozen=True)
class StudioBundleThemeViolation:
    """One way a Connector Studio UI bypasses the shared theme."""

    path: Path
    reason: str

    def describe(self, repository_root: Path) -> str:
        try:
            display_path = self.path.relative_to(repository_root)
        except ValueError:
            display_path = self.path
        return f"{display_path}: {self.reason}"


@dataclass(frozen=True)
class BundleSourceFile:
    """One non-test ui/src file, with comments removed and, in code, string literal text removed too."""

    path: Path
    source: str
    code: str


@dataclass(frozen=True)
class ThemeEntryPointCall:
    """One call of a shared theme entry point."""

    entry_point: str
    is_applied_from_ready_effect: bool


def main(arguments: list[str]) -> int:
    repository_root = Path(arguments[0]).resolve() if arguments else ROOT
    user_interface_directories = find_studio_user_interface_directories(repository_root)
    if not user_interface_directories:
        print(f"no Connector Studio UIs found below {repository_root / 'connectors'}", file=sys.stderr)
        return 1
    violations = find_studio_bundle_theme_violations(repository_root)
    if violations:
        print("Connector Studio bundles must use the shared sdk/react theme:", file=sys.stderr)
        for violation in violations:
            print(f"  {violation.describe(repository_root)}", file=sys.stderr)
        print(
            "Remove the bundle's own styles, render only classes from connectorStudioClassNames as className "
            "string literals, call applyConnectorStudioTheme(ready) from a React effect that depends on ready, "
            "or mountModelPickerBundle(...), from @superdurable/dex-connectors-react, and dedupe react and "
            "react-dom in the Vite config.",
            file=sys.stderr,
        )
        return 1
    print(f"{len(user_interface_directories)} Connector Studio UIs use the shared theme")
    return 0


def find_studio_bundle_theme_violations(repository_root: Path) -> list[StudioBundleThemeViolation]:
    studio_theme_path = repository_root / STUDIO_THEME_SOURCE
    contract_class_names = read_studio_class_contract(studio_theme_path)
    if not contract_class_names:
        return [StudioBundleThemeViolation(studio_theme_path, "does not declare the connectorStudioClassNames contract")]
    violations: list[StudioBundleThemeViolation] = []
    for user_interface_directory in find_studio_user_interface_directories(repository_root):
        violations.extend(find_user_interface_violations(user_interface_directory, contract_class_names))
    return violations


def read_studio_class_contract(studio_theme_path: Path) -> frozenset[str]:
    """Return the class names in sdk/react's connectorStudioClassNames, or an empty set when it cannot be read."""
    if not studio_theme_path.is_file():
        return frozenset()
    match = CLASS_CONTRACT_PATTERN.search(studio_theme_path.read_text(encoding="utf-8"))
    return frozenset(re.findall(r"""["'](studio-[\w-]+)["']""", match.group("class_names"))) if match else frozenset()


def find_studio_user_interface_directories(repository_root: Path) -> list[Path]:
    connectors_root = repository_root / "connectors"
    directories: list[Path] = []
    for directory, subdirectories, files in os.walk(connectors_root):
        subdirectories[:] = sorted(name for name in subdirectories if name not in SKIPPED_DIRECTORIES)
        current = Path(directory)
        if current.name == "ui" and "package.json" in files:
            directories.append(current)
    return sorted(directories)


def find_user_interface_violations(
    user_interface_directory: Path, contract_class_names: frozenset[str]
) -> list[StudioBundleThemeViolation]:
    violations: list[StudioBundleThemeViolation] = []
    bundle_source_files = [
        read_bundle_source_file(source_path)
        for source_path in find_source_files(user_interface_directory / "src")
        if not TEST_FILE_PATTERN.search(source_path.name)
    ]
    for bundle_source_file in bundle_source_files:
        violations.extend(find_style_injection_violations(bundle_source_file))
        violations.extend(find_class_contract_violations(bundle_source_file, contract_class_names))
    theme_reexports = find_theme_reexports(bundle_source_files)
    has_theme_call = False
    for bundle_source_file in bundle_source_files:
        theme_calls = find_theme_entry_point_calls(bundle_source_file, theme_reexports)
        has_theme_call = has_theme_call or bool(theme_calls)
        if any(not theme_call.is_applied_from_ready_effect for theme_call in theme_calls):
            violations.append(
                StudioBundleThemeViolation(
                    bundle_source_file.path,
                    f"calls {THEME_ENTRY_POINT} outside a React effect that passes it the host ready message "
                    "and lists that message as a dependency",
                )
            )
    index_path = user_interface_directory / "index.html"
    if index_path.is_file():
        index = re.sub(r"<!--.*?-->", "", index_path.read_text(encoding="utf-8"), flags=re.DOTALL)
        for pattern, reason in INDEX_STYLE_PATTERNS:
            if pattern.search(index):
                violations.append(StudioBundleThemeViolation(index_path, reason))
        for match in INDEX_CLASS_PATTERN.finditer(index):
            violations.extend(
                find_classes_outside_contract(index_path, match.group("classes"), contract_class_names)
            )
    if not has_theme_call:
        violations.append(
            StudioBundleThemeViolation(
                user_interface_directory / "src",
                f"never calls {THEME_ENTRY_POINT} or {MODEL_PICKER_ENTRY_POINT} from " + SHARED_PACKAGE,
            )
        )
    violations.extend(find_react_dedupe_violations(user_interface_directory))
    return violations


def read_bundle_source_file(source_path: Path) -> BundleSourceFile:
    raw_source = source_path.read_text(encoding="utf-8")
    return BundleSourceFile(
        source_path,
        remove_comments(raw_source, should_keep_string_text=True),
        remove_comments(raw_source, should_keep_string_text=False),
    )


def find_style_injection_violations(bundle_source_file: BundleSourceFile) -> list[StudioBundleThemeViolation]:
    violations: list[StudioBundleThemeViolation] = []
    for pattern, reason in STRING_STYLE_INJECTION_PATTERNS:
        if pattern.search(bundle_source_file.source):
            violations.append(StudioBundleThemeViolation(bundle_source_file.path, reason))
    for pattern, reason in CODE_STYLE_INJECTION_PATTERNS:
        if pattern.search(bundle_source_file.code):
            violations.append(StudioBundleThemeViolation(bundle_source_file.path, reason))
    return violations


def find_class_contract_violations(
    bundle_source_file: BundleSourceFile, contract_class_names: frozenset[str]
) -> list[StudioBundleThemeViolation]:
    """Report JSX className attributes and className object properties that are not string literals of contract classes."""
    violations: list[StudioBundleThemeViolation] = []
    source = bundle_source_file.source
    for attribute in JSX_CLASS_NAME_PATTERN.finditer(source):
        literal = LITERAL_CLASS_NAME_PATTERN.match(source, attribute.end())
        if not literal:
            violations.append(
                StudioBundleThemeViolation(
                    bundle_source_file.path,
                    "computes a className the check cannot read; use a string literal of connectorStudioClassNames",
                )
            )
            continue
        classes = next(group for group in literal.group("quoted", "braced", "template") if group is not None)
        violations.extend(find_classes_outside_contract(bundle_source_file.path, classes, contract_class_names))
    for property_key in OBJECT_CLASS_NAME_PATTERN.finditer(source):
        literal = OBJECT_LITERAL_CLASS_NAME_PATTERN.match(source, property_key.end())
        if not literal:
            violations.append(
                StudioBundleThemeViolation(
                    bundle_source_file.path,
                    "passes a className object property the check cannot read, for example to createElement or a "
                    "JSX spread; use a string literal of connectorStudioClassNames",
                )
            )
            continue
        classes = next(group for group in literal.group("quoted", "template") if group is not None)
        violations.extend(find_classes_outside_contract(bundle_source_file.path, classes, contract_class_names))
    if OBJECT_SHORTHAND_CLASS_NAME_PATTERN.search(bundle_source_file.code):
        violations.append(
            StudioBundleThemeViolation(
                bundle_source_file.path,
                "passes className as a shorthand object property the check cannot read; "
                "use a string literal of connectorStudioClassNames",
            )
        )
    return violations


def find_classes_outside_contract(
    path: Path, classes: str, contract_class_names: frozenset[str]
) -> list[StudioBundleThemeViolation]:
    return [
        StudioBundleThemeViolation(
            path,
            f'renders class "{class_name}", which is not in the connectorStudioClassNames contract '
            "that the host stylesheet styles",
        )
        for class_name in classes.split()
        if class_name not in contract_class_names
    ]


def find_react_dedupe_violations(user_interface_directory: Path) -> list[StudioBundleThemeViolation]:
    config_paths = [user_interface_directory / name for name in VITE_CONFIG_NAMES if (user_interface_directory / name).is_file()]
    if not config_paths:
        return [StudioBundleThemeViolation(user_interface_directory, "has no vite.config that dedupes react and react-dom")]
    config_path = config_paths[0]
    match = DEDUPE_PATTERN.search(remove_comments(config_path.read_text(encoding="utf-8"), should_keep_string_text=True))
    deduped_packages = set(re.findall(r"""["']([^"']+)["']""", match.group("packages"))) if match else set()
    if not set(DEDUPED_PACKAGES) <= deduped_packages:
        return [
            StudioBundleThemeViolation(
                config_path,
                'must set resolve.dedupe to ["react", "react-dom"] so the bundle has one React for the shared hooks',
            )
        ]
    return []


def find_source_files(source_root: Path) -> list[Path]:
    source_files: list[Path] = []
    for directory, subdirectories, files in os.walk(source_root):
        subdirectories[:] = sorted(name for name in subdirectories if name not in SKIPPED_DIRECTORIES)
        source_files.extend(Path(directory) / name for name in sorted(files) if Path(name).suffix in SOURCE_SUFFIXES)
    return source_files


def find_theme_reexports(bundle_source_files: list[BundleSourceFile]) -> dict[Path, dict[str, str]]:
    """Map each file to the names it re-exports for theme entry points of the shared package."""
    reexports: dict[Path, dict[str, str]] = {}
    for bundle_source_file in bundle_source_files:
        for match in REEXPORT_PATTERN.finditer(bundle_source_file.source):
            for imported_name, local_name in parse_specifiers(match.group("specifiers")):
                if imported_name in THEME_ENTRY_POINTS:
                    reexports.setdefault(bundle_source_file.path, {})[local_name] = imported_name
    return reexports


def find_theme_entry_point_calls(
    bundle_source_file: BundleSourceFile, theme_reexports: dict[Path, dict[str, str]]
) -> list[ThemeEntryPointCall]:
    """Return every call of a theme entry point imported from the shared package or a local re-export."""
    callable_patterns: list[tuple[str, str]] = []
    for match in NAMED_IMPORT_PATTERN.finditer(bundle_source_file.source):
        module = match.group("module")
        if module == SHARED_PACKAGE:
            exported_entry_points = {name: name for name in THEME_ENTRY_POINTS}
        elif module.startswith("."):
            module_path = resolve_relative_module(bundle_source_file.path, module, theme_reexports)
            exported_entry_points = theme_reexports.get(module_path, {}) if module_path else {}
        else:
            continue
        for imported_name, local_name in parse_specifiers(match.group("specifiers")):
            if imported_name in exported_entry_points:
                callable_patterns.append((exported_entry_points[imported_name], re.escape(local_name)))
    for match in NAMESPACE_IMPORT_PATTERN.finditer(bundle_source_file.source):
        namespace = re.escape(match.group("namespace"))
        callable_patterns.extend((name, rf"{namespace}\s*\.\s*{name}") for name in THEME_ENTRY_POINTS)
    code = bundle_source_file.code
    calls: list[ThemeEntryPointCall] = []
    for entry_point, callable_pattern in callable_patterns:
        for call in re.finditer(rf"(?<![\w$.]){callable_pattern}\s*\(", code):
            is_applied_from_ready_effect = entry_point == MODEL_PICKER_ENTRY_POINT or is_called_from_ready_effect(
                code, call.start(), call.end() - 1
            )
            calls.append(ThemeEntryPointCall(entry_point, is_applied_from_ready_effect))
    return calls


def parse_specifiers(specifiers: str) -> list[tuple[str, str]]:
    """Return (imported name, local name) pairs from an import or export specifier list, skipping types."""
    names: list[tuple[str, str]] = []
    for specifier in specifiers.split(","):
        parts = specifier.split()
        if not parts or parts[0] == "type":
            continue
        if len(parts) == 3 and parts[1] == "as":
            names.append((parts[0], parts[2]))
        elif len(parts) == 1:
            names.append((parts[0], parts[0]))
    return names


def resolve_relative_module(importer: Path, module: str, theme_reexports: dict[Path, dict[str, str]]) -> Path | None:
    base = Path(os.path.normpath(importer.parent / module))
    stems = [base, base.with_suffix("")] if base.suffix in SOURCE_SUFFIXES else [base]
    candidates = [base] + [stem.with_name(stem.name + suffix) for stem in stems for suffix in sorted(SOURCE_SUFFIXES)]
    candidates += [base / f"index{suffix}" for suffix in sorted(SOURCE_SUFFIXES)]
    return next((candidate for candidate in candidates if candidate in theme_reexports), None)


def is_called_from_ready_effect(code: str, call_start: int, call_open_paren: int) -> bool:
    """Report whether a theme call passes an argument inside an effect callback whose dependencies list it."""
    call_arguments = find_call_argument_spans(code, call_open_paren)
    if not call_arguments:
        return False
    host_ready = code[call_arguments[0][0] : call_arguments[0][1]].strip().rstrip("!").strip()
    if not host_ready:
        return False
    for effect in EFFECT_CALL_PATTERN.finditer(code, 0, call_start):
        effect_arguments = find_call_argument_spans(code, effect.end() - 1)
        if not effect_arguments or len(effect_arguments) < 2:
            continue
        (callback_start, callback_end), (dependencies_start, dependencies_end) = effect_arguments[:2]
        dependencies = code[dependencies_start:dependencies_end].strip()
        if not (callback_start <= call_start < callback_end and dependencies.startswith("[") and dependencies.endswith("]")):
            continue
        if host_ready in {dependency.strip() for dependency in dependencies[1:-1].split(",")}:
            return True
    return False


def find_call_argument_spans(code: str, open_paren: int) -> list[tuple[int, int]] | None:
    """Return the spans of a call's top-level arguments, or None when its brackets do not balance."""
    closing_stack = [")"]
    spans: list[tuple[int, int]] = []
    argument_start = open_paren + 1
    for index in range(open_paren + 1, len(code)):
        character = code[index]
        if character in OPENING_BRACKETS:
            closing_stack.append(OPENING_BRACKETS[character])
        elif character in ")]}":
            if character != closing_stack.pop():
                return None
            if not closing_stack:
                if code[argument_start:index].strip():
                    spans.append((argument_start, index))
                return spans
        elif character == "," and len(closing_stack) == 1:
            spans.append((argument_start, index))
            argument_start = index + 1
    return None


def remove_comments(source: str, should_keep_string_text: bool) -> str:
    """Remove // and /* */ comments, and string and template literal text unless it is kept."""
    kept: list[str] = []
    index = 0
    length = len(source)
    while index < length:
        character = source[index]
        following = source[index + 1] if index + 1 < length else ""
        if character in "\"'`":
            # Quotes stop at a newline so an apostrophe in JSX text cannot swallow later lines.
            end = index + 1
            while end < length and source[end] != character and (character == "`" or source[end] != "\n"):
                end += 2 if source[end] == "\\" else 1
            kept.append(source[index : end + 1] if should_keep_string_text else character + source[end : end + 1])
            index = end + 1
        elif character == "/" and following == "*":
            end = source.find("*/", index + 2)
            index = length if end == -1 else end + 2
            kept.append(" ")
        elif character == "/" and following == "/" and (index == 0 or source[index - 1] != ":"):
            end = source.find("\n", index)
            index = length if end == -1 else end
        else:
            kept.append(character)
            index += 1
    return "".join(kept)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
