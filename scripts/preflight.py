#!/usr/bin/env python3
"""Run CI's own steps locally, straight out of the workflow file.

The point is that there is no second copy of the build. Everything CI runs is
read out of .github/workflows/ci.yml and executed here in the same order with
the same environment, so a step added to CI is a step preflight runs, and a
green preflight means a green CI for the same reasons.

A `uses:` step is a hole in that guarantee, because this script cannot run a
GitHub Action. Setup and checkout actions do nothing a local checkout has not
already done, so they are skipped by name; anything else stops the run with an
explanation rather than quietly passing. That is the only way divergence can be
noticed instead of discovered on a red pipeline.

Usage:
    scripts/preflight.py [--job JOB] [--list] [--workflow PATH]
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import time
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.exit("preflight: PyYAML is required (pip install pyyaml)")

REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_WORKFLOW = REPO_ROOT / ".github" / "workflows" / "ci.yml"

# Actions that only prepare a runner to look like a developer's machine. Local
# checkouts and toolchains already satisfy them, so skipping them changes
# nothing about what is verified.
INERT_ACTIONS = (
    "actions/checkout",
    "actions/setup-go",
    "actions/setup-node",
    "actions/cache",
    "actions/upload-artifact",
    "actions/download-artifact",
)

BOLD, DIM, RED, GREEN, YELLOW, RESET = "\033[1m", "\033[2m", "\033[31m", "\033[32m", "\033[33m", "\033[0m"
if not sys.stdout.isatty() or os.environ.get("NO_COLOR"):
    BOLD = DIM = RED = GREEN = YELLOW = RESET = ""


def load_workflow(path: Path) -> dict:
    if not path.exists():
        sys.exit(f"preflight: no workflow at {path}")
    with path.open() as fh:
        workflow = yaml.safe_load(fh)
    if not isinstance(workflow, dict) or not workflow.get("jobs"):
        sys.exit(f"preflight: {path} has no jobs")
    return workflow


def action_name(uses: str) -> str:
    return uses.split("@", 1)[0]


def describe_services(job: dict) -> None:
    """Tell the developer what CI provides that they must provide themselves."""
    services = job.get("services") or {}
    for name, spec in services.items():
        image = spec.get("image", "?")
        print(f"{DIM}  service: {name} ({image}) — CI starts this; preflight expects it locally{RESET}")


def run_job(name: str, job: dict, workflow_env: dict) -> int:
    env = os.environ.copy()
    env.update({k: str(v) for k, v in (workflow_env or {}).items()})
    env.update({k: str(v) for k, v in (job.get("env") or {}).items()})

    print(f"{BOLD}preflight: job {name}{RESET}")
    describe_services(job)

    steps = job.get("steps") or []
    ran = 0
    for index, step in enumerate(steps, start=1):
        label = step.get("name") or step.get("uses") or f"step {index}"

        if "uses" in step:
            if action_name(step["uses"]).startswith(INERT_ACTIONS):
                print(f"{DIM}  skip  {label} (setup action){RESET}")
                continue
            print(f"{RED}  stop  {label}{RESET}")
            print(
                f"\n{RED}preflight cannot run the action {step['uses']!r}, so it cannot claim "
                f"parity with CI.{RESET}\n"
                "Either replace that step with a `run:` block, or add the action to "
                "INERT_ACTIONS in scripts/preflight.py if it genuinely does nothing "
                "a local checkout has not already done."
            )
            return 1

        script = step.get("run")
        if script is None:
            continue

        step_env = dict(env)
        step_env.update({k: str(v) for k, v in (step.get("env") or {}).items()})
        workdir = step.get("working-directory") or str(REPO_ROOT)

        print(f"{BOLD}  run   {label}{RESET}")
        started = time.monotonic()
        result = subprocess.run(
            ["bash", "-eo", "pipefail", "-c", script],
            cwd=workdir,
            env=step_env,
        )
        elapsed = time.monotonic() - started
        if result.returncode != 0:
            print(f"{RED}  fail  {label} ({elapsed:.1f}s, exit {result.returncode}){RESET}")
            return result.returncode
        print(f"{GREEN}  ok    {label}{RESET} {DIM}({elapsed:.1f}s){RESET}")
        ran += 1

    if ran == 0:
        print(f"{YELLOW}  no runnable steps in job {name}{RESET}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--workflow", type=Path, default=DEFAULT_WORKFLOW)
    parser.add_argument("--job", action="append", help="run only this job (repeatable)")
    parser.add_argument("--list", action="store_true", help="list the jobs and exit")
    args = parser.parse_args()

    workflow = load_workflow(args.workflow)
    jobs = workflow["jobs"]

    if args.list:
        for name, job in jobs.items():
            print(f"{name}\t{job.get('name', name)}")
        return 0

    selected = args.job or list(jobs)
    for name in selected:
        if name not in jobs:
            sys.exit(f"preflight: no job named {name!r} in {args.workflow} (have: {', '.join(jobs)})")

    started = time.monotonic()
    for name in selected:
        code = run_job(name, jobs[name], workflow.get("env") or {})
        if code != 0:
            print(f"\n{RED}preflight failed in job {name}{RESET}")
            return code

    print(f"\n{GREEN}preflight passed{RESET} {DIM}({time.monotonic() - started:.1f}s){RESET}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
