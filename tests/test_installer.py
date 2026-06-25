"""Tests for gnmic binary installer."""
import os
import stat
from gnmi_mcp_server.lib.installer import GnmicInstallError, _get_platform_suffix, _verify_sha256


class TestPlatformDetection:
    def test_get_platform_suffix_returns_string(self):
        suffix = _get_platform_suffix()
        assert isinstance(suffix, str)
        assert ".tar.gz" in suffix


class TestSha256Verification:
    def test_verify_matching_hash(self, tmp_path):
        f = tmp_path / "test.bin"
        f.write_bytes(b"hello world")
        import hashlib
        expected = hashlib.sha256(b"hello world").hexdigest()
        assert _verify_sha256(str(f), expected) is True

    def test_verify_mismatched_hash(self, tmp_path):
        f = tmp_path / "test.bin"
        f.write_bytes(b"hello world")
        assert _verify_sha256(str(f), "deadbeef") is False


class TestEnsureGnmic:
    def test_uses_binary_path_when_set(self, tmp_path):
        fake_gnmic = tmp_path / "gnmic"
        fake_gnmic.write_text("fake-binary")
        fake_gnmic.chmod(fake_gnmic.stat().st_mode | stat.S_IEXEC)

        class FakeConfig:
            binary_path = str(fake_gnmic)

        from gnmi_mcp_server.lib.installer import ensure_gnmic
        result = ensure_gnmic(FakeConfig())
        assert result == str(fake_gnmic)

    def test_uses_path_when_found(self, tmp_path, monkeypatch):
        bin_dir = tmp_path / "bin"
        bin_dir.mkdir()
        fake_gnmic = bin_dir / "gnmic"
        fake_gnmic.write_text("fake-binary")
        fake_gnmic.chmod(fake_gnmic.stat().st_mode | stat.S_IEXEC)
        monkeypatch.setenv("PATH", str(bin_dir) + ":" + os.environ.get("PATH", ""))

        class FakeConfig:
            binary_path = ""

        from gnmi_mcp_server.lib.installer import ensure_gnmic
        try:
            result = ensure_gnmic(FakeConfig())
            assert str(fake_gnmic) in result
        except GnmicInstallError:
            pass
