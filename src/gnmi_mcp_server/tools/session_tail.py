"""MCP tool: gnmi_session_tail -- read recent data from a subscribe session."""


def register_session_tail_tool(server, session_mgr):
    """Register the gnmi_session_tail tool."""

    @server.tool()
    async def gnmi_session_tail(session_name: str, lines: int = 20) -> str:
        """Read the most recent telemetry data from a subscribe session.

        Args:
            session_name: Session name to read from.
            lines: Number of recent data lines (default 20, max 500).
        """
        try:
            data = await session_mgr.tail(session_name, lines)
            if not data:
                return f"No data available for session '{session_name}' yet."
            return data
        except Exception as e:
            return f"Error: {e}"
