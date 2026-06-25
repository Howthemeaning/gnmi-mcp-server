"""MCP tool: gnmi_set -- modify device configuration with safety guardrails.

Two-phase flow:
  1. dry_run=true (default) -> returns diff preview + confirm_token
  2. confirm=<token> -> actually applies the change

Token: secrets.token_urlsafe(32), bound to hash(operations), 10min TTL, one-time use.
"""

import hashlib
import json
import logging
import secrets
import time

logger = logging.getLogger(__name__)

_pending_confirms: dict = {}
_CONFIRM_TTL = 600  # 10 minutes


def _ops_hash(operations: list) -> str:
    """Compute a deterministic hash of the operations list for token binding.

    Sort operations by their canonical form so the hash is order-independent
    (gNMI set ops are commutative for the final device state).
    """
    sorted_ops = sorted(operations, key=lambda op: json.dumps(op, sort_keys=True))
    canonical = json.dumps(sorted_ops, sort_keys=True)
    return hashlib.sha256(canonical.encode()).hexdigest()


def _generate_token(ops_hash_val: str) -> str:
    """Generate a confirm token bound to an operations hash."""
    token = secrets.token_urlsafe(32)
    _pending_confirms[token] = {
        "ops_hash": ops_hash_val,
        "expires_at": time.time() + _CONFIRM_TTL,
        "used": False,
    }
    return token


def _validate_and_consume_token(token: str, current_ops_hash: str) -> bool:
    """Validate a confirm token and mark it as used."""
    now = time.time()
    expired = [t for t, v in _pending_confirms.items() if v["expires_at"] < now]
    for t in expired:
        del _pending_confirms[t]

    entry = _pending_confirms.get(token)
    if not entry:
        return False
    if entry["used"]:
        return False
    if entry["expires_at"] < now:
        del _pending_confirms[token]
        return False
    if entry["ops_hash"] != current_ops_hash:
        return False

    entry["used"] = True
    return True


def register_set_tool(server, gnmic_client, config):
    """Register the gnmi_set tool (skipped if GNMI_READ_ONLY=true)."""

    @server.tool()
    async def gnmi_set(
        operations: list,
        target: str = "",
        address: str = "",
        dry_run: bool = True,
        confirm: str = "",
        insecure: bool = False,
        tls_ca: str = "",
        tls_cert: str = "",
        tls_key: str = "",
    ) -> str:
        """Modify gNMI device configuration (update, replace, or delete paths).

        Safety: By default (dry_run=true), this PREVIEWS changes without applying them.
        Review the preview, then call again with confirm=<token> to actually apply.

        operations: List of operations. Each has:
          - op: "update", "replace", or "delete"
          - path: gNMI path to modify
          - value: value to set (not needed for delete)

        Args:
            operations: List of set operations.
            target: Device alias from GNMI_DEVICES config.
            address: Direct host:port (requires GNMI_ALLOW_ARBITRARY=true).
            dry_run: Preview only, do not apply (default true).
            confirm: Token from previous dry_run to confirm execution.
            insecure: Skip TLS verification.
            tls_ca: TLS CA certificate path.
            tls_cert: TLS client certificate path.
            tls_key: TLS client key path.
        """
        from gnmi_mcp_server.tools._common import resolve_device, build_extra_args, validate_path

        # Validate operations
        if not operations or not isinstance(operations, list):
            return "Error: 'operations' must be a non-empty list."

        valid_ops = {"update", "replace", "delete"}
        for i, op in enumerate(operations):
            if not isinstance(op, dict):
                return f"Error: Operation {i} must be an object."
            if op.get("op") not in valid_ops:
                return f"Error: Operation {i} invalid op: {op.get('op')}. Must be: update, replace, delete."
            if not op.get("path"):
                return f"Error: Operation {i} missing 'path'."
            if op["op"] != "delete" and "value" not in op:
                return f"Error: Operation {i} ({op['op']}) requires a 'value'."
            try:
                validate_path(op["path"])
            except ValueError as e:
                return f"Error in operation {i}: {e}"

        try:
            device = resolve_device(config, target, address)
        except ValueError as e:
            return str(e)

        current_hash = _ops_hash(operations)

        if confirm:
            if not _validate_and_consume_token(confirm, current_hash):
                return (
                    "Error: Invalid or expired confirm token. "
                    "The token may have expired (10min TTL), already been used, "
                    "or the operations have changed since dry_run. "
                    "Run again without confirm to get a new preview."
                )

        elif dry_run:
            extra_args = build_extra_args(device, insecure, tls_ca, tls_cert, tls_key)
            extra_args.append("--dry-run")
            for op in operations:
                if op["op"] == "update":
                    extra_args.extend(["--update-path", op["path"]])
                    extra_args.extend(["--update-value", str(op.get("value", ""))])
                elif op["op"] == "replace":
                    extra_args.extend(["--replace-path", op["path"]])
                    extra_args.extend(["--replace-value", str(op.get("value", ""))])
                elif op["op"] == "delete":
                    extra_args.append(op["path"])

            result = await gnmic_client.run("set", extra_args, device)

            token = _generate_token(current_hash)

            preview = {
                "mode": "dry_run",
                "operations": operations,
                "device": device.address,
                "result": result.stdout if result.is_success else result.error_message,
                "confirm_token": token,
                "expires_in_seconds": _CONFIRM_TTL,
                "instruction": (
                    "Review the changes above. To apply, call gnmi_set again "
                    f"with the SAME operations and confirm='{token}'."
                ),
            }
            return json.dumps(preview, indent=2)
        else:
            return (
                "Error: Safety guard active. Set operations require confirmation.\n"
                "1. First call gnmi_set without 'confirm' to get a preview and token.\n"
                "2. Review the preview, then call again with confirm='<token>' to apply."
            )

        # ⬇ Only reached with valid confirm token ⬇
        # Execute set operation
        extra_args = build_extra_args(device, insecure, tls_ca, tls_cert, tls_key)
        for op in operations:
            if op["op"] == "update":
                extra_args.extend(["--update-path", op["path"]])
                extra_args.extend(["--update-value", str(op.get("value", ""))])
            elif op["op"] == "replace":
                extra_args.extend(["--replace-path", op["path"]])
                extra_args.extend(["--replace-value", str(op.get("value", ""))])
            elif op["op"] == "delete":
                extra_args.append(op["path"])

        result = await gnmic_client.run("set", extra_args, device)

        if result.is_success:
            return json.dumps({"status": "applied", "result": result.stdout}, indent=2)
        else:
            return f"Error: {result.error_message}"
