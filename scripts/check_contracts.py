"""Offline checks for API/schema contracts and synthetic evidence fixtures.

Requires Python 3.11+ and PyYAML 6.0.3. No API key or network connection is used.
This checks syntax and internal references. Full JSON Schema/OpenAPI conformance
validation will be added when generated server/client bindings are introduced.
"""

from __future__ import annotations

import json
import re
import os
from pathlib import Path
from typing import Any

import yaml


ROOT = Path(__file__).resolve().parents[1]
SCHEMAS = sorted((ROOT / "contracts").glob("*.schema.json"))


class UniqueKeyLoader(yaml.SafeLoader):
    pass


def unique_mapping(loader: UniqueKeyLoader, node: yaml.MappingNode) -> dict[Any, Any]:
    result: dict[Any, Any] = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node)
        if key in result:
            raise AssertionError(f"duplicate YAML key: {key!r}")
        result[key] = loader.construct_object(value_node)
    return result


UniqueKeyLoader.add_constructor(
    yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping
)


def fail(message: str) -> None:
    raise AssertionError(message)


def walk(value: Any):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def resolve_ref(owner: Path, ref: str, loaded: dict[Path, Any]) -> None:
    path_part, _, fragment = ref.partition("#")
    target = (owner.parent / path_part).resolve() if path_part else owner.resolve()
    if not target.is_relative_to(ROOT):
        fail(f"reference leaves repo: {owner}: {ref}")
    if not target.is_file():
        fail(f"missing reference file: {owner}: {ref}")
    if target not in loaded:
        loaded[target] = (
            json.loads(target.read_text(encoding="utf-8"))
            if target.suffix == ".json"
            else yaml.load(target.read_text(encoding="utf-8"), Loader=UniqueKeyLoader)
        )
    current: Any = loaded[target]
    if fragment:
        if not fragment.startswith("/"):
            fail(f"unsupported reference fragment: {owner}: {ref}")
        for raw_token in fragment[1:].split("/"):
            token = raw_token.replace("~1", "/").replace("~0", "~")
            if isinstance(current, dict) and token in current:
                current = current[token]
            elif isinstance(current, list) and token.isdigit() and int(token) < len(current):
                current = current[int(token)]
            else:
                fail(f"unresolved pointer: {owner}: {ref}")


def check_schemas_and_api() -> None:
    if len(SCHEMAS) < 4:
        fail("expected repository, evidence, analysis, and prompt schemas")
    loaded: dict[Path, Any] = {}
    for path in SCHEMAS:
        doc = json.loads(path.read_text(encoding="utf-8"))
        if doc.get("$schema") != "https://json-schema.org/draft/2020-12/schema":
            fail(f"unexpected schema dialect: {path}")
        if not doc.get("$defs") and not (doc.get("type") == "object" and doc.get("required")):
            fail(f"schema has neither definitions nor a required root object: {path}")
        loaded[path.resolve()] = doc

    api_path = (ROOT / "contracts" / "openapi.yaml").resolve()
    api = yaml.load(api_path.read_text(encoding="utf-8"), Loader=UniqueKeyLoader)
    if api.get("openapi") != "3.1.0" or not api.get("paths"):
        fail("OpenAPI 3.1 paths missing")
    loaded[api_path] = api
    for route, methods in api["paths"].items():
        if not route.startswith("/"):
            fail(f"invalid route: {route}")
        for method, operation in methods.items():
            if method not in {"get", "post", "delete", "put", "patch"}:
                fail(f"unexpected operation key: {route}: {method}")
            if not operation.get("operationId") or not operation.get("responses"):
                fail(f"operation incomplete: {route}: {method}")

    for owner, document in list(loaded.items()):
        for node in walk(document):
            ref = node.get("$ref")
            if isinstance(ref, str):
                resolve_ref(owner, ref, loaded)
    print(f"Validated {len(SCHEMAS)} JSON schemas and all OpenAPI references")


def check_fixture_cases() -> None:
    path = ROOT / "testdata" / "evals" / "cases.json"
    suite = json.loads(path.read_text(encoding="utf-8"))
    cases = suite.get("cases", [])
    if len(cases) < 8:
        fail("expected at least eight evaluation cases")
    ids: set[str] = set()
    for case in cases:
        case_id = case["id"]
        if case_id in ids:
            fail(f"duplicate evaluation ID: {case_id}")
        ids.add(case_id)
        fixture = (ROOT / case["fixture"]).resolve()
        if not fixture.is_dir() or not fixture.is_relative_to(ROOT / "testdata"):
            fail(f"invalid fixture for {case_id}")
        if not case.get("required_findings") or not case.get("forbidden_claims"):
            fail(f"missing rubric for {case_id}")
        for anchor in case["required_anchors"]:
            source = (fixture / anchor["path"]).resolve()
            if not source.is_file() or not source.is_relative_to(fixture):
                fail(f"missing evidence file: {case_id}: {anchor['path']}")
            lines = source.read_text(encoding="utf-8").splitlines()
            line = anchor.get("line")
            if not isinstance(line, int) or not (1 <= line <= len(lines)):
                fail(f"invalid evidence line: {case_id}: {anchor['path']}")
            if anchor["contains"] not in lines[line - 1]:
                fail(f"missing evidence text at frozen line: {case_id}: {anchor['path']}:{line}")
        for relative in case.get("excluded_paths", []):
            excluded = (fixture / relative).resolve()
            if not excluded.is_file() or not excluded.is_relative_to(fixture):
                fail(f"missing excluded fixture: {case_id}: {relative}")
    print(f"Validated {len(cases)} evaluation cases and source anchors")


def check_prompts() -> None:
    import hashlib
    registry_path = ROOT / "prompts" / "registry.json"
    registry = json.loads(registry_path.read_text(encoding="utf-8"))
    entries = [(registry["agent_template_ref"], registry["agent_checksum"])]
    entries += [(p["template_ref"], p["checksum"]) for p in registry["prompts"]]
    for relative, expected in entries:
        template = (registry_path.parent / relative).resolve()
        if not template.is_relative_to(ROOT / "prompts" / "templates"):
            fail(f"invalid template reference: {relative}")
        if hashlib.sha256(template.read_bytes()).hexdigest() != expected:
            fail(f"prompt checksum mismatch: {relative}")
    canary = "SYNTHETIC_CANARY_NOT_A_CREDENTIAL"
    matches = []
    excluded = {".git", "work", "secrets", "data", "node_modules", "dist", "build", ".cache", "__pycache__"}
    candidates = []
    for directory, dirs, files in os.walk(ROOT):
        dirs[:] = [name for name in dirs if name not in excluded]
        candidates.extend(Path(directory) / name for name in files)
    for path in candidates:
        if path.is_file() and ".git" not in path.parts and path.suffix in {
            ".md", ".json", ".yaml", ".py", ".go", ".ts", ".synthetic"
        }:
            if canary in path.read_text(encoding="utf-8"):
                matches.append(path.relative_to(ROOT).as_posix())
    allowed = {
        "testdata/mixed-monolith/deploy/.env.synthetic",
        "scripts/check_contracts.py",
        "internal/privacy/privacy.go",
        "internal/privacy/privacy_test.go",
    }
    if set(matches) != allowed:
        fail(f"synthetic marker occurred in unexpected files: {matches}")
    print("Validated prompt checksums and canary placement")


def check_markdown_links() -> None:
    markdown_files = [ROOT / "README.md"]
    markdown_files.extend((ROOT / "docs").rglob("*.md"))
    markdown_files.extend((ROOT / "prompts").rglob("*.md"))
    checked = 0
    for owner in markdown_files:
        for target in re.findall(r"(?<!!)\[[^\]]+\]\(([^)]+)\)", owner.read_text(encoding="utf-8")):
            if target.startswith(("https://", "http://", "mailto:", "#")):
                continue
            path_part = target.split("#", 1)[0]
            destination = (owner.parent / path_part).resolve()
            if not destination.is_relative_to(ROOT) or not destination.exists():
                fail(f"broken local Markdown link: {owner.relative_to(ROOT)} -> {target}")
            checked += 1
    print(f"Validated {checked} local Markdown links")


if __name__ == "__main__":
    check_schemas_and_api()
    check_fixture_cases()
    check_prompts()
    check_markdown_links()
    print("Offline contract checks passed")
