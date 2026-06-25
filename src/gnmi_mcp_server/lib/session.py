"""Subscribe session lifecycle management.

SessionManager owns the session registry, metadata persistence,
tail reading, and cleanup. It delegates process operations to GnmicClient.
"""

import asyncio
import json
import logging
import os
import re
import platform
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)

# Valid session name: [A-Za-z0-9_-], max 64 chars
_SESSION_NAME_RE = re.compile(r'^[A-Za-z0-9_-]{1,64}$')

SESSION_STATUS_RUNNING = "running"
SESSION_STATUS_STOPPED = "stopped"
SESSION_STATUS_CRASHED = "crashed"
SESSION_STATUS_UNKNOWN = "unknown"


class SessionError(Exception):
    """Session operation error."""
    pass


@dataclass
class SessionHandle:
    """Metadata for a subscribe session."""
    name: str
    pid: int
    target: str
    address: str
    path: str
    mode: str
    stream_mode: str = "TARGET_DEFINED"
    sample_interval: str = ""
    output_file: str = ""
    stderr_file: str = ""
    metadata_file: str = ""
    created_at: float = field(default_factory=time.time)
    status: str = SESSION_STATUS_RUNNING
    output_bytes: int = 0
    process: Optional[object] = None  # ProcessHandle (from GnmicClient.spawn)


def _validate_session_name(name: str) -> str:
    """Validate and normalize session name."""
    if not _SESSION_NAME_RE.match(name):
        raise SessionError(
            f"Invalid session name '{name}'. Must be 1-64 chars of [A-Za-z0-9_-]."
        )
    return name


def _check_pid_alive(pid: int, search_term: str = "gnmic") -> bool:
    """Cross-platform check if a process is alive."""
    system = platform.system().lower()
    if system == "linux":
        cmdline_path = f"/proc/{pid}/cmdline"
        try:
            with open(cmdline_path, "rb") as f:
                content = f.read()
                return search_term.encode() in content
        except (FileNotFoundError, PermissionError):
            return False
    elif system == "darwin":
        import subprocess
        try:
            result = subprocess.run(
                ["ps", "-p", str(pid), "-o", "command="],
                capture_output=True, text=True, timeout=5
            )
            return search_term in result.stdout
        except Exception:
            return False
    else:
        return False


class SessionManager:
    """Manages subscribe session lifecycle."""

    def __init__(self, sessions_dir: str):
        self._sessions_dir = Path(sessions_dir)
        os.makedirs(self._sessions_dir, exist_ok=True)
        self._sessions: dict[str, SessionHandle] = {}
        self._lock = asyncio.Lock()

    def _session_dir(self, name: str) -> Path:
        return self._sessions_dir / name

    def _write_metadata(self, session: SessionHandle):
        """Persist session metadata to disk."""
        os.makedirs(self._session_dir(session.name), exist_ok=True)
        meta = {
            "name": session.name, "pid": session.pid,
            "target": session.target, "address": session.address,
            "path": session.path, "mode": session.mode,
            "stream_mode": session.stream_mode,
            "sample_interval": session.sample_interval,
            "created_at": session.created_at, "status": session.status,
            "output_bytes": session.output_bytes,
        }
        meta_path = self._session_dir(session.name) / "metadata.json"
        with open(meta_path, "w") as f:
            json.dump(meta, f, indent=2)

    def _read_metadata(self, name: str) -> Optional[dict]:
        meta_path = self._session_dir(name) / "metadata.json"
        if not meta_path.exists():
            return None
        with open(meta_path) as f:
            return json.load(f)

    async def create(self, gnmic_client, device, path: str, mode: str,
                     stream_mode: str = "TARGET_DEFINED",
                     sample_interval: str = "",
                     heartbeat_interval: str = "",
                     session_name: str = "") -> SessionHandle:
        """Create and start a new subscribe session."""
        if not session_name:
            session_name = f"sub-{int(time.time())}"
        _validate_session_name(session_name)

        session_dir = self._session_dir(session_name)

        extra_args = ["--path", path, "--mode", mode.lower()]
        if stream_mode:
            extra_args.extend(["--stream-mode", stream_mode.lower()])
        if sample_interval:
            extra_args.extend(["--sample-interval", sample_interval])
        if heartbeat_interval:
            extra_args.extend(["--heartbeat-interval", heartbeat_interval])

        async with self._lock:
            existing = self._sessions.get(session_name)
            if existing and existing.status == SESSION_STATUS_RUNNING:
                raise SessionError(
                    f"Session '{session_name}' is already running."
                )

            output_file = str(session_dir / "output.json")
            stderr_file = str(session_dir / "stderr.log")
            metadata_file = str(session_dir / "metadata.json")

            process_handle = await gnmic_client.spawn(
                "subscribe", extra_args, device,
                output_file=output_file, stderr_file=stderr_file,
            )

            session = SessionHandle(
                name=session_name, pid=process_handle.pid,
                target=device.name, address=device.address,
                path=path, mode=mode, stream_mode=stream_mode,
                sample_interval=sample_interval,
                output_file=output_file, stderr_file=stderr_file,
                metadata_file=metadata_file,
                process=process_handle,
            )
            self._write_metadata(session)
            self._sessions[session_name] = session

        logger.info(f"Created subscribe session '{session_name}' (pid={session.pid})")
        return session

    async def stop(self, gnmic_client, name: str) -> bool:
        """Stop a running subscribe session."""
        _validate_session_name(name)

        async with self._lock:
            session = self._sessions.get(name)
            if not session:
                meta = self._read_metadata(name)
                if meta and meta.get("status") == SESSION_STATUS_RUNNING:
                    if _check_pid_alive(meta["pid"]):
                        raise SessionError(
                            f"Session '{name}' was created by a previous server instance. "
                            f"PID {meta['pid']} is still running."
                        )
                    else:
                        self._mark_status(name, SESSION_STATUS_CRASHED)
                        return True
                raise SessionError(f"Session '{name}' not found.")

            if session.status != SESSION_STATUS_RUNNING:
                return True

            result = await gnmic_client.terminate(session.process)
            session.status = SESSION_STATUS_STOPPED
            self._write_metadata(session)
            return result

    def list(self, active_only: bool = False) -> list[dict]:
        """List all known sessions."""
        results = []
        for name in os.listdir(self._sessions_dir):
            meta = self._read_metadata(name)
            if not meta:
                continue
            session = self._sessions.get(name)
            if session:
                results.append(self._summary(session))
            else:
                meta["status"] = self._check_disk_status(meta)
                if not active_only or meta["status"] == SESSION_STATUS_RUNNING:
                    results.append({
                        "name": meta["name"], "pid": meta["pid"],
                        "target": meta["target"], "path": meta["path"],
                        "mode": meta["mode"], "status": meta["status"],
                        "created_at": meta["created_at"],
                        "output_bytes": meta.get("output_bytes", 0),
                    })
        return results

    async def tail(self, name: str, lines: int = 20) -> str:
        """Read the last N lines of a session's output file."""
        _validate_session_name(name)
        lines = min(max(lines, 1), 500)

        output_path = self._session_dir(name) / "output.json"
        if not output_path.exists():
            return f"No output data for session '{name}' yet."

        with open(output_path, "r") as f:
            all_lines = f.readlines()
            recent = all_lines[-lines:]
            return "".join(recent).strip()

    def _summary(self, session: SessionHandle) -> dict:
        output_bytes = 0
        try:
            output_bytes = os.path.getsize(session.output_file)
        except OSError:
            pass
        return {
            "name": session.name, "pid": session.pid,
            "target": session.target, "path": session.path,
            "mode": session.mode, "status": session.status,
            "created_at": session.created_at, "output_bytes": output_bytes,
        }

    def _check_disk_status(self, meta: dict) -> str:
        if meta["status"] == SESSION_STATUS_RUNNING:
            if _check_pid_alive(meta["pid"]):
                return SESSION_STATUS_RUNNING
            else:
                return SESSION_STATUS_CRASHED
        return meta["status"]

    def _mark_status(self, name: str, status: str):
        meta = self._read_metadata(name)
        if meta:
            meta["status"] = status
            meta_path = self._session_dir(name) / "metadata.json"
            with open(meta_path, "w") as f:
                json.dump(meta, f, indent=2)

    async def cleanup(self, gnmic_client):
        """Stop all active sessions (called on server shutdown)."""
        logger.info("Cleaning up all active sessions...")
        async with self._lock:
            for name, session in list(self._sessions.items()):
                if session.status == SESSION_STATUS_RUNNING:
                    try:
                        await gnmic_client.terminate(session.process)
                        session.status = SESSION_STATUS_STOPPED
                        self._write_metadata(session)
                    except Exception as e:
                        logger.error(f"Failed to stop session '{name}': {e}")
