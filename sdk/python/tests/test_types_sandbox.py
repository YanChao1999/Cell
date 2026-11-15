"""Offline unit tests for SDK types and path helpers (no httpx required)."""

from __future__ import annotations

import importlib.util
import sys
import unittest
from pathlib import Path

_SRC = Path(__file__).resolve().parents[1] / "src"


def _load(name: str, rel: str):
    path = _SRC / rel
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod


_types = _load("cell_types_ut", "cell/types.py")
_files = _load("cell_files_ut", "cell/files.py")
ExecResult = _types.ExecResult
SnapshotInfo = _types.SnapshotInfo
_workspace_rel = _files._workspace_rel


class TypesTest(unittest.TestCase):
    def test_snapshot_info(self) -> None:
        snap = SnapshotInfo.from_dict(
            {"id": "abc", "cell_id": "c1", "bytes": 12, "created_at": "t"}
        )
        self.assertEqual(snap.id, "abc")
        self.assertEqual(snap.bytes, 12)

    def test_exec_result_ok(self) -> None:
        self.assertTrue(ExecResult(0, "x", "").ok)
        self.assertFalse(ExecResult(1, "", "err").ok)


class FilesPathTest(unittest.TestCase):
    def test_rel(self) -> None:
        self.assertEqual(_workspace_rel("/workspace/a.txt"), "a.txt")
        self.assertEqual(_workspace_rel("src/main.go"), "src/main.go")

    def test_escape(self) -> None:
        with self.assertRaises(ValueError):
            _workspace_rel("../etc/passwd")


if __name__ == "__main__":
    unittest.main()
