"""gnmi-mcp: MCP server for gNMI network device management.

Entry point: main() -- loads config, initializes components, registers tools,
starts MCP stdio server.
"""

import asyncio
import logging
import signal
import sys
from pathlib import Path

from mcp.server import Server
from mcp.server.stdio import stdio_server

from gnmi_mcp_server.lib.config import load_config, ConfigError
from gnmi_mcp_server.lib.installer import ensure_gnmic, GnmicInstallError
from gnmi_mcp_server.lib.gnmic import GnmicClient
from gnmi_mcp_server.lib.session import SessionManager

from gnmi_mcp_server.tools.capabilities import register_capabilities_tool
from gnmi_mcp_server.tools.get import register_get_tool
from gnmi_mcp_server.tools.set import register_set_tool
from gnmi_mcp_server.tools.subscribe import register_subscribe_tool
from gnmi_mcp_server.tools.session_list import register_session_list_tool
from gnmi_mcp_server.tools.session_stop import register_session_stop_tool
from gnmi_mcp_server.tools.session_tail import register_session_tail_tool
from gnmi_mcp_server.tools.path import register_path_tool

logger = logging.getLogger("gnmi-mcp")


def setup_logging(config):
    """Configure logging with rotation."""
    log_dir = Path(config.data_dir) / "logs"
    log_dir.mkdir(parents=True, exist_ok=True)

    from logging.handlers import RotatingFileHandler

    handler = RotatingFileHandler(
        str(log_dir / "gnmi-mcp.log"),
        maxBytes=config.log_max_size,
        backupCount=config.log_backup_count,
    )
    handler.setFormatter(logging.Formatter(
        "%(asctime)s [%(levelname)s] %(name)s: %(message)s"
    ))

    root_logger = logging.getLogger()
    root_logger.addHandler(handler)
    root_logger.setLevel(getattr(logging, config.log_level.upper(), logging.INFO))

    stderr_handler = logging.StreamHandler(sys.stderr)
    stderr_handler.setFormatter(logging.Formatter("[%(levelname)s] %(message)s"))
    root_logger.addHandler(stderr_handler)


def main():
    """Entry point for gnmi-mcp MCP server."""
    try:
        config = load_config()
    except ConfigError as e:
        print(f"Configuration error: {e}", file=sys.stderr)
        sys.exit(1)

    setup_logging(config)
    logger.info(f"gnmi-mcp starting (read_only={config.read_only})")
    logger.info(f"Configured devices: {', '.join(config.devices.keys())}")

    try:
        gnmic_path = ensure_gnmic(config)
    except GnmicInstallError as e:
        logger.error(f"gnmic installation failed: {e}")
        sys.exit(1)

    gnmic_client = GnmicClient(gnmic_path)
    sessions_dir = Path(config.data_dir) / "sessions"
    session_mgr = SessionManager(str(sessions_dir))

    server = Server("gnmi-mcp")

    # Register all tools
    register_capabilities_tool(server, gnmic_client, config)
    register_get_tool(server, gnmic_client, config)

    if not config.read_only:
        register_set_tool(server, gnmic_client, config)
        logger.info("gnmi_set tool registered")
    else:
        logger.info("gnmi_set tool DISABLED (GNMI_READ_ONLY=true)")

    register_subscribe_tool(server, gnmic_client, session_mgr, config)
    register_session_list_tool(server, session_mgr)
    register_session_stop_tool(server, gnmic_client, session_mgr)
    register_session_tail_tool(server, session_mgr)

    if config.yang_dir:
        register_path_tool(server, gnmic_client, config)
        logger.info(f"gnmi_path tool registered (yang_dir={config.yang_dir})")

    logger.info("All tools registered. Starting MCP stdio server.")

    async def run_server():
        async with stdio_server() as (read_stream, write_stream):
            await server.run(
                read_stream,
                write_stream,
                server.create_initialization_options(),
            )

    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)

    def shutdown():
        logger.info("Shutting down...")
        loop.create_task(session_mgr.cleanup(gnmic_client))

    for sig in (signal.SIGTERM, signal.SIGINT):
        try:
            loop.add_signal_handler(sig, shutdown)
        except NotImplementedError:
            pass

    try:
        loop.run_until_complete(run_server())
    except KeyboardInterrupt:
        pass
    finally:
        loop.run_until_complete(session_mgr.cleanup(gnmic_client))
        loop.close()
        logger.info("gnmi-mcp stopped.")


if __name__ == "__main__":
    main()
