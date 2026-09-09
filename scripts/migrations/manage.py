#!/usr/bin/env python3
"""Validate ordered migration pairs and create the next pair without collisions."""

import argparse
import fcntl
import os
from pathlib import Path
import re
import sys

MODULES = ("user", "post", "notification", "settings")
FILENAME = re.compile(r"([0-9]{6})_([a-z][a-z0-9]*(?:_[a-z0-9]+)*)\.(up|down)\.sql")
NAME = re.compile(r"[a-z][a-z0-9]*(?:_[a-z0-9]+)*")


def validate(directory, *, allow_empty=False):
    pairs = {}
    for path in sorted(directory.iterdir()):
        if path.suffix != ".sql":
            continue
        match = FILENAME.fullmatch(path.name)
        if not match:
            raise ValueError(f"invalid migration filename: {path}")
        version, name, direction = match.groups()
        entry = pairs.setdefault(int(version), {})
        if direction in entry:
            raise ValueError(f"duplicate version {version} ({direction}) in {directory}")
        entry[direction] = name
        if not allow_empty:
            sql = re.sub(r"/\*.*?\*/|--[^\n]*", "", path.read_text(), flags=re.S)
            if not sql.strip():
                raise ValueError(f"migration has no SQL: {path}")
    if sorted(pairs) != list(range(1, len(pairs) + 1)):
        raise ValueError(f"migration versions must start at 000001 without gaps: {directory}")
    for version, entry in pairs.items():
        if set(entry) != {"up", "down"} or entry["up"] != entry["down"]:
            raise ValueError(f"missing or mismatched up/down pair {version:06d}: {directory}")
    return len(pairs)


def create(root, module, name):
    if module not in MODULES:
        raise ValueError(f"invalid active module: {module}; expected {', '.join(MODULES)}")
    if not NAME.fullmatch(name):
        raise ValueError("name must be lowercase snake_case, starting with a letter")
    directory = root / module
    # Lock the directory itself: no lock file to commit or stale lock to clear.
    fd = os.open(directory, os.O_RDONLY)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX)
        version = validate(directory, allow_empty=True) + 1
        if version > 999999:
            raise ValueError("six-digit migration sequence exhausted")
        paths = [directory / f"{version:06d}_{name}.{direction}.sql" for direction in ("up", "down")]
        created = []
        try:
            for path in paths:
                with path.open("x") as stream:
                    created.append(path)
                    stream.write("-- Replace this comment with the migration SQL.\n")
        except OSError:
            for path in created:
                path.unlink()
            raise
    finally:
        os.close(fd)
    return paths


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2] / "migrations")
    subcommands = parser.add_subparsers(dest="command", required=True)
    subcommands.add_parser("check")
    command = subcommands.add_parser("create")
    command.add_argument("module")
    command.add_argument("name")
    args = parser.parse_args()
    try:
        if args.command == "check":
            for module in MODULES:
                if not validate(args.root / module):
                    raise ValueError(f"missing baseline: {module}")
            print("Migration pairs, names, sequences and SQL content are valid")
        else:
            for path in create(args.root, args.module, args.name):
                print(path)
    except (OSError, ValueError) as error:
        print(f"migration: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
