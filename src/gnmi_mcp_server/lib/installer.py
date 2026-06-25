"""Automatic gnmic binary download and verification."""

import hashlib
import logging
import os
import platform
import shutil
import stat
import tarfile
import tempfile
from pathlib import Path
import httpx

logger = logging.getLogger(__name__)

GITHUB_API = "https://api.github.com/repos/openconfig/gnmic/releases"


class GnmicInstallError(Exception):
    """gnmic binary installation failed."""
    pass


def _get_platform_suffix() -> str:
    """Get the platform-specific asset name suffix for gnmic downloads."""
    system = platform.system().lower()
    machine = platform.machine().lower()

    if system == "darwin":
        os_name = "Darwin"
    elif system == "linux":
        os_name = "Linux"
    else:
        raise GnmicInstallError(f"Unsupported platform: {system}")

    arch_map = {
        "x86_64": "x86_64",
        "amd64": "x86_64",
        "arm64": "arm64",
        "aarch64": "arm64",
    }
    arch = arch_map.get(machine, machine)

    return f"{os_name}_{arch}.tar.gz"


async def _fetch_latest_release(client: httpx.AsyncClient, version: str) -> dict:
    """Fetch release info from GitHub API."""
    url = f"{GITHUB_API}/{version}" if version != "latest" else f"{GITHUB_API}/latest"
    resp = await client.get(url, follow_redirects=True)
    resp.raise_for_status()
    return resp.json()


def _verify_sha256(filepath: str, expected_hash: str) -> bool:
    """Verify file SHA256 digest."""
    sha256 = hashlib.sha256()
    with open(filepath, "rb") as f:
        for chunk in iter(lambda: f.read(8192), b""):
            sha256.update(chunk)
    actual = sha256.hexdigest()
    return actual == expected_hash


async def _download_and_extract(client: httpx.AsyncClient, download_url: str,
                                 download_dir: str, checksum_url: str = "") -> str:
    """Download the gnmic tarball, verify checksum, extract, return binary path."""
    os.makedirs(download_dir, exist_ok=True)

    logger.info(f"Downloading gnmic from {download_url}...")
    resp = await client.get(download_url)
    resp.raise_for_status()

    with tempfile.NamedTemporaryFile(suffix=".tar.gz", delete=False) as tmp:
        tmp.write(resp.content)
        tarball_path = tmp.name

    try:
        if checksum_url:
            logger.info("Verifying SHA256 checksum...")
            sha_resp = await client.get(checksum_url)
            sha_resp.raise_for_status()
            for line in sha_resp.text.splitlines():
                parts = line.strip().split()
                if len(parts) >= 2:
                    expected_hash, filename = parts[0], parts[1]
                    if filename == os.path.basename(
                        download_url.split("/")[-1].split("?")[0]
                    ):
                        if not _verify_sha256(tarball_path, expected_hash):
                            raise GnmicInstallError(
                                f"SHA256 verification failed for {filename}"
                            )
                        break
            else:
                logger.warning("No matching checksum entry found, skipping verification")

        logger.info("Extracting gnmic...")
        with tarfile.open(tarball_path, "r:gz") as tar:
            for member in tar.getmembers():
                if member.name.endswith("gnmic") or member.name == "gnmic":
                    member.name = "gnmic"
                    tar.extract(member, download_dir)
                    break

        binary_path = os.path.join(download_dir, "gnmic")
        st = os.stat(binary_path)
        os.chmod(binary_path, st.st_mode | stat.S_IEXEC)
        logger.info(f"gnmic installed to {binary_path}")
        return binary_path
    finally:
        os.unlink(tarball_path)


def ensure_gnmic(config) -> str:
    """Ensure gnmic binary is available. Returns path to gnmic binary.

    Checks in order:
    1. config.binary_path (if set and exists)
    2. $PATH lookup via shutil.which
    3. Download from GitHub Releases

    Raises:
        GnmicInstallError: If gnmic cannot be found or installed.
    """
    if config.binary_path and os.path.isfile(config.binary_path):
        logger.info(f"Using gnmic from config: {config.binary_path}")
        return os.path.abspath(config.binary_path)

    which_result = shutil.which("gnmic")
    if which_result:
        logger.info(f"Found gnmic in PATH: {which_result}")
        return which_result

    import asyncio
    download_dir = os.path.expanduser(config.download_dir)
    cached = os.path.join(download_dir, "gnmic")
    if os.path.isfile(cached):
        logger.info(f"Using cached gnmic: {cached}")
        return cached

    async def _download():
        async with httpx.AsyncClient(timeout=300.0) as client:
            release = await _fetch_latest_release(client, config.version)
            suffix = _get_platform_suffix()

            asset_url = None
            checksum_url = None
            for asset in release.get("assets", []):
                name = asset["name"]
                if "checksums" in name.lower():
                    checksum_url = asset["browser_download_url"]
                elif suffix in name:
                    asset_url = asset["browser_download_url"]

            if not asset_url:
                available = [a["name"] for a in release.get("assets", [])]
                raise GnmicInstallError(
                    f"No gnmic binary found for platform '{suffix}'. "
                    f"Available assets: {available}"
                )

            return await _download_and_extract(
                client, asset_url, download_dir, checksum_url
            )

    try:
        return asyncio.run(_download())
    except GnmicInstallError:
        raise
    except Exception as e:
        raise GnmicInstallError(
            f"Failed to install gnmic: {e}. "
            f"You can manually install gnmic from https://github.com/openconfig/gnmic/releases "
            f"and set GNMI_BINARY_PATH to its location."
        )
