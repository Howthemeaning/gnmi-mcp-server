"""gnmic CLI subprocess management -- spawn, run, terminate.

GnmicClient is stateless: it only handles process lifecycle.
Session state is managed by SessionManager in session.py.
"""

import asyncio
import json
import logging
import os
from dataclasses import dataclass
from typing import Optional, Any

logger = logging.getLogger(__name__)


@dataclass
class GnmicResult:
    """Result of a gnmic CLI execution."""
    stdout: str
    stderr: str
    exit_code: int

    @property
    def is_success(self) -> bool:
        return self.exit_code == 0

    @property
    def parsed(self) -> Optional[Any]:
        """Parse stdout as JSON, or return None if not JSON."""
        try:
            return json.loads(self.stdout)
        except (json.JSONDecodeError, TypeError):
            return None

    @property
    def error_message(self) -> str:
        """Human-readable error suitable for returning to AI."""
        if self.is_success:
            return ""
        msg = self.stderr.strip() or self.stdout.strip() or f"exit code {self.exit_code}"
        if "connection refused" in msg.lower():
            return f"Connection refused. Check that the target is reachable and gNMI is enabled. Details: {msg}"
        if "unauthenticated" in msg.lower() or "permission denied" in msg.lower():
            return f"Authentication failed. Verify GNMI_USER_* and GNMI_PASS_* environment variables. Details: {msg}"
        if "timeout" in msg.lower() or "deadline exceeded" in msg.lower():
            return f"Request timed out. The device may be slow or unreachable. Details: {msg}"
        if "not found" in msg.lower() or "no such" in msg.lower():
            return f"Path or resource not found. Check the gNMI path syntax. Details: {msg}"
        return f"gnmic error: {msg}"


@dataclass
class ProcessHandle:
    """Handle to a running gnmic subprocess."""
    pid: int
    process: asyncio.subprocess.Process


class GnmicClient:
    """Manages gnmic CLI subprocess execution.

    Responsibilities:
    - Spawn one-shot commands (capabilities, get, set, path)
    - Spawn long-running subscribe processes
    - Terminate processes

    Does NOT manage session state -- see SessionManager for that.
    """

    def __init__(self, binary_path: str):
        self.binary_path = binary_path

    def _build_env(self, device) -> dict:
        """Build environment dict with gnmic credentials.

        Credentials are passed via GNMIC_USERNAME/GNMIC_PASSWORD env vars,
        NEVER on the command line (prevents ps auxe exposure).
        """
        env = os.environ.copy()
        env["GNMIC_USERNAME"] = device.username
        env["GNMIC_PASSWORD"] = device.password
        return env

    def _build_args(self, command: str, extra_args: list[str]) -> list[str]:
        """Build the argument list for gnmic execution."""
        args = [self.binary_path, "--format", "json", command]
        args.extend(extra_args)
        return args

    async def run(self, command: str, extra_args: list[str],
                  device, timeout: int = 30) -> GnmicResult:
        """Execute a gnmic command and wait for completion."""
        env = self._build_env(device)
        args = self._build_args(command, extra_args)

        logger.debug(f"Running: {' '.join(args)}")

        proc = None
        try:
            proc = await asyncio.create_subprocess_exec(
                *args,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=env,
            )
            stdout, stderr = await asyncio.wait_for(
                proc.communicate(), timeout=timeout
            )
            result = GnmicResult(
                stdout=stdout.decode("utf-8", errors="replace") if stdout else "",
                stderr=stderr.decode("utf-8", errors="replace") if stderr else "",
                exit_code=proc.returncode or 0,
            )
            logger.debug(f"gnmic exited with code {result.exit_code}")
            return result
        except asyncio.TimeoutError:
            if proc:
                try:
                    proc.terminate()
                except Exception:
                    pass
            return GnmicResult(
                stdout="",
                stderr=f"Command timed out after {timeout}s",
                exit_code=-1,
            )
        except FileNotFoundError:
            return GnmicResult(
                stdout="",
                stderr=f"gnmic binary not found at '{self.binary_path}'. "
                        f"Install gnmic or set GNMI_BINARY_PATH.",
                exit_code=-1,
            )
        except Exception as e:
            return GnmicResult(
                stdout="",
                stderr=str(e),
                exit_code=-1,
            )

    async def spawn(self, command: str, extra_args: list[str],
                    device, output_file: str, stderr_file: str) -> ProcessHandle:
        """Spawn a long-running gnmic process (for subscribe STREAM/POLL).

        Stdout is redirected to output_file, stderr to stderr_file.
        Returns immediately with a ProcessHandle.
        """
        env = self._build_env(device)
        args = self._build_args(command, extra_args)

        logger.debug(f"Spawning: {' '.join(args)}")

        os.makedirs(os.path.dirname(output_file), exist_ok=True)
        os.makedirs(os.path.dirname(stderr_file), exist_ok=True)

        stdout_fh = open(output_file, "a")
        stderr_fh = open(stderr_file, "a")

        proc = await asyncio.create_subprocess_exec(
            *args,
            stdout=stdout_fh,
            stderr=stderr_fh,
            env=env,
        )
        # 子进程已 dup fd，关闭父进程的句柄避免泄漏
        stdout_fh.close()
        stderr_fh.close()

        logger.info(f"Spawned gnmic subscribe (pid={proc.pid}) -> {output_file}")
        return ProcessHandle(pid=proc.pid, process=proc)

    async def terminate(self, handle: ProcessHandle) -> bool:
        """Terminate a spawned process gracefully (SIGTERM, then SIGKILL)."""
        try:
            proc = handle.process
            if proc.returncode is not None:
                logger.info(f"Process {handle.pid} already exited")
                return True

            logger.info(f"Terminating process {handle.pid}")
            proc.terminate()
            try:
                await asyncio.wait_for(proc.wait(), timeout=5.0)
            except asyncio.TimeoutError:
                logger.warning(f"Process {handle.pid} did not exit, sending SIGKILL")
                proc.kill()
                await proc.wait()
            return True
        except ProcessLookupError:
            logger.info(f"Process {handle.pid} already gone")
            return True
        except Exception as e:
            logger.error(f"Failed to terminate process {handle.pid}: {e}")
            return False
