"""Tests for shared tool helpers."""
import json
import pytest
from unittest.mock import MagicMock
from gnmi_mcp_server.tools._common import resolve_device, build_extra_args, validate_path
from gnmi_mcp_server.lib.config import DeviceConfig


class TestValidatePath:
    def test_valid_path(self):
        assert validate_path("/state/system") == "/state/system"

    def test_empty_path(self):
        with pytest.raises(ValueError, match="empty"):
            validate_path("")

    def test_no_leading_slash(self):
        with pytest.raises(ValueError, match="start with '/'"):
            validate_path("state/system")

    def test_parent_traversal(self):
        with pytest.raises(ValueError, match="contain '..'"):
            validate_path("/state/../system")

    def test_null_bytes(self):
        with pytest.raises(ValueError, match="null"):
            validate_path("/state\x00/system")


class TestResolveDevice:
    def test_resolve_by_target(self):
        config = MagicMock()
        device = DeviceConfig(name="sw1", address="10.0.0.1:57400", username="admin", password="pass")
        config.devices = {"sw1": device}
        result = resolve_device(config, "sw1", "")
        assert result.name == "sw1"

    def test_target_not_found(self):
        config = MagicMock()
        config.devices = {}
        with pytest.raises(ValueError, match="not found"):
            resolve_device(config, "sw1", "")

    def test_both_target_and_address(self):
        config = MagicMock()
        with pytest.raises(ValueError, match="not both"):
            resolve_device(config, "sw1", "10.0.0.1:57400")

    def test_address_blocked(self):
        config = MagicMock()
        config.allow_arbitrary = False
        with pytest.raises(ValueError, match="disabled"):
            resolve_device(config, "", "10.0.0.1:57400")


class TestBuildExtraArgs:
    def test_basic_args(self):
        device = DeviceConfig(name="sw1", address="10.0.0.1:57400", username="a", password="b")
        args = build_extra_args(device, False, "", "", "")
        assert "-a" in args
        assert "10.0.0.1:57400" in args

    def test_insecure_flag(self):
        device = DeviceConfig(name="sw1", address="10.0.0.1:57400", username="a", password="b", insecure=True)
        args = build_extra_args(device, False, "", "", "")
        assert "--insecure" in args
