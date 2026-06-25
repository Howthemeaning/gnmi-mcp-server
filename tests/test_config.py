import os
import json
import pytest
from gnmi_mcp_server.lib.config import DeviceConfig, AppConfig, load_config, ConfigError

class TestDeviceConfig:
    def test_valid_device(self):
        d = DeviceConfig(name="sw1", address="10.0.0.1:57400", username="admin", password="pass")
        assert d.name == "sw1"
        assert d.address == "10.0.0.1:57400"

    def test_invalid_address_format(self):
        with pytest.raises(Exception):
            DeviceConfig(name="sw1", address="not-a-valid-address", username="u", password="p")

class TestLoadConfig:
    def test_minimal_config(self, monkeypatch):
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "sw1", "address": "10.0.0.1:57400"}
        ]))
        monkeypatch.setenv("GNMI_USER_SW1", "admin")
        monkeypatch.setenv("GNMI_PASS_SW1", "secret")
        config = load_config()
        assert "sw1" in config.devices
        assert config.devices["sw1"].address == "10.0.0.1:57400"
        assert config.devices["sw1"].username == "admin"
        assert config.devices["sw1"].password == "secret"

    def test_missing_credentials(self, monkeypatch):
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "sw1", "address": "10.0.0.1:57400"}
        ]))
        with pytest.raises(ConfigError, match="GNMI_USER_SW1"):
            load_config()

    def test_default_values(self, monkeypatch):
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "sw1", "address": "10.0.0.1:57400"}
        ]))
        monkeypatch.setenv("GNMI_USER_SW1", "admin")
        monkeypatch.setenv("GNMI_PASS_SW1", "secret")
        config = load_config()
        assert config.data_dir.endswith(".gnmi-mcp/data")
        assert config.read_only is False
        assert config.allow_arbitrary is False
        assert config.log_level == "INFO"

    def test_name_collision(self, monkeypatch):
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "core-switch", "address": "10.0.0.1:57400"},
            {"name": "core_switch", "address": "10.0.0.2:57400"}
        ]))
        monkeypatch.setenv("GNMI_USER_CORE_SWITCH", "admin")
        monkeypatch.setenv("GNMI_PASS_CORE_SWITCH", "secret")
        with pytest.raises(ConfigError, match="CORE_SWITCH"):
            load_config()

    def test_name_to_env_convert(self, monkeypatch):
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "my-device", "address": "10.0.0.1:57400"}
        ]))
        monkeypatch.setenv("GNMI_USER_MY_DEVICE", "admin")
        monkeypatch.setenv("GNMI_PASS_MY_DEVICE", "secret")
        config = load_config()
        assert "my-device" in config.devices

    def test_read_only_mode(self, monkeypatch):
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "sw1", "address": "10.0.0.1:57400"}
        ]))
        monkeypatch.setenv("GNMI_USER_SW1", "admin")
        monkeypatch.setenv("GNMI_PASS_SW1", "secret")
        monkeypatch.setenv("GNMI_READ_ONLY", "true")
        config = load_config()
        assert config.read_only is True

    def test_tls_dir_validation(self, monkeypatch, tmp_path):
        cert_dir = tmp_path / "certs"
        cert_dir.mkdir()
        (cert_dir / "ca.pem").write_text("fake-cert")
        monkeypatch.setenv("GNMI_TLS_DIR", str(cert_dir))
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "sw1", "address": "10.0.0.1:57400", "tls_ca": "ca.pem"}
        ]))
        monkeypatch.setenv("GNMI_USER_SW1", "admin")
        monkeypatch.setenv("GNMI_PASS_SW1", "secret")
        config = load_config()
        assert config.devices["sw1"].tls_ca == "ca.pem"

    def test_tls_dir_rejects_path_outside(self, monkeypatch, tmp_path):
        cert_dir = tmp_path / "certs"
        cert_dir.mkdir()
        monkeypatch.setenv("GNMI_TLS_DIR", str(cert_dir))
        monkeypatch.setenv("GNMI_DEVICES", json.dumps([
            {"name": "sw1", "address": "10.0.0.1:57400", "tls_ca": "/etc/passwd"}
        ]))
        monkeypatch.setenv("GNMI_USER_SW1", "admin")
        monkeypatch.setenv("GNMI_PASS_SW1", "secret")
        with pytest.raises(ConfigError, match="tls_ca"):
            load_config()
