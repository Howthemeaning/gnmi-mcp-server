"""Tests for gnmi_set tool with dry-run/confirm safety flow."""
import json
import pytest
from unittest.mock import AsyncMock, MagicMock
from gnmi_mcp_server.tools.set import (
    _ops_hash, _generate_token, _validate_and_consume_token, _CONFIRM_TTL,
    _pending_confirms
)
from gnmi_mcp_server.lib.config import DeviceConfig
from gnmi_mcp_server.lib.gnmic import GnmicResult


class TestOpsHash:
    def test_deterministic(self):
        ops = [{"op": "update", "path": "/x", "value": "y"}]
        h1 = _ops_hash(ops)
        h2 = _ops_hash(ops)
        assert h1 == h2

    def test_order_independent(self):
        """gNMI set ops are commutative in final state; hash should be order-independent."""
        ops1 = [{"op": "update", "path": "/a"}, {"op": "delete", "path": "/b"}]
        ops2 = [{"op": "delete", "path": "/b"}, {"op": "update", "path": "/a"}]
        assert _ops_hash(ops1) == _ops_hash(ops2)


class TestTokenFlow:
    def test_generate_and_validate(self, monkeypatch):
        monkeypatch.setattr("time.time", lambda: 1000)
        h = "abc123"
        token = _generate_token(h)
        assert _validate_and_consume_token(token, h) is True

    def test_token_one_time_use(self, monkeypatch):
        monkeypatch.setattr("time.time", lambda: 1000)
        token = _generate_token("hash-x")
        assert _validate_and_consume_token(token, "hash-x") is True
        assert _validate_and_consume_token(token, "hash-x") is False

    def test_token_binding(self, monkeypatch):
        monkeypatch.setattr("time.time", lambda: 1000)
        token = _generate_token("hash-a")
        assert _validate_and_consume_token(token, "hash-b") is False

    def test_token_expiry(self, monkeypatch):
        monkeypatch.setattr("time.time", lambda: 0)
        token = _generate_token("hash-x")
        monkeypatch.setattr("time.time", lambda: _CONFIRM_TTL + 1)
        assert _validate_and_consume_token(token, "hash-x") is False
