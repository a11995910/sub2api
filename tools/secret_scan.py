#!/usr/bin/env python3
"""轻量 secret scanning（CI 门禁 + 本地自检）。

只扫描 Git 已跟踪文件，输出仅包含文件、行号与规则名，不回显敏感内容。
"""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Sequence


@dataclass(frozen=True)
class Rule:
    name: str
    pattern: re.Pattern[str]
    allowlist: Sequence[re.Pattern[str]]


RULES: list[Rule] = [
    Rule(
        name="google_oauth_client_secret",
        pattern=re.compile(r"GOCSPX-[0-9A-Za-z_-]{24,}"),
        allowlist=(
            re.compile(r"GOCSPX-your-"),
            re.compile(r"GOCSPX-REDACTED"),
        ),
    ),
    Rule(
        name="google_api_key",
        pattern=re.compile(r"AIza[0-9A-Za-z_-]{35}"),
        allowlist=(
            re.compile(r"AIza\.{3}"),
            re.compile(r"AIza-your-"),
            re.compile(r"AIza-REDACTED"),
        ),
    ),
]


def iter_git_files(repo_root: Path) -> list[Path]:
    try:
        output = subprocess.check_output(
            ["git", "ls-files"],
            cwd=repo_root,
            stderr=subprocess.DEVNULL,
            text=True,
        )
    except Exception:
        return []
    files: list[Path] = []
    for line in output.splitlines():
        path = (repo_root / line).resolve()
        if path.is_file():
            files.append(path)
    return files


def iter_walk_files(repo_root: Path) -> Iterable[Path]:
    for dirpath, _dirnames, filenames in os.walk(repo_root):
        if "/.git/" in dirpath.replace("\\", "/"):
            continue
        for name in filenames:
            yield Path(dirpath) / name


def should_skip(path: Path, repo_root: Path) -> bool:
    relative = path.relative_to(repo_root).as_posix()
    if any(
        relative.endswith(suffix)
        for suffix in (".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip")
    ):
        return True
    return relative.startswith("backend/bin/")


def scan_file(path: Path, repo_root: Path) -> list[str]:
    try:
        raw = path.read_bytes()
        text = raw.decode("utf-8")
    except (OSError, UnicodeDecodeError):
        return []

    findings: list[str] = []
    for line_number, line in enumerate(text.splitlines(), start=1):
        for rule in RULES:
            if not rule.pattern.search(line):
                continue
            if any(allowed.search(line) for allowed in rule.allowlist):
                continue
            relative = path.relative_to(repo_root).as_posix()
            findings.append(f"{relative}:{line_number} ({rule.name})")
    return findings


def main(argv: Sequence[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--repo-root",
        default=str(Path(__file__).resolve().parents[1]),
        help="仓库根目录（默认：脚本上两级目录）",
    )
    args = parser.parse_args(argv)

    repo_root = Path(args.repo_root).resolve()
    files = iter_git_files(repo_root)
    if not files:
        files = list(iter_walk_files(repo_root))

    problems: list[str] = []
    for file in files:
        if not should_skip(file, repo_root):
            problems.extend(scan_file(file, repo_root))

    if problems:
        sys.stderr.write("Secret scan FAILED. Potential secrets detected:\n")
        for problem in problems:
            sys.stderr.write(f"- {problem}\n")
        sys.stderr.write(
            "\n请移除或改为环境变量注入，并使用明确的占位符。\n"
        )
        return 1

    print("Secret scan OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
