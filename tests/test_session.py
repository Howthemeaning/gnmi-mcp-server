import json
import pytest
from pathlib import Path
from unittest.mock import AsyncMock
from gnmi_mcp_server.lib.session import (
    SessionManager, SessionHandle, SessionError,
    _validate_session_name, SESSION_STATUS_RUNNING, SESSION_STATUS_STOPPED
)
from gnmi_mcp_server.lib.config import DeviceConfig

@pytest.fixture
def device():
    return DeviceConfig(name="sw1", address="10.0.0.1:57400", username="admin", password="secret")

@pytest.fixture
def session_mgr(tmp_path):
    return SessionManager(sessions_dir=str(tmp_path / "sessions"))

@pytest.fixture
def mock_client():
    client = AsyncMock()
    handle = AsyncMock()
    handle.pid = 12345
    client.spawn.return_value = handle
    return client

class TestSessionNameValidation:
    def test_valid_names(self):
        assert _validate_session_name("test-1") == "test-1"
        assert _validate_session_name("my_session_2") == "my_session_2"

    def test_invalid_characters(self):
        with pytest.raises(SessionError):
            _validate_session_name("bad/name")

    def test_too_long(self):
        with pytest.raises(SessionError):
            _validate_session_name("a" * 65)

class TestSessionCreate:
    @pytest.mark.asyncio
    async def test_create_session(self, session_mgr, mock_client, device):
        session = await session_mgr.create(
            mock_client, device, "/state/port",
            mode="STREAM", stream_mode="SAMPLE",
            sample_interval="10s", session_name="test-sub"
        )
        assert session.name == "test-sub"
        assert session.pid == 12345
        assert session.status == SESSION_STATUS_RUNNING
        assert Path(session.metadata_file).exists()

    @pytest.mark.asyncio
    async def test_duplicate_session(self, session_mgr, mock_client, device):
        await session_mgr.create(mock_client, device, "/state", mode="STREAM", session_name="dup")
        with pytest.raises(SessionError, match="already running"):
            await session_mgr.create(mock_client, device, "/state", mode="STREAM", session_name="dup")

    @pytest.mark.asyncio
    async def test_auto_generated_name(self, session_mgr, mock_client, device):
        session = await session_mgr.create(mock_client, device, "/state", mode="STREAM")
        assert session.name.startswith("sub-")

class TestSessionTail:
    @pytest.mark.asyncio
    async def test_tail_output(self, session_mgr, mock_client, device):
        session = await session_mgr.create(
            mock_client, device, "/state", mode="STREAM", session_name="tail-test"
        )
        with open(session.output_file, "w") as f:
            for i in range(30):
                f.write(json.dumps({"index": i}) + "\n")

        result = await session_mgr.tail("tail-test", lines=10)
        lines = result.strip().split("\n")
        assert len(lines) == 10
        assert json.loads(lines[-1])["index"] == 29

class TestSessionList:
    @pytest.mark.asyncio
    async def test_list_sessions(self, session_mgr, mock_client, device):
        await session_mgr.create(mock_client, device, "/a", mode="STREAM", session_name="sess-a")
        s2 = await session_mgr.create(mock_client, device, "/b", mode="STREAM", session_name="sess-b")
        s2.status = SESSION_STATUS_STOPPED

        all_sessions = session_mgr.list()
        assert len(all_sessions) >= 2

        active = session_mgr.list(active_only=True)
        assert any(s["name"] == "sess-a" for s in active)
