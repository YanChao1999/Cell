"""Shared types for the Spark Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, TypedDict


class LimitsDict(TypedDict, total=False):
    cpu: str
    memory: str
    disk: str


class CreateSparkRequest(TypedDict, total=False):
    repo: str
    ref: str
    environment: str
    ttl: str
    prefer_local: bool
    limits: LimitsDict
    client_worker_id: str


class ExecRequest(TypedDict, total=False):
    command: str
    timeout_seconds: int


class RunRequest(CreateSparkRequest, total=False):
    commands: list[str]


@dataclass(frozen=True)
class Limits:
    cpu: str | None = None
    memory: str | None = None
    disk: str | None = None

    def to_dict(self) -> dict[str, str]:
        out: dict[str, str] = {}
        if self.cpu is not None:
            out["cpu"] = self.cpu
        if self.memory is not None:
            out["memory"] = self.memory
        if self.disk is not None:
            out["disk"] = self.disk
        return out

    @classmethod
    def from_dict(cls, data: dict[str, Any] | None) -> Limits | None:
        if not data:
            return None
        return cls(
            cpu=data.get("cpu"),
            memory=data.get("memory"),
            disk=data.get("disk"),
        )


@dataclass(frozen=True)
class ExecResult:
    exit_code: int
    stdout: str
    stderr: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ExecResult:
        return cls(
            exit_code=int(data.get("exit_code", -1)),
            stdout=str(data.get("stdout", "")),
            stderr=str(data.get("stderr", "")),
        )

    @property
    def ok(self) -> bool:
        return self.exit_code == 0


@dataclass
class SparkInfo:
    """Metadata returned by CreateSpark / GetSpark / ListSparks."""

    id: str
    worker_id: str = ""
    environment: str = ""
    status: str = ""
    expires_at: str = ""
    raw: dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> SparkInfo:
        return cls(
            id=str(data["id"]),
            worker_id=str(data.get("worker_id", "")),
            environment=str(data.get("environment", "")),
            status=str(data.get("status", "")),
            expires_at=str(data.get("expires_at", "")),
            raw=dict(data),
        )


@dataclass(frozen=True)
class RunResult:
    id: str
    results: tuple[ExecResult, ...]
    destroyed: bool = True

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> RunResult:
        results = tuple(
            ExecResult.from_dict(item) for item in data.get("results", [])
        )
        return cls(
            id=str(data.get("id", "")),
            results=results,
            destroyed=bool(data.get("destroyed", True)),
        )
