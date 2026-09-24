"""Copy Phase 0 fixtures into fresh local Git repositories for integration tests.

Usage: python scripts/materialize_fixtures.py <new-output-directory>
The output directory must not exist. The mixed fixture gets one commit; the sparse
fixture intentionally remains an unborn-HEAD working tree.
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def run_git(directory: Path, *arguments: str) -> None:
    subprocess.run(["git", *arguments], cwd=directory, check=True, capture_output=True)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="A new directory for generated Git fixtures")
    arguments = parser.parse_args()
    output = arguments.output.resolve()
    if output.exists():
        parser.error("output directory already exists; choose a new path")
    output.mkdir(parents=True)

    for name in ("mixed-monolith", "sparse-repo"):
        source = ROOT / "testdata" / name
        destination = output / name
        shutil.copytree(source, destination)
        run_git(destination, "init", "-q")
        if name == "mixed-monolith":
            run_git(destination, "add", "--all")
            run_git(
                destination,
                "-c", "user.name=Fixture Builder",
                "-c", "user.email=fixture@example.invalid",
                "commit", "-q", "-m", "synthetic fixture",
            )
        print(f"{name}: {destination}")


if __name__ == "__main__":
    main()
