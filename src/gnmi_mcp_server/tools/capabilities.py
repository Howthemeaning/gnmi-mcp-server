"""MCP tool: gnmi_capabilities -- query gNMI device capabilities."""

import time
import logging

logger = logging.getLogger(__name__)

_capabilities_cache: dict = {}


def register_capabilities_tool(server, gnmic_client, config):
    """Register the gnmi_capabilities tool."""

    @server.tool()
    async def gnmi_capabilities(
        target: str = "",
        address: str = "",
        insecure: bool = False,
        tls_ca: str = "",
        tls_cert: str = "",
        tls_key: str = "",
    ) -> str:
        """Query gNMI device capabilities (version, supported models, encodings)."""
        from gnmi_mcp_server.tools._common import resolve_device, build_extra_args

        try:
            device = resolve_device(config, target, address)
        except ValueError as e:
            return str(e)

        # 对 adhoc 设备（无预定义 name）用 address 区分缓存
        name_key = device.address if device.name == "__adhoc__" else device.name
        cache_key = (name_key, "JSON_IETF", insecure)
        if cache_key in _capabilities_cache:
            cached_result, expiry = _capabilities_cache[cache_key]
            if time.time() < expiry:
                return cached_result

        extra_args = build_extra_args(device, insecure, tls_ca, tls_cert, tls_key)
        result = await gnmic_client.run("capabilities", extra_args, device)

        if result.is_success:
            output = result.stdout
            _capabilities_cache[cache_key] = (output, time.time() + 300)
            return output
        else:
            return f"Error: {result.error_message}"
