"""Fake Savant host: the real agent binary, a local shell instead of SSH, and a fake sclibridge."""

from __future__ import annotations

import asyncio
import contextlib
import gzip
import json
import os
import shutil
import signal
import socket
import subprocess
import time
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from unittest.mock import patch

import pytest
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.savant.const import (
    CONF_AGENT_PORT,
    CONF_HOST_KEY,
    CONF_PRIVATE_KEY,
    CONF_SSH_PORT,
    CONF_TOKEN,
    DOMAIN,
)
from custom_components.savant.installer import BINARIES

ROOT = Path(__file__).parent.parent
FIXTURES = ROOT / "agent" / "testdata"
TOKEN = "test-token"
HOST_UID = "842F579B837F0000"
VERSION = "9.9.9"
OLD_VERSION = "9.9.8"

BATH_TIME = "A718B42D-0873-4FB3-8D26-6B4154E964B9"
SCENE_DB = f"""
CREATE TABLE Scenes (identifier TEXT UNIQUE, name TEXT, imageKey TEXT, tags TEXT, isGlobal INTEGER, isActive INTEGER,
  isScheduled INTEGER, hasCustomImage INTEGER, alias TEXT, isSceneOn INTEGER);
CREATE TABLE SceneUserMap (id INTEGER PRIMARY KEY, sceneIdentifier TEXT, user Text, sceneOrder INTEGER);
CREATE TABLE SceneDefinitions (id INTEGER PRIMARY KEY, sceneIdentifier TEXT, definition TEXT);
INSERT INTO Scenes (identifier, name, isGlobal) VALUES ('{BATH_TIME}', 'Bath Time', 1), ('6B32980B', 'Chill', 0);
INSERT INTO SceneUserMap (sceneIdentifier, user) VALUES ('{BATH_TIME}', 'admin'), ('6B32980B', 'someone@example.com');
INSERT INTO SceneDefinitions (sceneIdentifier, definition) VALUES
  ('{BATH_TIME}', '{{"av":{{"PS5.Game_console":{{"serviceID":"SVC_AV_GAME","rooms":["Room 1"]}}}}}}'),
  ('6B32980B', '{{"volume":{{"Great Room":20}}}}');
"""

INITIAL_STATES = {
    "Room 1.ActiveService": "",
    "Door Lock Controller.Door_lock.DoorLockStatus_1": "Unlocked",
    "Vhern's.SoftwareVersion": "9.4.6:696",
    "Vhern's.SystemName": "Vhern's",
    "Vhern's.DeviceModel": "Smart Host",
    "Vhern's.SysStatus.CPUPercent": "5.4",
    "Vhern's.SysStatus.RAMPercent": "23.1",
    "Vhern's.SysStatus.FreeDiskSpacePercentage": "28",
    "Vhern's.SysStatus.Uptime.Days": "3",
}


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations):
    yield


def _binary_name() -> str:
    os_, arch = os.uname().sysname, os.uname().machine
    return BINARIES[(os_, arch)]


def _build(out_dir: Path, version: str) -> Path:
    out_dir.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ["go", "build", "-ldflags", f"-X main.version={version}", "-o", str(out_dir / _binary_name()), "./cmd/savant-ha"],
        cwd=ROOT / "agent",
        check=True,
        env={**os.environ, "CGO_ENABLED": "0"},
    )
    (out_dir / "VERSION").write_text(version + "\n")
    return out_dir


@pytest.fixture(scope="session")
def bin_dirs(tmp_path_factory) -> dict[str, Path]:
    base = tmp_path_factory.mktemp("bin")
    return {VERSION: _build(base / "new", VERSION), OLD_VERSION: _build(base / "old", OLD_VERSION)}


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Result:
    def __init__(self, stdout, stderr, exit_status):
        self.stdout, self.stderr, self.exit_status = stdout, stderr, exit_status


class LocalConn:
    def __init__(self, env: dict[str, str]) -> None:
        self.env = env

    async def __aenter__(self):
        return self

    async def __aexit__(self, *exc):
        return False

    def close(self) -> None:
        pass

    async def run(self, cmd, input=None, encoding="utf-8", timeout=None):
        proc = await asyncio.create_subprocess_shell(
            cmd, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE, env=self.env,
        )
        data = input.encode() if isinstance(input, str) else input
        out, err = await asyncio.wait_for(proc.communicate(data), timeout)
        if encoding:
            out, err = out.decode(encoding), err.decode(encoding)
        return Result(out, err, proc.returncode)


@dataclass
class FakeHost:
    home: Path
    state_file: Path
    log_file: Path
    port: int
    env: dict[str, str]
    bin_dirs: dict[str, Path]
    uid: str = HOST_UID
    name: str = "Vhern's"
    ssh_port: int = 22

    def entry(self, **options) -> MockConfigEntry:
        return MockConfigEntry(
            domain=DOMAIN,
            title=self.name,
            unique_id=self.uid,
            options=options,
            data={
                "host": "127.0.0.1",
                CONF_SSH_PORT: self.ssh_port,
                "username": "RPM",
                CONF_PRIVATE_KEY: "unused",
                CONF_HOST_KEY: "ssh-ed25519 AAAAtest",
                CONF_TOKEN: TOKEN,
                CONF_AGENT_PORT: self.port,
            },
        )

    @property
    def agent_dir(self) -> Path:
        return self.home / ".savant-ha"

    def log(self) -> str:
        return self.log_file.read_text() if self.log_file.exists() else ""

    def kill_agent(self) -> None:
        pid_file = self.agent_dir / "agent.pid"
        if pid_file.exists():
            with contextlib.suppress(ProcessLookupError, ValueError):
                os.kill(int(pid_file.read_text()), signal.SIGKILL)
        time.sleep(0.2)


def _fake_host(tmp_path: Path, bin_dirs, uid: str = HOST_UID, name: str = "Vhern's", ssh_port: int = 22) -> FakeHost:
    home = tmp_path / "home"
    (home / "bin").mkdir(parents=True)
    shutil.copy(Path(__file__).parent / "fake_sclibridge.sh", home / "bin" / "sclibridge")
    (home / "bin" / "sclibridge").chmod(0o755)
    for rel in (
        "Library/Application Support/RacePointMedia/userConfig.rpmConfig",
        "GNUstep/Library/ApplicationSupport/RacePointMedia/userConfig.rpmConfig",
    ):
        cfg = home / rel
        cfg.mkdir(parents=True)
        shutil.copy(FIXTURES / "shr2000-dataTableInfo.plist", cfg / "dataTableInfo.plist")
        manifest = json.loads((FIXTURES / "shr2000-uimanifest.json").read_text())
        manifest["HostUID"] = uid
        (cfg / "uimanifest.json").write_text(json.dumps(manifest))
        with gzip.open(FIXTURES / "shr2000-serviceImplementation.xml.gz") as src:
            (cfg / "serviceImplementation.xml").write_bytes(src.read())
    for rel in (
        "Library/Application Support/RacePointMedia/statusfiles/dis",
        "GNUstep/Library/ApplicationSupport/RacePointMedia/statusfiles/dis",
    ):
        (home / rel).mkdir(parents=True)
        subprocess.run(["sqlite3", str(home / rel / "dashboard.sqlite"), SCENE_DB], check=True)
    state = tmp_path / "state.txt"
    states = {**INITIAL_STATES, "Vhern's.SystemName": name}
    state.write_text("".join(f"{k}={v}\n" for k, v in states.items()))
    env = {
        **os.environ,
        "HOME": str(home),
        "PATH": f"{home / 'bin'}:{os.environ['PATH']}",
        "FAKE_STATE": str(state),
        "FAKE_LOG": str(tmp_path / "sclibridge.log"),
        "FAKE_SERVICES": str(FIXTURES / "shr2000-servicesforzone-room1.txt"),
    }
    return FakeHost(home, state, tmp_path / "sclibridge.log", _free_port(), env, bin_dirs, uid, name, ssh_port)


@pytest.fixture
def fake_hosts():
    """Every fake host by SSH port, so the patched SSH login reaches the right one."""
    return {}


@pytest.fixture
def savant_host(socket_enabled, tmp_path, bin_dirs, fake_hosts):
    host = _fake_host(tmp_path / "a", bin_dirs)
    fake_hosts[host.ssh_port] = host

    async def connect(self):
        if self.host_key is None:
            self.host_key = "ssh-ed25519 AAAAtest"
        return LocalConn(fake_hosts[self.port].env)

    with (
        patch("custom_components.savant.installer.Installer.connect", connect),
        patch("custom_components.savant.installer.BIN_DIR", bin_dirs[VERSION]),
        patch("custom_components.savant.installer.LISTEN_ADDR", "127.0.0.1"),
        patch("custom_components.savant.hub.MAX_BACKOFF", 2),
    ):
        yield host
    for h in fake_hosts.values():
        h.kill_agent()


@pytest.fixture
def second_host(savant_host, tmp_path, bin_dirs, fake_hosts):
    """Another Savant system, like a second unit in the same building."""
    host = _fake_host(tmp_path / "b", bin_dirs, uid="00AA11BB22CC0000", name="Unit B", ssh_port=2222)
    fake_hosts[host.ssh_port] = host
    return host


async def wait_for(hass, check: Callable[[], bool], timeout: float = 20) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        await hass.async_block_till_done()
        try:
            if check():
                return
        except AttributeError:
            pass
        await asyncio.sleep(0.2)
    raise AssertionError("condition not met in time")
