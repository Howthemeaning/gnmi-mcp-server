"""MCP tool: gnmi_session_list -- list all subscribe sessions."""
import json


def register_session_list_tool(server, session_mgr):
    """Register the gnmi_session_list tool."""

    @server.tool()
    async def gnmi_session_list() -> str:
        """List all gNMI subscribe sessions and their statuses.

        Returns session name, PID, target, path, mode, status, and output size.
        """
        sessions = session_mgr.list()
        if not sessions:
            return "No subscribe sessions found."
        return json.dumps(sessions, indent=2)
