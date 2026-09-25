"""Validate the Phase 3 ZIP against the structural OKF v0.2 rules we emit."""

import re
import sys
import zipfile
from pathlib import Path

import yaml


def fail(message: str) -> None:
    raise SystemExit(message)


def frontmatter(content: str, name: str) -> tuple[dict, str]:
    if not content.startswith("---\n"):
        fail(f"{name}: missing frontmatter")
    _, separator, rest = content[4:].partition("\n---\n")
    if not separator:
        fail(f"{name}: unclosed frontmatter")
    header, _, body = content[4:].partition("\n---\n")
    data = yaml.safe_load(header)
    if not isinstance(data, dict):
        fail(f"{name}: invalid frontmatter mapping")
    return data, body


def validate(bundle: Path) -> None:
    with zipfile.ZipFile(bundle) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)) or "index.md" not in names:
            fail("OKF ZIP has duplicate names or no root index")
        concepts = [name for name in names if name.endswith(".md") and name != "index.md"]
        if not concepts:
            fail("OKF ZIP has no concepts")
        for name in names:
            if name.startswith("/") or "\\" in name or any(part in ("", ".", "..") for part in name.split("/")):
                fail(f"unsafe ZIP path: {name}")
        root, _ = frontmatter(archive.read("index.md").decode("utf-8"), "index.md")
        if str(root.get("okf_version")) != "0.2":
            fail("root index does not declare OKF v0.2")
        for name in concepts:
            data, body = frontmatter(archive.read(name).decode("utf-8"), name)
            if not isinstance(data.get("type"), str) or not data["type"].strip():
                fail(f"{name}: missing concept type")
            if "verified" in data:
                fail(f"{name}: unearned verification event")
            sources = data.get("sources", [])
            if not isinstance(sources, list):
                fail(f"{name}: sources must be a list")
            source_ids = set()
            for source in sources:
                if not isinstance(source, dict) or not source.get("resource") or not source.get("id"):
                    fail(f"{name}: invalid source entry")
                if source["id"] in source_ids:
                    fail(f"{name}: duplicate source ID")
                source_ids.add(source["id"])
            used = set(re.findall(r"\[\^([A-Za-z0-9_-]+)\](?!:)", body))
            defined = set(re.findall(r"(?m)^\[\^([A-Za-z0-9_-]+)\]:", body))
            if used != source_ids or defined != source_ids:
                fail(f"{name}: footnotes do not match sources")
        print(f"Validated OKF v0.2 structure: {len(concepts)} concepts")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        fail("usage: python scripts/validate_okf.py <bundle.zip>")
    validate(Path(sys.argv[1]))
