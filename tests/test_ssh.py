"""The installer's real SSH code against a real SSH server set up like a Savant Mac host."""

from __future__ import annotations

import asyncssh
import pytest
from homeassistant.core import HomeAssistant

from custom_components.savant.installer import AuthFailed, HostKeyChanged, Installer, generate_key

PASSWORD = "test-password"


class MacLikeServer(asyncssh.SSHServer):
    """Keyboard-interactive "Password:" prompts and public keys, like macOS sshd."""

    authorized: list[asyncssh.SSHKey] = []

    def begin_auth(self, username: str) -> bool:
        return True

    def password_auth_supported(self) -> bool:
        return False

    def kbdint_auth_supported(self) -> bool:
        return True

    def get_kbdint_challenge(self, username, lang, submethods):
        return "", "", "", [("Password:", False)]

    def validate_kbdint_response(self, username, responses) -> bool:
        return responses == [PASSWORD]

    def public_key_auth_supported(self) -> bool:
        return True

    def validate_public_key(self, username, key) -> bool:
        return any(key.public_data == k.public_data for k in self.authorized)


@pytest.fixture
async def ssh_server(socket_enabled):
    MacLikeServer.authorized = []
    keys = [asyncssh.generate_private_key("ssh-rsa"), asyncssh.generate_private_key("ssh-ed25519")]
    server = await asyncssh.create_server(MacLikeServer, "127.0.0.1", 0, server_host_keys=keys)
    yield server, server.sockets[0].getsockname()[1]
    server.close()
    await server.wait_closed()


async def test_password_then_key_with_pinned_host_key(hass: HomeAssistant, ssh_server) -> None:
    server, port = ssh_server
    first = Installer(hass, "127.0.0.1", port, "RPM", password=PASSWORD)
    async with await first.connect():
        pass
    assert first.host_key and first.port == port

    private, public = generate_key()
    MacLikeServer.authorized.append(asyncssh.import_public_key(public))
    keyed = Installer(hass, "127.0.0.1", port, "RPM", private_key=private, host_key=first.host_key)
    async with await keyed.connect():
        pass


async def test_wrong_password(hass: HomeAssistant, ssh_server) -> None:
    _, port = ssh_server
    with pytest.raises(AuthFailed):
        await Installer(hass, "127.0.0.1", port, "RPM", password="nope").connect()


async def test_changed_host_key(hass: HomeAssistant, ssh_server) -> None:
    _, port = ssh_server
    private, public = generate_key()
    MacLikeServer.authorized.append(asyncssh.import_public_key(public))
    other = asyncssh.generate_private_key("ssh-ed25519").export_public_key().decode().strip()
    with pytest.raises(HostKeyChanged):
        await Installer(hass, "127.0.0.1", port, "RPM", private_key=private, host_key=other).connect()
