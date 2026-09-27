"""Refresh registry checksums after editing canonical prompts/templates/*.txt.

Preserves prompt versions, budgets and schema metadata. Bump a prompt's version
in registry.json when its meaning changes. Does not rewrite template wording.
"""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REGISTRY = ROOT / "prompts" / "registry.json"

def checksum(relative):
    target = (REGISTRY.parent / relative).resolve()
    if not target.is_relative_to(REGISTRY.parent / "templates"):
        raise ValueError("template reference must stay in prompts/templates")
    return hashlib.sha256(target.read_bytes()).hexdigest()

def main():
    registry = json.loads(REGISTRY.read_text(encoding="utf-8"))
    registry["agent_checksum"] = checksum(registry["agent_template_ref"])
    for entry in registry["prompts"]:
        entry["checksum"] = checksum(entry["template_ref"])
    REGISTRY.write_text(json.dumps(registry, indent=2) + "\n", encoding="utf-8")

if __name__ == "__main__":
    main()
