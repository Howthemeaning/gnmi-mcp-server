"""MCP tool: gnmi_subscribe -- subscribe to device telemetry."""

import json
import logging

logger = logging.getLogger(__name__)


def register_subscribe_tool(server, gnmic_client, session_mgr, config):
    """Register the gnmi_subscribe tool."""

    @server.tool()
    async def gnmi_subscribe(
        path: str,
        mode: str,  # ONCE / POLL / STREAM
        target: str = "",
        address: str = "",
        stream_mode: str = "TARGET_DEFINED",
        sample_interval: str = "",
        heartbeat_interval: str = "",
        session_name: str = "",
        timeout: int = 30,
        insecure: bool = False,
        tls_ca: str = "",
        tls_cert: str = "",
        tls_key: str = "",
    ) -> str:
        """Subscribe to gNMI device telemetry data.

        ONCE mode: One-time snapshot, returns immediately.
        STREAM/POLL mode: Starts background collection, use session tools to manage.

        Args:
            path: gNMI subscription path (e.g., /state/port/statistics).
            mode: ONCE (one-shot), POLL, or STREAM (continuous).
            target: Device alias from GNMI_DEVICES config.
            address: Direct host:port (requires GNMI_ALLOW_ARBITRARY=true).
            stream_mode: For STREAM: SAMPLE, ON_CHANGE, or TARGET_DEFINED.
            sample_interval: Sampling interval for SAMPLE mode (e.g., 10s).
            heartbeat_interval: Heartbeat interval (e.g., 30s).
            session_name: Name for STREAM/POLL sessions (auto-generated if empty).
            timeout: Timeout in seconds for ONCE mode (default 30).
            insecure: Skip TLS verification.
            tls_ca: TLS CA certificate path.
            tls_cert: TLS client certificate path.
            tls_key: TLS client key path.
        """
        from gnmi_mcp_server.tools._common import resolve_device, build_extra_args, validate_path

        try:
            validate_path(path)
            device = resolve_device(config, target, address)
        except ValueError as e:
            return str(e)

        mode_upper = mode.upper()
        valid_modes = {"ONCE", "POLL", "STREAM"}
        if mode_upper not in valid_modes:
            return f"Error: mode must be one of: {', '.join(sorted(valid_modes))}"

        if mode_upper == "ONCE":
            extra_args = build_extra_args(device, insecure, tls_ca, tls_cert, tls_key)
            extra_args.extend(["--path", path, "--mode", "once"])
            result = await gnmic_client.run("subscribe", extra_args, device, timeout=timeout)
            if result.is_success:
                return result.stdout
            else:
                return f"Error: {result.error_message}"

        # STREAM / POLL -- start background session
        try:
            session = await session_mgr.create(
                gnmic_client=gnmic_client,
                device=device,
                path=path,
                mode=mode_upper,
                stream_mode=stream_mode.upper() if stream_mode else "TARGET_DEFINED",
                sample_interval=sample_interval,
                heartbeat_interval=heartbeat_interval,
                session_name=session_name,
            )
            return json.dumps({
                "session_name": session.name,
                "pid": session.pid,
                "target": session.target,
                "path": session.path,
                "mode": session.mode,
                "status": session.status,
                "output_file": session.output_file,
                "instruction": (
                    f"Use gnmi_session_tail with session_name='{session.name}' to view data. "
                    f"Use gnmi_session_stop to end the session."
                ),
            }, indent=2)
        except Exception as e:
            return f"Error creating subscribe session: {e}"
