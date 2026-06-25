"""MCP tool: gnmi_get -- retrieve configuration or state data."""

import json
import logging

logger = logging.getLogger(__name__)


def _truncate_json_boundary(text: str, max_bytes: int) -> str:
    """Truncate text at a clean JSON boundary within max_bytes."""
    encoded = text.encode("utf-8")
    if len(encoded) <= max_bytes:
        return text

    truncated = encoded[:max_bytes].decode("utf-8", errors="ignore")
    last_brace = truncated.rfind("}")
    last_bracket = truncated.rfind("]")

    if last_brace > 0 or last_bracket > 0:
        cut = max(last_brace, last_bracket)
        return truncated[:cut + 1] + "\n"

    last_newline = truncated.rfind("\n")
    if last_newline > 0:
        return truncated[:last_newline]

    return truncated


def register_get_tool(server, gnmic_client, config):
    """Register the gnmi_get tool."""

    @server.tool()
    async def gnmi_get(
        path: str,
        target: str = "",
        address: str = "",
        type: str = "ALL",
        prefix: str = "",
        max_bytes: int = 65536,
        insecure: bool = False,
        tls_ca: str = "",
        tls_cert: str = "",
        tls_key: str = "",
    ) -> str:
        """Retrieve configuration or state data from a gNMI device."""
        from gnmi_mcp_server.tools._common import resolve_device, build_extra_args, validate_path

        try:
            validate_path(path)
            device = resolve_device(config, target, address)
        except ValueError as e:
            return str(e)

        extra_args = build_extra_args(device, insecure, tls_ca, tls_cert, tls_key)
        if prefix:
            extra_args.extend(["--prefix", prefix])
        if type != "ALL":
            extra_args.extend(["--type", type])
        extra_args.extend(["--path", path])

        # Retry once on network error (GET is idempotent)
        for attempt in range(2):
            result = await gnmic_client.run("get", extra_args, device)
            if result.is_success:
                break
            if attempt == 0 and ("timeout" in result.error_message.lower() or
                                  "connection refused" in result.error_message.lower()):
                logger.warning(f"GET retry {attempt + 1} after: {result.error_message}")
                import asyncio
                await asyncio.sleep(1)
                continue
            return f"Error: {result.error_message}"

        output = result.stdout

        if len(output.encode("utf-8")) > max_bytes:
            truncated = _truncate_json_boundary(output, max_bytes)
            orig_size = len(output.encode("utf-8"))
            return json.dumps({
                "data": truncated,
                "truncated": True,
                "original_bytes": orig_size,
                "hint": f"Output truncated at {max_bytes} bytes. Use a more specific path or increase max_bytes."
            })

        return output
