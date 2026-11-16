"""Installs and runs the agent on the Savant host over SSH."""

from __future__ import annotations

import json
import logging
import shlex
from dataclasses import dataclass
from pathlib import Path

import asyncssh
from homeassistant.core import HomeAssistant

from .const import AGENT_BIN, AGENT_DIR, KEY_COMMENT

_LOGGER = logging.getLogger(__name__)

BIN_DIR = Path(__file__).parent / "bin"
LISTEN_ADDR = "0.0.0.0"
PORT_TRIES = 10

BINARIES = {
    ("Linux", "armv7l"): "savant-ha-linux-armv7",
    ("Linux", "armv6l"): "savant-ha-linux-armv7",
    ("Linux", "aarch64"): "savant-ha-linux-arm64",
    ("Linux", "x86_64"): "savant-ha-linux-amd64",
    ("Darwin", "arm64"): "savant-ha-darwin-arm64",
    ("Darwin", "x86_64"): "savant-ha-darwin-amd64",
}

_D = f'D="$HOME/{AGENT_DIR}"; B="$D/{AGENT_BIN}"'

_STATUS = _D + """
v=$("$B" version 2>/dev/null | awk '{print $2}')
p=$(cat "$D/agent.pid" 2>/dev/null)
r=0; [ -n "$p" ] && kill -0 "$p" 2>/dev/null && r=1
printf 'version=%s\\nrunning=%s\\n' "$v" "$r"
"""

_STOP = _D + """
p=$(cat "$D/agent.pid" 2>/dev/null)
if [ -n "$p" ] && kill -0 "$p" 2>/dev/null; then
  kill "$p"; i=0
  while kill -0 "$p" 2>/dev/null && [ $i -lt 10 ]; do sleep 1; i=$((i+1)); done
  kill -9 "$p" 2>/dev/null
fi
command -v pkill >/dev/null 2>&1 && pkill -f "$B -config" 2>/dev/null
rm -f "$D/agent.pid"
true
"""

_START = _D + """
cd "$D" || exit 1
rm -f "$D/agent.pid"
[ -f "$D/agent.log" ] && mv -f "$D/agent.log" "$D/agent.log.1"
if command -v setsid >/dev/null 2>&1; then S=setsid; else S=; fi
$S nohup "$B" -config "$D/config.json" -pidfile "$D/agent.pid" serve </dev/null >"$D/agent.log" 2>&1 &
i=0
while [ $i -lt 15 ]; do
  p=$(cat "$D/agent.pid" 2>/dev/null)
  if [ -n "$p" ] && kill -0 "$p" 2>/dev/null; then echo started; exit 0; fi
  sleep 1; i=$((i+1))
done
echo "agent did not start:"; tail -n 20 "$D/agent.log" 2>/dev/null; exit 1
"""


class InstallError(Exception):
    """Installing or starting the agent failed."""


class CannotConnect(InstallError):
    """No SSH answer from the host."""


class AuthFailed(InstallError):
    """The host rejected the SSH login."""


class HostKeyChanged(InstallError):
    """The host's SSH key changed since setup."""


class UnsupportedHost(InstallError):
    """No agent build for this OS or CPU."""


@dataclass
class AgentStatus:
    version: str | None
    running: bool


def generate_key() -> tuple[str, str]:
    key = asyncssh.generate_private_key("ssh-ed25519", comment=KEY_COMMENT)
    return key.export_private_key().decode(), key.export_public_key().decode().strip()


def bundled_version() -> str:
    return (BIN_DIR / "VERSION").read_text().strip()


def _sh(script: str) -> str:
    return "sh -c " + shlex.quote(script)


class Installer:
    def __init__(
        self,
        hass: HomeAssistant,
        host: str,
        port: int,
        username: str,
        *,
        private_key: str | None = None,
        password: str | None = None,
        host_key: str | None = None,
    ) -> None:
        self.hass = hass
        self.host = host
        self.port = port
        self.username = username
        self.private_key = private_key
        self.password = password
        self.host_key = host_key
        self.agent_port: int | None = None

    async def connect(self) -> asyncssh.SSHClientConnection:
        if self.host_key:
            name = self.host if self.port == 22 else f"[{self.host}]:{self.port}"
            known_hosts = asyncssh.import_known_hosts(f"{name} {self.host_key}\n")
        else:
            known_hosts = None
        keys = [asyncssh.import_private_key(self.private_key)] if self.private_key else None
        try:
            conn = await asyncssh.connect(
                self.host,
                port=self.port,
                username=self.username,
                password=self.password,
                client_keys=keys,
                known_hosts=known_hosts,
                agent_path=None,
                connect_timeout=15,
                keepalive_interval=15,
            )
        except asyncssh.PermissionDenied as err:
            raise AuthFailed(str(err)) from err
        except asyncssh.HostKeyNotVerifiable as err:
            raise HostKeyChanged(str(err)) from err
        except (OSError, TimeoutError, asyncssh.Error) as err:
            raise CannotConnect(f"{self.host}:{self.port}: {err}") from err
        if not self.host_key:
            key = conn.get_server_host_key()
            if key is not None:
                self.host_key = key.export_public_key().decode().strip()
        return conn

    async def _run(
        self, conn: asyncssh.SSHClientConnection, script: str, data: bytes | None = None
    ) -> str:
        res = await conn.run(_sh(script), input=data, encoding=None if data else "utf-8", timeout=120)
        out = res.stdout.decode() if isinstance(res.stdout, bytes) else (res.stdout or "")
        if res.exit_status != 0:
            err = res.stderr.decode() if isinstance(res.stderr, bytes) else (res.stderr or "")
            raise InstallError(f"remote command failed ({res.exit_status}): {(out + err).strip()[-500:]}")
        return out

    async def binary_for(self, conn: asyncssh.SSHClientConnection) -> str:
        out = (await self._run(conn, "uname -s; uname -m")).split()
        name = BINARIES.get((out[0], out[1])) if len(out) >= 2 else None
        if name is None:
            raise UnsupportedHost(" ".join(out))
        return name

    async def status(self, conn: asyncssh.SSHClientConnection) -> AgentStatus:
        fields = dict(
            line.split("=", 1) for line in (await self._run(conn, _STATUS)).splitlines() if "=" in line
        )
        return AgentStatus(fields.get("version") or None, fields.get("running") == "1")

    async def authorize_key(self, conn: asyncssh.SSHClientConnection, public_key: str) -> None:
        q = shlex.quote(public_key)
        await self._run(
            conn,
            f"""
mkdir -p "$HOME/.ssh" && chmod 700 "$HOME/.ssh"
A="$HOME/.ssh/authorized_keys"; touch "$A"; chmod 600 "$A"
grep -v ' {KEY_COMMENT}$' "$A" > "$A.tmp"; printf '%s\\n' {q} >> "$A.tmp"; cat "$A.tmp" > "$A"; rm -f "$A.tmp"
""",
        )

    async def install(
        self, conn: asyncssh.SSHClientConnection, token: str, agent_port: int
    ) -> None:
        name = await self.binary_for(conn)
        data = await self.hass.async_add_executor_job((BIN_DIR / name).read_bytes)
        _LOGGER.info("Installing %s (%d bytes) on %s", name, len(data), self.host)
        await self._run(
            conn,
            _D + '\nmkdir -p "$D" && cat > "$B.new" && chmod 755 "$B.new" && "$B.new" version >/dev/null && mv -f "$B.new" "$B"',
            data,
        )
        await self.write_config(conn, token, agent_port)

    async def write_config(self, conn: asyncssh.SSHClientConnection, token: str, agent_port: int) -> bool:
        cfg = json.dumps({"listen": f"{LISTEN_ADDR}:{agent_port}", "token": token}).encode()
        out = await self._run(
            conn,
            _D
            + """
mkdir -p "$D" && umask 077 && cat > "$D/config.json.new" || exit 1
if [ -f "$D/config.json" ] && cmp -s "$D/config.json" "$D/config.json.new"; then rm -f "$D/config.json.new"; echo same
else mv -f "$D/config.json.new" "$D/config.json"; echo changed; fi
""",
            cfg,
        )
        return "changed" in out

    async def start(self, conn: asyncssh.SSHClientConnection, token: str, agent_port: int) -> int:
        """Start the agent, moving to the next port if something else holds this one. Returns the port."""
        port = agent_port
        for _ in range(PORT_TRIES):
            await self._run(conn, _STOP)
            try:
                await self._run(conn, _START)
            except InstallError as err:
                if "address already in use" not in str(err):
                    raise
                _LOGGER.warning("Port %d is taken on %s, trying %d", port, self.host, port + 1)
                port += 1
                await self.write_config(conn, token, port)
                continue
            self.agent_port = port
            return port
        raise InstallError(f"ports {agent_port}-{port - 1} are all taken on the host")

    async def ensure(self, token: str, agent_port: int, *, restart: bool = False) -> str:
        want = await self.hass.async_add_executor_job(bundled_version)
        self.agent_port = agent_port
        conn = await self.connect()
        async with conn:
            st = await self.status(conn)
            if st.version != want:
                await self.install(conn, token, agent_port)
                await self.start(conn, token, agent_port)
                return "updated" if st.version else "installed"
            changed = await self.write_config(conn, token, agent_port)
            if restart or changed or not st.running:
                await self.start(conn, token, agent_port)
                return "restarted" if st.running else "started"
            return "running"

    async def remove(self) -> None:
        conn = await self.connect()
        async with conn:
            await self._run(conn, _STOP)
            await self._run(
                conn,
                _D
                + f"""
rm -rf "$D"
A="$HOME/.ssh/authorized_keys"
if [ -f "$A" ]; then {{ grep -v ' {KEY_COMMENT}$' "$A" || true; }} > "$A.tmp"; cat "$A.tmp" > "$A"; rm -f "$A.tmp"; fi
true
""",
            )
