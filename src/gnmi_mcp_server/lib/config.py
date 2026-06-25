"""Configuration loading from environment variables."""

import json
import os
import re
from pathlib import Path
from typing import Optional
from pydantic import BaseModel, field_validator

class ConfigError(Exception):
    """Configuration validation error with actionable message."""
    pass

class DeviceConfig(BaseModel):
    """Single gNMI target device configuration."""
    name: str
    address: str
    username: str = ""
    password: str = ""
    insecure: bool = False
    tls_ca: str = ""
    tls_cert: str = ""
    tls_key: str = ""
    timeout: str = "30s"

    @field_validator("address")
    @classmethod
    def validate_address(cls, v: str) -> str:
        if not re.match(r'^[\w.-]+:\d+$', v):
            raise ValueError(f"Invalid address format: {v}. Expected 'host:port'")
        return v

    @field_validator("timeout")
    @classmethod
    def validate_timeout(cls, v: str) -> str:
        if not re.match(r'^\d+(s|m)$', v):
            raise ValueError(f"Invalid timeout format: {v}. Expected like '30s' or '1m'")
        return v

class AppConfig(BaseModel):
    """Application configuration loaded from environment variables."""
    devices: dict[str, DeviceConfig] = {}
    binary_path: str = ""
    version: str = "latest"
    download_dir: str = ""
    data_dir: str = ""
    log_level: str = "INFO"
    log_max_size: int = 10 * 1024 * 1024
    log_backup_count: int = 3
    read_only: bool = False
    allow_arbitrary: bool = False
    tls_dir: str = ""
    yang_dir: str = ""


def _env_to_name(name: str) -> str:
    """Convert device name to env var suffix: 'core-switch' -> 'CORE_SWITCH'."""
    return name.upper().replace("-", "_")


def _expand_path(path: str) -> str:
    """Expand ~ and environment variables in a path string."""
    return os.path.expanduser(os.path.expandvars(path))


def _within_dir(path: str, directory: str) -> bool:
    """Check if a resolved path is within a directory (prevents symlink escape)."""
    resolved = os.path.realpath(path)
    resolved_dir = os.path.realpath(directory)
    return resolved.startswith(resolved_dir + os.sep) or resolved == resolved_dir


def load_config() -> AppConfig:
    """Load all configuration from environment variables.

    Raises ConfigError on invalid or missing configuration.
    """
    home = Path.home()
    default_data_dir = str(home / ".gnmi-mcp" / "data")
    default_download_dir = str(home / ".gnmi-mcp" / "bin")

    devices_json = os.getenv("GNMI_DEVICES", "[]")
    try:
        raw_devices = json.loads(devices_json)
    except json.JSONDecodeError as e:
        raise ConfigError(f"GNMI_DEVICES is not valid JSON: {e}")

    if not isinstance(raw_devices, list):
        raise ConfigError("GNMI_DEVICES must be a JSON array")

    devices: dict[str, DeviceConfig] = {}
    seen_env_names: dict[str, str] = {}

    for item in raw_devices:
        if not isinstance(item, dict) or "name" not in item:
            raise ConfigError(f"Each entry in GNMI_DEVICES must have a 'name' field: {item}")

        name = item["name"]
        env_name = _env_to_name(name)

        if env_name in seen_env_names:
            raise ConfigError(
                f"Device name collision: '{name}' and '{seen_env_names[env_name]}' "
                f"both map to env var prefix '{env_name}'. Rename one device."
            )
        seen_env_names[env_name] = name

        username = os.getenv(f"GNMI_USER_{env_name}", "")
        password = os.getenv(f"GNMI_PASS_{env_name}", "")
        if not username:
            raise ConfigError(
                f"Environment variable GNMI_USER_{env_name} is required for device '{name}'. "
                f"Set it in your shell profile or MCP client config."
            )
        if not password:
            raise ConfigError(
                f"Environment variable GNMI_PASS_{env_name} is required for device '{name}'. "
                f"Set it in your shell profile or MCP client config."
            )

        try:
            device = DeviceConfig(
                name=name,
                address=item.get("address", ""),
                username=username,
                password=password,
                insecure=item.get("insecure", False),
                tls_ca=item.get("tls_ca", ""),
                tls_cert=item.get("tls_cert", ""),
                tls_key=item.get("tls_key", ""),
                timeout=item.get("timeout", "30s"),
            )
        except Exception as e:
            raise ConfigError(f"Invalid device config for '{name}': {e}")

        devices[name] = device

    tls_dir = os.getenv("GNMI_TLS_DIR", "")
    yang_dir = os.getenv("GNMI_YANG_DIR", "")

    if tls_dir:
        tls_dir = _expand_path(tls_dir)
        for device in devices.values():
            for field_name in ("tls_ca", "tls_cert", "tls_key"):
                field_value = getattr(device, field_name, "")
                if field_value:
                    full_path = os.path.join(tls_dir, field_value)
                    if not _within_dir(full_path, tls_dir):
                        raise ConfigError(
                            f"Device '{device.name}' {field_name}='{field_value}' "
                            f"is not within GNMI_TLS_DIR='{tls_dir}'"
                        )

    read_only = os.getenv("GNMI_READ_ONLY", "false").lower() in ("true", "1", "yes")
    allow_arbitrary = os.getenv("GNMI_ALLOW_ARBITRARY", "false").lower() in ("true", "1", "yes")

    return AppConfig(
        devices=devices,
        binary_path=os.getenv("GNMI_BINARY_PATH", ""),
        version=os.getenv("GNMI_VERSION", "latest"),
        download_dir=os.getenv("GNMI_DOWNLOAD_DIR", default_download_dir),
        data_dir=os.getenv("GNMI_DATA_DIR", default_data_dir),
        log_level=os.getenv("GNMI_LOG_LEVEL", "INFO"),
        log_max_size=int(os.getenv("GNMI_LOG_MAX_SIZE", str(10 * 1024 * 1024))),
        log_backup_count=int(os.getenv("GNMI_LOG_BACKUP_COUNT", "3")),
        read_only=read_only,
        allow_arbitrary=allow_arbitrary,
        tls_dir=tls_dir,
        yang_dir=yang_dir,
    )
