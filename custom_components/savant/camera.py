"""Cameras set up in Savant, read straight from each camera."""

from __future__ import annotations

import logging
from urllib.parse import unquote, urlsplit, urlunsplit

import aiohttp
from homeassistant.components.camera import Camera, CameraEntityFeature
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas

_LOGGER = logging.getLogger(__name__)
TIMEOUT = aiohttp.ClientTimeout(total=10)


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantCamera(hub, m) for m in metas(hub, "camera"))


def split_login(url: str, username: str | None, password: str | None) -> tuple[str, str | None, str | None]:
    """Takes the login out of a URL like http://user:pass@cam/snapshot.cgi."""
    parts = urlsplit(url)
    if parts.username is not None:
        username, password = unquote(parts.username), unquote(parts.password or "")
        host = parts.hostname or ""
        if ":" in host:
            host = f"[{host}]"
        if parts.port:
            host += f":{parts.port}"
        url = urlunsplit(parts._replace(netloc=host))
    return url, username, password


class SavantCamera(SavantEntity, Camera):
    def __init__(self, hub, meta) -> None:
        SavantEntity.__init__(self, hub, meta)
        Camera.__init__(self)
        secret = meta.get("secret") or {}
        self._stream = secret.get("stream_url")
        self._snapshot = secret.get("snapshot_url")
        self._username = secret.get("username")
        self._password = secret.get("password")
        if self._stream:
            self._attr_supported_features = CameraEntityFeature.STREAM
        self._digest = False

    @property
    def available(self) -> bool:
        return True

    async def stream_source(self) -> str | None:
        return self._stream

    async def async_camera_image(self, width: int | None = None, height: int | None = None) -> bytes | None:
        if not self._snapshot:
            return None
        url, user, password = split_login(self._snapshot, self._username, self._password)
        session = async_get_clientsession(self.hass)
        kwargs: dict = {"timeout": TIMEOUT}
        if user:
            if self._digest:
                kwargs["middlewares"] = (aiohttp.DigestAuthMiddleware(user, password or ""),)
            else:
                kwargs["auth"] = aiohttp.BasicAuth(user, password or "")
        try:
            async with session.get(url, **kwargs) as resp:
                if resp.status == 401 and user and not self._digest and "digest" in resp.headers.get("WWW-Authenticate", "").lower():
                    self._digest = True
                    return await self.async_camera_image(width, height)
                resp.raise_for_status()
                return await resp.read()
        except (aiohttp.ClientError, TimeoutError) as err:
            _LOGGER.debug("Snapshot from %s failed: %s", self.name, err)
            return None
