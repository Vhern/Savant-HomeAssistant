"""Connection to the agent running on the Savant host."""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Callable
from typing import Any

import aiohttp
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import CONF_HOST, CONF_USERNAME
from homeassistant.core import HomeAssistant, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import issue_registry as ir
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.device_registry import DeviceInfo

from .const import (
    CONF_AGENT_PORT,
    CONF_HOST_KEY,
    CONF_PRIVATE_KEY,
    CONF_SSH_PORT,
    CONF_TOKEN,
    DOMAIN,
)
from .installer import AuthFailed, HostKeyChanged, Installer, bundled_version

_LOGGER = logging.getLogger(__name__)

CONNECT_TIMEOUT = 10
SUBSCRIBE_TIMEOUT = 90
REQUEST_TIMEOUT = 40
READ_TIMEOUT = 75
MAX_BACKOFF = 60


class AgentError(HomeAssistantError):
    """The agent refused a request or could not be reached."""


class OutdatedAgent(AgentError):
    """The agent on the host is a different version than the one bundled here."""


async def agent_health(session: aiohttp.ClientSession, host: str, port: int) -> dict[str, Any]:
    async with session.get(
        f"http://{host}:{port}/health", timeout=aiohttp.ClientTimeout(total=CONNECT_TIMEOUT)
    ) as resp:
        resp.raise_for_status()
        return await resp.json()


async def connect_agent(
    session: aiohttp.ClientSession, host: str, port: int, token: str
) -> tuple[aiohttp.ClientWebSocketResponse, dict[str, Any], dict[str, Any]]:
    ws = await session.ws_connect(
        f"ws://{host}:{port}/ws",
        headers={"Authorization": f"Bearer {token}"},
        timeout=aiohttp.ClientWSTimeout(ws_close=10),
        max_msg_size=32 * 1024 * 1024,
    )
    try:
        hello = await ws.receive_json(timeout=CONNECT_TIMEOUT)
        if hello.get("type") != "hello":
            raise AgentError(f"unexpected greeting: {hello}")
        await ws.send_json({"id": 0, "type": "subscribe"})
        async with asyncio.timeout(SUBSCRIBE_TIMEOUT):
            while True:
                msg = await ws.receive_json()
                if msg.get("type") == "result" and msg.get("id") == 0:
                    break
        if not msg.get("success"):
            raise AgentError(msg.get("error") or "subscribe failed")
    except BaseException:
        await ws.close()
        raise
    return ws, hello, msg["result"]


class SavantHub:
    def __init__(self, hass: HomeAssistant, entry: ConfigEntry) -> None:
        self.hass = hass
        self.entry = entry
        data = entry.data
        self.host: str = data[CONF_HOST]
        self.agent_port: int = data[CONF_AGENT_PORT]
        self.token: str = data[CONF_TOKEN]
        self.installer = Installer(
            hass,
            self.host,
            data[CONF_SSH_PORT],
            data[CONF_USERNAME],
            private_key=data[CONF_PRIVATE_KEY],
            host_key=data.get(CONF_HOST_KEY),
        )
        self.session = async_get_clientsession(hass)
        self.info: dict[str, Any] = {}
        self.entities: dict[str, dict[str, Any]] = {}
        self.states: dict[str, dict[str, Any] | None] = {}
        self.connected = False
        self.host_available = True
        self.agent_version: str | None = None
        self.last_repair: str | None = None
        self._want: str | None = None
        self._ws: aiohttp.ClientWebSocketResponse | None = None
        self._pending: dict[int, asyncio.Future[dict[str, Any]]] = {}
        self._next_id = 1
        self._listeners: dict[str | None, set[Callable[[], None]]] = {}
        self._task: asyncio.Task | None = None
        self._closing = False

    @property
    def uid(self) -> str:
        return self.info.get("uid") or self.entry.unique_id or self.entry.entry_id

    @property
    def issue_id(self) -> str:
        return f"unreachable_{self.entry.entry_id}"

    def host_device(self) -> DeviceInfo:
        return DeviceInfo(
            identifiers={(DOMAIN, self.uid)},
            name=self.info.get("name") or "Savant Host",
            manufacturer="Savant",
            model=self.info.get("model") or "Savant Host",
            sw_version=self.info.get("software"),
            serial_number=self.info.get("uid"),
        )

    def entity_device(self, sid: str, name: str, zone: str, model: str) -> DeviceInfo:
        return DeviceInfo(
            identifiers={(DOMAIN, f"{self.uid}_{sid}")},
            name=name,
            manufacturer="Savant",
            model=model,
            suggested_area=zone,
            via_device=(DOMAIN, self.uid),
        )

    async def async_start(self) -> None:
        self._want = await self.hass.async_add_executor_job(bundled_version)
        try:
            ws, snapshot = await self._connect()
        except (OutdatedAgent, AgentError, aiohttp.ClientError, TimeoutError, OSError) as err:
            _LOGGER.info("Savant agent on %s not usable (%s); repairing over SSH", self.host, err)
            await self._repair(restart=isinstance(err, OutdatedAgent))
            ws, snapshot = await self._connect(retries=10)
        self._apply(snapshot, initial=True)
        self._task = self.entry.async_create_background_task(
            self.hass, self._supervise(ws), f"savant {self.host}"
        )

    async def async_stop(self, *_: Any) -> None:
        self._closing = True
        ws, self._ws = self._ws, None
        if self._task:
            self._task.cancel()
        if ws and not ws.closed:
            await ws.close()
        self._fail_pending(AgentError("connection closed"))

    async def _connect(self, retries: int = 1) -> tuple[aiohttp.ClientWebSocketResponse, dict[str, Any]]:
        for attempt in range(retries):
            try:
                health = await agent_health(self.session, self.host, self.agent_port)
                self.agent_version = health.get("version")
                if self._want and self.agent_version != self._want:
                    raise OutdatedAgent(f"agent {self.agent_version}, integration ships {self._want}")
                ws, _, snapshot = await connect_agent(self.session, self.host, self.agent_port, self.token)
            except OutdatedAgent:
                raise
            except (aiohttp.ClientError, TimeoutError, OSError, AgentError):
                if attempt == retries - 1:
                    raise
                await asyncio.sleep(2)
                continue
            return ws, snapshot
        raise AgentError("unreachable")

    async def _repair(self, restart: bool = False) -> None:
        result = await self.installer.ensure(self.token, self.agent_port, restart=restart)
        self.last_repair = result
        _LOGGER.info("Savant agent on %s: %s", self.host, result)
        updates = {}
        if self.installer.host_key and self.installer.host_key != self.entry.data.get(CONF_HOST_KEY):
            updates[CONF_HOST_KEY] = self.installer.host_key
        if self.installer.agent_port and self.installer.agent_port != self.agent_port:
            _LOGGER.warning("Savant agent on %s moved to port %d", self.host, self.installer.agent_port)
            self.agent_port = updates[CONF_AGENT_PORT] = self.installer.agent_port
        if updates:
            self.hass.config_entries.async_update_entry(self.entry, data={**self.entry.data, **updates})

    async def _supervise(self, ws: aiohttp.ClientWebSocketResponse) -> None:
        current: aiohttp.ClientWebSocketResponse | None = ws
        failures = 0
        while not self._closing:
            if current is not None:
                await self._listen(current)
                current = None
                if self._closing:
                    return
                _LOGGER.warning("Lost connection to the Savant agent on %s", self.host)
            delay = min(2**failures, MAX_BACKOFF)
            try:
                current, snapshot = await self._connect()
            except OutdatedAgent:
                failures += 1
                await self._try_repair(restart=True)
                await asyncio.sleep(delay)
                continue
            except (aiohttp.ClientError, TimeoutError, OSError, AgentError) as err:
                failures += 1
                _LOGGER.debug("Reconnect to %s failed (%s), attempt %d", self.host, err, failures)
                if failures >= 2:
                    await self._try_repair(restart=failures % 4 == 0)
                if failures == 5:
                    ir.async_create_issue(
                        self.hass, DOMAIN, self.issue_id,
                        is_fixable=False, severity=ir.IssueSeverity.ERROR,
                        translation_key="unreachable", translation_placeholders={"host": self.host},
                    )
                await asyncio.sleep(delay)
                continue
            failures = 0
            ir.async_delete_issue(self.hass, DOMAIN, self.issue_id)
            self._apply(snapshot)
            _LOGGER.info("Reconnected to the Savant agent on %s", self.host)

    async def _try_repair(self, restart: bool) -> None:
        try:
            await self._repair(restart=restart)
        except (AuthFailed, HostKeyChanged) as err:
            _LOGGER.error("SSH to %s rejected (%s); asking for the password again", self.host, err)
            self.entry.async_start_reauth(self.hass)
        except Exception as err:  # noqa: BLE001
            _LOGGER.warning("Could not repair the Savant agent on %s: %s", self.host, err)

    async def _listen(self, ws: aiohttp.ClientWebSocketResponse) -> None:
        self._ws = ws
        self._set_connected(True)
        try:
            while True:
                # The agent pings every 30 s, so a long silence means the connection is gone.
                msg = await ws.receive(timeout=READ_TIMEOUT)
                if msg.type == aiohttp.WSMsgType.TEXT:
                    self._handle(msg.json())
                elif msg.type in (aiohttp.WSMsgType.ERROR, aiohttp.WSMsgType.CLOSE, aiohttp.WSMsgType.CLOSED):
                    break
        except (aiohttp.ClientError, ValueError, TimeoutError) as err:
            _LOGGER.debug("Agent connection error: %s", err)
        finally:
            self._ws = None
            if not ws.closed:
                await ws.close()
            self._fail_pending(AgentError("connection to the Savant host was lost"))
            self._set_connected(False)

    def _apply(self, snapshot: dict[str, Any], initial: bool = False) -> None:
        entities = {e["id"]: e for e in snapshot.get("entities") or []}
        if not initial and entities.keys() != self.entities.keys():
            _LOGGER.info("Savant entities changed while disconnected; reloading")
            self.hass.config_entries.async_schedule_reload(self.entry.entry_id)
        self.info = snapshot.get("host") or {}
        self.entities = entities
        self.states = dict(snapshot.get("states") or {})
        self.host_available = snapshot.get("available", True)
        self._notify(None)

    @callback
    def _handle(self, msg: dict[str, Any]) -> None:
        kind = msg.get("type")
        if kind == "result":
            fut = self._pending.pop(msg.get("id", -1), None)
            if fut and not fut.done():
                fut.set_result(msg)
        elif kind == "state":
            eid = msg.get("entity_id")
            self.states[eid] = msg.get("state")
            self._notify(eid)
        elif kind == "availability":
            self.host_available = bool(msg.get("available"))
            self._notify(None)
        elif kind == "entities_changed":
            _LOGGER.info("Savant configuration changed; reloading entities")
            self.hass.config_entries.async_schedule_reload(self.entry.entry_id)

    @callback
    def _set_connected(self, connected: bool) -> None:
        if self.connected != connected:
            self.connected = connected
            self._notify(None)

    @callback
    def _notify(self, entity_id: str | None) -> None:
        if entity_id:
            targets = set(self._listeners.get(entity_id, ()))
        else:
            targets = {cb for cbs in self._listeners.values() for cb in cbs}
        for cb in targets:
            cb()

    @callback
    def async_add_listener(self, entity_id: str | None, cb: Callable[[], None]) -> Callable[[], None]:
        self._listeners.setdefault(entity_id, set()).add(cb)

        def remove() -> None:
            self._listeners.get(entity_id, set()).discard(cb)

        return remove

    def _fail_pending(self, err: Exception) -> None:
        for fut in self._pending.values():
            if not fut.done():
                fut.set_exception(err)
        self._pending.clear()

    async def _request(self, payload: dict[str, Any], timeout: float = REQUEST_TIMEOUT) -> Any:
        ws = self._ws
        if ws is None or ws.closed:
            raise AgentError(f"Savant host {self.host} is not connected")
        msg_id = self._next_id
        self._next_id += 1
        fut: asyncio.Future[dict[str, Any]] = self.hass.loop.create_future()
        self._pending[msg_id] = fut
        try:
            await ws.send_json({"id": msg_id, **payload})
            async with asyncio.timeout(timeout):
                res = await fut
        except TimeoutError as err:
            raise AgentError("Savant host did not answer in time") from err
        finally:
            self._pending.pop(msg_id, None)
        if not res.get("success"):
            raise AgentError(res.get("error") or "request failed")
        return res.get("result")

    async def async_command(self, entity_id: str, action: str, params: dict[str, Any] | None = None) -> None:
        await self._request({"type": "command", "entity_id": entity_id, "action": action, "params": params or {}})

    async def async_service_request(self, **fields: Any) -> None:
        await self._request({"type": "service_request", **fields})

    async def async_rediscover(self) -> None:
        await self._request({"type": "rediscover"}, timeout=120)

    async def async_diagnostics(self) -> Any:
        return await self._request({"type": "diagnostics"})

    async def async_restart_agent(self) -> None:
        try:
            await self._repair(restart=True)
        except (AuthFailed, HostKeyChanged) as err:
            self.entry.async_start_reauth(self.hass)
            raise HomeAssistantError(f"SSH to {self.host} was rejected: {err}") from err
        except Exception as err:
            raise HomeAssistantError(f"Could not restart the Savant agent: {err}") from err
