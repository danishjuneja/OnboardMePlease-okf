"""Regenerate checked-in prompt templates and checksums from the baseline library."""

import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LIBRARY = ROOT / "docs" / "prompt-library.md"
TEMPLATES = ROOT / "prompts" / "templates"
REGISTRY = ROOT / "prompts" / "registry.json"


def main() -> None:
    source = LIBRARY.read_text(encoding="utf-8")
    sections = re.findall(r"(?ms)^## ((?:A|P)\d\d) — ([^\n]+)\n(.*?)(?=^## |\Z)", source)
    if len(sections) != 13:
        raise SystemExit(f"Expected A00 and P00-P11, found {len(sections)} sections")
    TEMPLATES.mkdir(exist_ok=True)
    entries = []
    agent_checksum = None
    for identifier, purpose, section in sections:
        match = re.search(r"```text\n(.*?)\n```", section, flags=re.S)
        if not match:
            raise SystemExit(f"No text template for {identifier}")
        template = (match.group(1).rstrip() + "\n").encode("utf-8")
        (TEMPLATES / f"{identifier}.txt").write_bytes(template)
        if identifier == "A00":
            agent_checksum = hashlib.sha256(template).hexdigest()
            continue
        tools = []
        if identifier == "P02":
            tools = ["read_evidence", "find_references"]
        elif identifier == "P03":
            tools = ["read_evidence", "find_references", "expand_graph"]
        entries.append({
            "id": identifier,
            "version": "1.1.0",
            "purpose": purpose,
            "template_ref": f"templates/{identifier}.txt",
            "input_schema_ref": "../contracts/prompt-input.schema.json",
            "output_schema_ref": "../contracts/" + ({"P01":"synthesis-result", "P04":"synthesis-result", "P07":"synthesis-result", "P05":"question-plan", "P06":"support-result"}.get(identifier,"prompt-output")) + ".schema.json",
            "allowed_tools": tools,
            "max_output_tokens": 6000,
            "max_tool_calls": 6 if tools else 0,
            "checksum": hashlib.sha256(template).hexdigest(),
        })
    REGISTRY.write_text(json.dumps({"registry_version": "1.0.0", "agent_template_ref": "templates/A00.txt",
                                    "agent_checksum": agent_checksum, "prompts": entries}, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
