# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay shared connector scenarios only for directly changed connectors."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
NATIVE_WIRE_CONNECTORS = frozenset({
    "connectors/postgresql", "connectors/mysql", "connectors/microsoft/sql-server",
    "connectors/superdurable/email",
})


def conformance_exemptions() -> dict[str, str]:
    """Load reviewed native-wire exemptions; never exempt an HTTP connector."""
    exemption_file = ROOT / "conformance/exemptions.yaml"
    document = json.loads(exemption_file.read_text())
    entries = document.get("exemptions")
    if document.get("schemaVersion") != 1 or not isinstance(entries, list):
        raise ValueError("conformance/exemptions.yaml has an invalid schema")
    exemptions: dict[str, str] = {}
    for entry in entries:
        if not isinstance(entry, dict) or set(entry) != {"connector", "reason"}:
            raise ValueError("conformance exemption needs connector and reason")
        connector, reason = entry["connector"], entry["reason"]
        if connector not in NATIVE_WIRE_CONNECTORS:
            raise ValueError(f"{connector} is not an approved native-wire connector")
        if not isinstance(reason, str) or not reason.strip():
            raise ValueError(f"{connector} has no exemption reason")
        if connector in exemptions:
            raise ValueError(f"{connector} repeats a conformance exemption")
        if not (ROOT / connector / "connector.yaml").is_file():
            raise ValueError(f"{connector} is not a connector")
        exemptions[connector] = reason
    return exemptions


def database_ci_matrix(paths: list[str]) -> list[dict[str, str]]:
    """Select exempt connectors that have a Python or TypeScript package to test."""
    images = {
        "connectors/postgresql": ("postgres:17", "5432"),
        "connectors/mysql": ("mysql:8.4", "3306"),
        "connectors/microsoft/sql-server": ("mcr.microsoft.com/mssql/server:2022-latest", "1433"),
    }
    shared_change = any(path in {"conformance/exemptions.yaml", "script/connector_conformance.py",
                                 ".github/workflows/ci.yml"} for path in paths)
    matrix = []
    for connector in conformance_exemptions():
        if connector not in images:
            continue
        if paths and not shared_change and not any(path.startswith(connector + "/") for path in paths):
            continue
        if not any((ROOT / connector / language / package).is_file() for language, package in (
            ("python", "pyproject.toml"), ("typescript", "package.json")
        )):
            continue
        image, port = images[connector]
        matrix.append({"connector": connector, "image": image, "port": port})
    return matrix


def email_ci_matrix(paths: list[str]) -> list[dict[str, str]]:
    """Select the exempt email connector when it has a language package to test."""
    connector = "connectors/superdurable/email"
    shared_change = any(path in {"conformance/exemptions.yaml", "script/connector_conformance.py",
                                 ".github/workflows/ci.yml"} for path in paths)
    if paths and not shared_change and not any(path.startswith(connector + "/") for path in paths):
        return []
    if connector not in conformance_exemptions():
        return []
    if not any((ROOT / connector / language / package).is_file() for language, package in (
        ("python", "pyproject.toml"), ("typescript", "package.json")
    )):
        return []
    return [{"connector": connector}]


def connector_for_path(path: str) -> Path | None:
    """Find the registered connector owning one source or conformance path."""
    if path.startswith("conformance/connectors/"):
        path = "connectors/" + path.removeprefix("conformance/connectors/")
    if not path.startswith("connectors/"):
        return None
    current = (ROOT / path).parent
    while current != ROOT and ROOT in current.parents:
        if (current / "connector.yaml").is_file():
            return current
        current = current.parent
    return None


def changed_paths(base: str, head: str) -> list[str]:
    """Return paths changed since the merge base, including fixture additions."""
    merge_base = subprocess.check_output(
        ["git", "merge-base", base, head], cwd=ROOT, text=True
    ).strip()
    names = subprocess.check_output(
        ["git", "diff", "--name-only", "--no-renames", "-z", merge_base, head], cwd=ROOT
    )
    return [raw.decode() for raw in names.split(b"\0") if raw]


def changed_connectors(paths: list[str]) -> list[Path]:
    """Select directly changed connectors and fixtures when shared runners change."""
    connectors = {connector for path in paths
                  if (connector := connector_for_path(path)) is not None}
    if any(path.startswith("conformance/runners/") or path == "script/connector_conformance.py"
           for path in paths):
        connectors.update(ROOT / "connectors" / fixture.relative_to(ROOT / "conformance/connectors").parent
                          for fixture in (ROOT / "conformance/connectors").rglob("scenarios.json"))
    return sorted(connectors)


def run_connector(connector: Path, paths: list[str], exemptions: dict[str, str] | None = None) -> None:
    """Require each language runner and execute Go, Python, and TypeScript replay."""
    python_package = connector / "python"
    typescript_package = connector / "typescript"
    relative = connector.relative_to(ROOT / "connectors")
    exemption = (exemptions if exemptions is not None else conformance_exemptions()).get(
        str(connector.relative_to(ROOT)))
    if exemption is not None:
        print(f"{connector.relative_to(ROOT)}: exempt ({exemption})", flush=True)
        return
    fixture = ROOT / "conformance/connectors" / relative / "scenarios.json"
    if not fixture.is_file():
        prefix = str(connector.relative_to(ROOT)) + "/"
        fixture_prefix = "conformance/connectors/" + str(relative) + "/"
        if any(path.startswith(prefix + "python/") or path.startswith(prefix + "typescript/")
               or path.startswith(fixture_prefix) for path in paths):
            raise ValueError(f"{connector.relative_to(ROOT)} has no conformance scenarios")
        if (python_package / "pyproject.toml").is_file() or (typescript_package / "package.json").is_file():
            print(f"warning: {connector.relative_to(ROOT)} missing conformance (legacy)", file=sys.stderr)
        return
    document = json.loads(fixture.read_text())
    if document.get("schemaVersion") != 1 or not document.get("scenarios"):
        raise ValueError(f"{fixture.relative_to(ROOT)} has no supported scenarios")
    names = [scenario["name"] for scenario in document["scenarios"]]
    if len(names) != len(set(names)):
        raise ValueError(f"{fixture.relative_to(ROOT)} repeats a scenario name")
    operation_output = subprocess.check_output(
        ["go", "run", "./cmd/connectorctl", "conformance-operations", str(connector / "connector.yaml")],
        cwd=ROOT, env={**os.environ, "GOWORK": "off"}, text=True,
    )
    declared_operations: dict[str, dict[str, object]] = json.loads(operation_output)
    observed_branches: dict[str, set[str]] = {}
    observed_rate_limits: set[str] = set()
    observed_server_errors: set[str] = set()
    observed_transport_failures: set[str] = set()
    observed_uncertainty: set[str] = set()
    for scenario in document["scenarios"]:
        if "pending" in scenario:
            raise ValueError(f"{scenario['name']} uses unsupported pending scenario")
        operation = scenario["operation"]
        expected = scenario["expected"]
        if scenario.get("attempts", 1) != len(scenario.get("intermediate", [])) + 1:
            raise ValueError(f"{scenario['name']} has mismatched attempts and intermediate retries")
        provider_requests = sum(exchange.get("transportFailure") != "before-send"
                                for exchange in scenario["exchanges"])
        if expected["requestCount"] != provider_requests:
            raise ValueError(f"{scenario['name']} has a wrong request count")
        if not scenario.get("callId"):
            raise ValueError(f"{scenario['name']} has no Call ID")
        if operation not in declared_operations:
            raise ValueError(f"{fixture.relative_to(ROOT)} names unknown operation {operation}")
        if declared_operations[operation]["kind"] == "mutation":
            if not scenario.get("idempotencyKey"):
                raise ValueError(f"{scenario['name']} needs a recorded production idempotency key")
        elif scenario.get("idempotencyKey"):
            raise ValueError(f"{scenario['name']} is a query with an idempotency key")
        if any("transportFailure" in exchange for exchange in scenario["exchanges"]):
            observed_transport_failures.add(operation)
        branch = expected["branch"]
        final_exchange_index = scenario.get("intermediate", [{}])[-1].get("exchangeCount", 0)
        if not isinstance(final_exchange_index, int) or final_exchange_index < 0 or (
            scenario["exchanges"] and final_exchange_index >= len(scenario["exchanges"])
        ):
            raise ValueError(f"{scenario['name']} has invalid final attempt exchange count")
        classified_status = (scenario["exchanges"][final_exchange_index].get("status", 0)
                             if scenario["exchanges"] else 0)
        if expected["retry"]:
            if branch:
                raise ValueError(f"{scenario['name']} has both retry and branch")
            if classified_status == 429:
                observed_rate_limits.add(operation)
            if 500 <= classified_status <= 599:
                observed_server_errors.add(operation)
        elif branch not in declared_operations[operation]["branches"]:
            raise ValueError(f"{scenario['name']} names undeclared branch {branch}")
        if not expected["retry"] and "output" not in expected:
            raise ValueError(f"{scenario['name']} has no recorded output")
        if expected["uncertain"]:
            observed_uncertainty.add(operation)
        observed_branches.setdefault(operation, set()).add(branch)
    for operation, declaration in declared_operations.items():
        if operation not in observed_transport_failures:
            raise ValueError(f"{fixture.relative_to(ROOT)} lacks {operation} transport failure coverage")
        if operation not in observed_rate_limits or operation not in observed_server_errors:
            raise ValueError(f"{fixture.relative_to(ROOT)} lacks {operation} separate 429 and 5xx Retry coverage")
        if "uncertain" in declaration["branches"] and operation not in observed_uncertainty:
            raise ValueError(f"{fixture.relative_to(ROOT)} lacks {operation} uncertainty coverage")
    for language, package_file, runner_path in (
        ("python", "pyproject.toml", "tests/test_conformance.py"),
        ("typescript", "package.json", "test/conformance.test.ts"),
    ):
        language_package = connector / language
        if not (language_package / package_file).is_file():
            continue
        if not (language_package / runner_path).is_file():
            raise ValueError(f"{language_package.relative_to(ROOT)} has no conformance runner")
        capability_output = subprocess.check_output(
            ["go", "run", "./cmd/connectorctl", "language-capabilities",
             str(connector / "connector.yaml"), language],
            cwd=ROOT, env={**os.environ, "GOWORK": "off"}, text=True,
        )
        for operation in json.loads(capability_output)["operations"]:
            missing = set(declared_operations[operation]["branches"]) - observed_branches.get(operation, set())
            if missing:
                raise ValueError(f"{fixture.relative_to(ROOT)} lacks {operation} branches: {sorted(missing)}")
    go_test = connector / "go/zz_generated_conformance_test.go"
    if not go_test.is_file():
        raise ValueError(f"{connector.relative_to(ROOT)} has no Go reference test")
    subprocess.run(
        ["go", "test", "./...", "-run", "^TestConnectorConformance$", "-count=1"],
        cwd=connector / "go", env={**os.environ, "GOWORK": "off"}, check=True
    )
    if (python_package / "pyproject.toml").is_file():
        runner = python_package / "tests/test_conformance.py"
        subprocess.run(["uv", "sync", "--locked", "--project", str(python_package)], cwd=ROOT, check=True)
        subprocess.run(["uv", "run", "--project", str(python_package), "pytest", str(runner)], cwd=ROOT, check=True)
    if (typescript_package / "package.json").is_file():
        runner = typescript_package / "test/conformance.test.ts"
        subprocess.run(["npm", "ci", "--prefix", str(typescript_package)], cwd=ROOT, check=True)
        subprocess.run(["npm", "run", "build", "--prefix", str(typescript_package)], cwd=ROOT, check=True)
        subprocess.run([str(typescript_package / "node_modules/.bin/vitest"), "run", "test/conformance.test.ts"],
                       cwd=typescript_package, check=True)


def mutation_self_test(connectors: list[Path]) -> None:
    """Prove recordings and all port runners reject representative weak expectations."""
    for connector in connectors:
        relative = connector.relative_to(ROOT / "connectors")
        fixture_path = ROOT / "conformance/connectors" / relative / "scenarios.json"
        if not fixture_path.is_file():
            continue
        original = json.loads(fixture_path.read_text())
        operation_output = subprocess.check_output(
            ["go", "run", "./cmd/connectorctl", "conformance-operations", str(connector / "connector.yaml")],
            cwd=ROOT, env={**os.environ, "GOWORK": "off"}, text=True,
        )
        declared_operations = json.loads(operation_output)
        scenario = next((item for item in original["scenarios"]
                         if item["exchanges"] and not item["expected"]["retry"]), None)
        if scenario is None:
            raise ValueError(f"{relative} has no completed provider scenario to mutate")
        commands = {"go": (["go", "test", ".", "-run", "^TestConnectorConformance$", "-count=1"],
                           connector / "go")}
        python_package = connector / "python"
        if (python_package / "pyproject.toml").is_file():
            subprocess.run(["uv", "sync", "--locked", "--project", str(python_package)], cwd=ROOT, check=True)
            commands["python"] = (["uv", "run", "--project", str(python_package), "pytest",
                                    str(python_package / "tests/test_conformance.py"), "-q"], ROOT)
        typescript_package = connector / "typescript"
        if (typescript_package / "package.json").is_file():
            subprocess.run(["npm", "ci", "--prefix", str(typescript_package)], cwd=ROOT, check=True)
            subprocess.run(["npm", "run", "build", "--prefix", str(typescript_package)], cwd=ROOT, check=True)
            commands["typescript"] = ([str(typescript_package / "node_modules/.bin/vitest"), "run",
                                        "test/conformance.test.ts"], typescript_package)
        mutation_sources = [("classification", scenario), ("exchange", scenario),
                            ("wrong-output-field", scenario), ("pending-passing-go", scenario)]
        list_output_scenario = next((item for item in original["scenarios"]
                                     if isinstance(item["expected"].get("output"), dict)
                                     and any(isinstance(value, list)
                                             for value in item["expected"]["output"].values())), None)
        query_scenario = next((item for item in original["scenarios"]
                               if any(exchange.get("query") for exchange in item["exchanges"])), None)
        optional_query_scenario = next((item for item in original["scenarios"]
                                        if any(field in item["input"] and field in exchange.get("query", {})
                                               for field in declared_operations[item["operation"]]["optionalInputFields"]
                                               for exchange in item["exchanges"])), None)
        form_scenario = next((item for item in original["scenarios"]
                              if any(exchange.get("form") for exchange in item["exchanges"])), None)
        read_server_error = next((item for item in original["scenarios"] if item["expected"]["retry"]
                                  and declared_operations[item["operation"]]["kind"] == "query"
                                  and any(500 <= exchange.get("status", 0) <= 599
                                          for exchange in item["exchanges"])), None)
        mime_scenario = next((item for item in original["scenarios"]
                              if any("$rawMime" in exchange.get("requestBody", {})
                                      for exchange in item["exchanges"])), None)
        keyed_scenario = next((item for item in original["scenarios"]
                               if item.get("idempotencyKey")), None)
        heartbeat_scenario = next((item for item in original["scenarios"]
                                   if any("heartbeat" in intermediate
                                          for intermediate in item.get("intermediate", []))), None)
        final_heartbeat_scenario = next((item for item in original["scenarios"]
                                         if "heartbeat" in item["expected"]), None)
        no_heartbeat_scenario = next((item for item in original["scenarios"]
                                      if declared_operations[item["operation"]]["kind"] == "mutation"
                                      and "priorHeartbeat" in item and "heartbeat" not in item["expected"]), None)
        mutation_sources.extend((name, source) for name, source in (
            ("dropped-query", query_scenario), ("renamed-query", query_scenario),
            ("renamed-optional-query", optional_query_scenario),
            ("changed-form-value", form_scenario), ("read-5xx-classification", read_server_error),
            ("mime-subject", mime_scenario), ("mime-recipient", mime_scenario),
            ("mime-body", mime_scenario), ("random-idempotency-key", keyed_scenario),
            ("wrong-nonempty-list", list_output_scenario),
            ("wrong-heartbeat-shape", heartbeat_scenario),
            ("port-heartbeat-shape", heartbeat_scenario),
            ("wrong-final-heartbeat-shape", final_heartbeat_scenario),
            ("extra-heartbeat", no_heartbeat_scenario),
        ) if source is not None)
        for mutation, source in mutation_sources:
            changed = json.loads(json.dumps(source))
            marker = {
                "classification": "__wrong_conformance_branch__",
                "exchange": "/wrong-conformance-path",
                "renamed-query": "__wrong_conformance_query__",
                "changed-form-value": "__wrong_conformance_form__",
                "read-5xx-classification": "__wrong_read_5xx_branch__",
                "wrong-output-field": "__wrong_output_field__",
                "mime-subject": "__wrong_mime_subject__",
                "mime-recipient": "__wrong_mime_recipient__",
                "mime-body": "__wrong_mime_body__",
            }.get(mutation, "unasserted query")
            if mutation in ("classification", "read-5xx-classification"):
                changed["expected"]["branch"] = marker
                if mutation == "read-5xx-classification":
                    changed["expected"]["retry"] = False
            elif mutation == "exchange":
                changed["exchanges"][0]["path"] = marker
            elif mutation in ("dropped-query", "renamed-query"):
                exchange = next(exchange for exchange in changed["exchanges"] if exchange.get("query"))
                key, value = next(iter(exchange["query"].items()))
                del exchange["query"][key]
                if mutation == "renamed-query":
                    exchange["query"][marker] = value
            elif mutation == "renamed-optional-query":
                optional_fields = declared_operations[changed["operation"]]["optionalInputFields"]
                exchange = next(exchange for exchange in changed["exchanges"]
                                if any(field in changed["input"] and field in exchange.get("query", {})
                                       for field in optional_fields))
                field = next(field for field in optional_fields if field in changed["input"]
                             and field in exchange["query"])
                renamed = re.sub(r"(?<!^)(?=[A-Z])", "_", field).lower()
                exchange["query"][renamed if renamed != field else field + "_renamed"] = exchange["query"].pop(field)
            elif mutation == "wrong-output-field":
                changed["expected"]["output"][marker] = "wrong"
            elif mutation == "wrong-nonempty-list":
                key = next(key for key, value in changed["expected"]["output"].items()
                           if isinstance(value, list))
                changed["expected"]["output"][key] = ["__wrong_nonempty_list__"]
            elif mutation == "wrong-heartbeat-shape":
                intermediate = next(value for value in changed["intermediate"] if "heartbeat" in value)
                intermediate["heartbeat"] = {"__wrong_heartbeat_shape__": True}
            elif mutation == "wrong-final-heartbeat-shape":
                changed["expected"]["heartbeat"] = {"__wrong_heartbeat_shape__": True}
            elif mutation == "port-heartbeat-shape":
                pass
            elif mutation == "extra-heartbeat":
                pass
            elif mutation.startswith("mime-"):
                exchange = next(exchange for exchange in changed["exchanges"]
                                if "$rawMime" in exchange.get("requestBody", {}))
                mime = exchange["requestBody"]["$rawMime"]
                if mutation == "mime-subject":
                    exchange["requestBody"]["$rawMime"] = re.sub(r"Subject: [^\r\n]+", "Subject: " + marker, mime, count=1)
                elif mutation == "mime-recipient":
                    exchange["requestBody"]["$rawMime"] = re.sub(r"To: [^\r\n]+", "To: " + marker, mime, count=1)
                else:
                    exchange["requestBody"]["$rawMime"] = mime.replace("Update", marker, 1)
            elif mutation == "random-idempotency-key":
                changed["idempotencyKey"] = "random-00000000-0000-4000-8000-000000000000"
            elif mutation == "pending-passing-go":
                changed["pending"] = True
            else:
                exchange = next(exchange for exchange in changed["exchanges"] if exchange.get("form"))
                key = next(iter(exchange["form"]))
                exchange["form"][key] = marker
            document = {**original, "scenarios": [changed if item["name"] == source["name"] else item
                                                 for item in original["scenarios"]]}
            with tempfile.TemporaryDirectory(prefix="connector-conformance-") as directory:
                fixture = Path(directory) / "scenarios.json"
                fixture.write_text(json.dumps(document))
                selected_commands = ({language: value for language, value in commands.items() if language != "go"}
                                     if mutation in ("port-heartbeat-shape", "extra-heartbeat") else commands)
                for language, (command, cwd) in selected_commands.items():
                    result = subprocess.run(command, cwd=cwd, text=True, capture_output=True, check=False,
                                            env={**os.environ, "GOWORK": "off",
                                                 "CONNECTOR_CONFORMANCE_FIXTURE": str(fixture),
                                                 **({"CONNECTOR_CONFORMANCE_MUTATE_HEARTBEAT": "1"}
                                                    if mutation == "port-heartbeat-shape" else {}),
                                                 **({"CONNECTOR_CONFORMANCE_MUTATE_EXTRA_HEARTBEAT": changed["name"]}
                                                    if mutation == "extra-heartbeat" else {})})
                    if result.returncode == 0:
                        raise AssertionError(f"{relative} {language} runner accepted {mutation} mutation")
                    if mutation != "pending-passing-go" and changed["name"] not in result.stdout + result.stderr:
                        raise AssertionError(f"{relative} {language} runner did not exercise {changed['name']}")
                    print(f"Mutation {mutation}: {relative} {language} runner rejected fixture", flush=True)


def main() -> None:
    """Run changed connectors, or explicitly named local connector directories."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default=os.environ.get("CONNECTOR_BASE"))
    parser.add_argument("--head", default=os.environ.get("CONNECTOR_HEAD", "HEAD"))
    parser.add_argument("--connector", action="append", default=[])
    parser.add_argument("--mutation-self-test", action="store_true")
    parser.add_argument("--database-ci-matrix", action="store_true")
    parser.add_argument("--email-ci-matrix", action="store_true")
    arguments = parser.parse_args()
    exemptions = conformance_exemptions()
    if arguments.database_ci_matrix:
        paths = changed_paths(arguments.base, arguments.head) if arguments.base else []
        print(json.dumps({"include": database_ci_matrix(paths)}))
        return
    if arguments.email_ci_matrix:
        paths = changed_paths(arguments.base, arguments.head) if arguments.base else []
        print(json.dumps({"include": email_ci_matrix(paths)}))
        return
    if arguments.mutation_self_test:
        connectors = changed_connectors(changed_paths(arguments.base, arguments.head)) if arguments.base else sorted(
            ROOT / "connectors" / fixture.relative_to(ROOT / "conformance/connectors").parent
            for fixture in (ROOT / "conformance/connectors").rglob("scenarios.json")
        )
        mutation_self_test([connector for connector in connectors
                            if str(connector.relative_to(ROOT)) not in exemptions])
        return
    if arguments.connector:
        connectors = sorted({(ROOT / path).resolve() for path in arguments.connector})
        paths = []
    elif arguments.base:
        paths = changed_paths(arguments.base, arguments.head)
        connectors = changed_connectors(paths)
    else:
        parser.error("supply --base or --connector")
    for connector in connectors:
        if not (connector / "connector.yaml").is_file():
            parser.error(f"{connector} is not a connector directory")
        print(f"Conformance: {connector.relative_to(ROOT)}", flush=True)
        run_connector(connector, paths, exemptions)


if __name__ == "__main__":
    main()
