"""Shared helper functions for MCP tool handlers."""


def resolve_device(config, target: str, address: str):
    """Resolve a DeviceConfig from target name or address."""
    if target and address:
        raise ValueError("Specify 'target' or 'address', not both.")
    if target:
        device = config.devices.get(target)
        if not device:
            available = ", ".join(config.devices.keys())
            raise ValueError(f"Target '{target}' not found in GNMI_DEVICES. Available: {available}")
        return device
    if address:
        if not config.allow_arbitrary:
            raise ValueError(
                "Direct 'address' connections are disabled. "
                "Set GNMI_ALLOW_ARBITRARY=true or use a predefined 'target'."
            )
        from gnmi_mcp_server.lib.config import DeviceConfig
        return DeviceConfig(name="__adhoc__", address=address, username="", password="")
    raise ValueError("Either 'target' or 'address' must be specified.")


def build_extra_args(device, insecure: bool, tls_ca: str, tls_cert: str, tls_key: str) -> list[str]:
    """Build common gnmic connection flags from device config and tool params."""
    args = ["-a", device.address]
    if insecure or device.insecure:
        args.append("--insecure")
    # When not insecure, let gnmic use TLS with system/default CA.
    # No --skip-verify unless explicitly configured via future GNMI_SKIP_VERIFY env var.
    ca = tls_ca or device.tls_ca
    cert = tls_cert or device.tls_cert
    key = tls_key or device.tls_key
    if ca:
        args.extend(["--tls-ca", ca])
    if cert:
        args.extend(["--tls-cert", cert])
    if key:
        args.extend(["--tls-key", key])
    return args


def validate_path(path: str) -> str:
    """Validate and normalize a gNMI path."""
    if not path:
        raise ValueError("Path must not be empty.")
    if not path.startswith("/"):
        raise ValueError("Path must start with '/'.")
    if ".." in path:
        raise ValueError("Path must not contain '..'.")
    if "\x00" in path:
        raise ValueError("Path must not contain null bytes.")
    if any(ord(c) < 32 and c not in "\t\n\r" for c in path):
        raise ValueError("Path contains non-printable characters.")
    return path
