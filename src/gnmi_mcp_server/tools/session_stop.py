"""MCP tool: gnmi_session_stop -- stop a subscribe session."""


def register_session_stop_tool(server, gnmic_client, session_mgr):
    """Register the gnmi_session_stop tool."""

    @server.tool()
    async def gnmi_session_stop(session_name: str) -> str:
        """Stop a running gNMI subscribe session.

        Args:
            session_name: Name of the session to stop.
        """
        try:
            result = await session_mgr.stop(gnmic_client, session_name)
            if result:
                return f"Session '{session_name}' stopped."
            else:
                return f"Failed to stop session '{session_name}'."
        except Exception as e:
            return f"Error: {e}"
