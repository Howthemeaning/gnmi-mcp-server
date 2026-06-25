import asyncio
import json
import pytest
from pathlib import Path
from unittest.mock import AsyncMock, patch, MagicMock
from gnmi_mcp_server.lib.gnmic import GnmicClient, GnmicResult

@pytest.fixture
def client():
    return GnmicClient(binary_path="/fake/gnmic")

@pytest.fixture
def device():
    from gnmi_mcp_server.lib.config import DeviceConfig
    return DeviceConfig(
        name="sw1", address="10.0.0.1:57400",
        username="admin", password="secret"
    )

class TestGnmicResult:
    def test_success_result(self):
        r = GnmicResult(stdout='{"key": "value"}', stderr="", exit_code=0)
        assert r.is_success
        assert r.parsed == {"key": "value"}

    def test_error_result(self):
        r = GnmicResult(stdout="", stderr="connection refused", exit_code=1)
        assert not r.is_success
        assert "connection refused" in r.error_message

    def test_non_json_output(self):
        r = GnmicResult(stdout="plain text", stderr="", exit_code=0)
        assert r.is_success
        assert r.parsed is None
        assert r.stdout == "plain text"

class TestGnmicClientRun:
    @pytest.mark.asyncio
    async def test_run_success(self, client, device):
        mock_proc = AsyncMock()
        mock_proc.communicate.return_value = (b'{"result": "ok"}', b"")
        mock_proc.returncode = 0

        with patch("asyncio.create_subprocess_exec", return_value=mock_proc) as mock_exec:
            result = await client.run("capabilities", [], device)

        call_kwargs = mock_exec.call_args[1]
        assert "env" in call_kwargs
        assert call_kwargs["env"].get("GNMIC_USERNAME") == "admin"
        assert call_kwargs["env"].get("GNMIC_PASSWORD") == "secret"

        args = mock_exec.call_args[0]
        assert "--format" in args
        assert "json" in args

        assert result.is_success

    @pytest.mark.asyncio
    async def test_run_auth_error(self, client, device):
        mock_proc = AsyncMock()
        mock_proc.communicate.return_value = (b"", b"rpc error: Unauthenticated")
        mock_proc.returncode = 1

        with patch("asyncio.create_subprocess_exec", return_value=mock_proc):
            result = await client.run("get", ["--path", "/state"], device)

        assert not result.is_success
        assert "Authentication" in result.error_message

    @pytest.mark.asyncio
    async def test_run_timeout(self, client, device):
        mock_proc = AsyncMock()
        mock_proc.communicate.side_effect = asyncio.TimeoutError()

        with patch("asyncio.create_subprocess_exec", return_value=mock_proc):
            result = await client.run("get", ["--path", "/state"], device, timeout=5)

        assert not result.is_success
        assert "timed out" in result.error_message.lower()

    @pytest.mark.asyncio
    async def test_credentials_not_in_args(self, client, device):
        mock_proc = AsyncMock()
        mock_proc.communicate.return_value = (b"{}", b"")
        mock_proc.returncode = 0

        with patch("asyncio.create_subprocess_exec", return_value=mock_proc) as mock_exec:
            await client.run("get", ["--path", "/state"], device)

        all_args = " ".join(str(a) for a in mock_exec.call_args[0])
        assert "--username" not in all_args
        assert "--password" not in all_args
        assert device.password not in all_args

    @pytest.mark.asyncio
    async def test_run_file_not_found(self, client, device):
        with patch("asyncio.create_subprocess_exec", side_effect=FileNotFoundError("gnmic not found")):
            result = await client.run("capabilities", [], device)

        assert not result.is_success
        assert "not found" in result.error_message.lower()

class TestGnmicClientSpawn:
    @pytest.mark.asyncio
    async def test_spawn_subscribe(self, client, device, tmp_path):
        output_file = tmp_path / "output.json"
        stderr_file = tmp_path / "stderr.log"

        mock_proc = AsyncMock()
        mock_proc.pid = 12345
        mock_proc.returncode = None

        with patch("asyncio.create_subprocess_exec", return_value=mock_proc) as mock_exec:
            handle = await client.spawn(
                "subscribe", ["--path", "/state"], device,
                output_file=str(output_file), stderr_file=str(stderr_file)
            )

        assert handle.pid == 12345
        assert handle.process is mock_proc

        call_kwargs = mock_exec.call_args[1]
        assert call_kwargs["stdout"].name == str(output_file)
        assert call_kwargs["stderr"].name == str(stderr_file)

    @pytest.mark.asyncio
    async def test_terminate_process(self, client):
        mock_proc = AsyncMock()
        mock_proc.returncode = None
        mock_proc.wait.return_value = None

        from gnmi_mcp_server.lib.gnmic import ProcessHandle
        handle = ProcessHandle(pid=12345, process=mock_proc)

        result = await client.terminate(handle)
        assert result is True
        mock_proc.terminate.assert_called_once()

    @pytest.mark.asyncio
    async def test_terminate_already_exited(self, client):
        mock_proc = AsyncMock()
        mock_proc.returncode = 0

        from gnmi_mcp_server.lib.gnmic import ProcessHandle
        handle = ProcessHandle(pid=12345, process=mock_proc)

        result = await client.terminate(handle)
        assert result is True
        mock_proc.terminate.assert_not_called()
