"""Spark Python SDK — ephemeral isolated environments for AI agents."""

from spark.client import Client, Spark
from spark.types import ExecResult, Limits, RunResult, SparkInfo

__all__ = [
    "Client",
    "Spark",
    "ExecResult",
    "Limits",
    "RunResult",
    "SparkInfo",
]

__version__ = "0.1.0"
