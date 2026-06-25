"""MCP tool: gnmi_path -- browse YANG model paths (optional, only if GNMI_YANG_DIR set)."""


def register_path_tool(server, gnmic_client, config):
    """Register the gnmi_path tool (only called when GNMI_YANG_DIR is configured)."""

    @server.tool()
    async def gnmi_path(
        yang_file: str = "",
        yang_dir: str = "",
        model: str = "",
        search: str = "",
    ) -> str:
        """Browse gNMI paths from YANG model files.

        Helpful for discovering available data paths when you don't know
        the exact gNMI path to query.

        Args:
            yang_file: Single YANG model file (within GNMI_YANG_DIR).
            yang_dir: YANG model directory (within GNMI_YANG_DIR).
            model: Filter by model name.
            search: Filter results by keyword (client-side filtering).
        """
        import os

        extra_args = []
        if model:
            extra_args.extend(["--model", model])
        if yang_file:
            full_path = os.path.join(config.yang_dir, yang_file)
            # 防路径遍历：realpath 解析后必须在 GNMI_YANG_DIR 内
            from gnmi_mcp_server.lib.config import _within_dir
            if not os.path.isfile(full_path):
                return f"Error: YANG file '{yang_file}' not found in GNMI_YANG_DIR."
            if not _within_dir(full_path, config.yang_dir):
                return f"Error: YANG file '{yang_file}' is outside GNMI_YANG_DIR."
            extra_args.extend(["--file", full_path])
        if yang_dir:
            full_dir = os.path.join(config.yang_dir, yang_dir)
            from gnmi_mcp_server.lib.config import _within_dir
            if not os.path.isdir(full_dir):
                return f"Error: YANG dir '{yang_dir}' not found in GNMI_YANG_DIR."
            if not _within_dir(full_dir, config.yang_dir):
                return f"Error: YANG dir '{yang_dir}' is outside GNMI_YANG_DIR."
            extra_args.extend(["--dir", full_dir])

        from gnmi_mcp_server.lib.config import DeviceConfig
        fake_device = DeviceConfig(
            name="__path__", address="localhost:0",
            username="", password="",
        )

        result = await gnmic_client.run("path", extra_args, fake_device)

        if not result.is_success:
            return f"Error: {result.error_message}"

        output = result.stdout
        if search:
            lines = output.splitlines()
            filtered = [l for l in lines if search.lower() in l.lower()]
            return "\n".join(filtered)

        return output
