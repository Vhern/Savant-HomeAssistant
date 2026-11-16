"""Savant rooms and their sources as media players."""

from __future__ import annotations

from homeassistant.components.media_player import (
    MediaPlayerDeviceClass,
    MediaPlayerEntity,
    MediaPlayerEntityFeature,
    MediaPlayerState,
)
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas

F = MediaPlayerEntityFeature
OPTIONAL_FEATURES = {
    "volume_step": F.VOLUME_STEP,
    "volume_mute": F.VOLUME_MUTE,
    "volume_set": F.VOLUME_SET,
    "play": F.PLAY,
    "pause": F.PAUSE,
    "stop": F.STOP,
}


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(
        SavantRoom(hub, m) if (m.get("attributes") or {}).get("kind") == "room" else SavantSource(hub, m)
        for m in metas(hub, "media_player")
    )


class SavantPlayer(SavantEntity, MediaPlayerEntity):
    base_features = F.TURN_ON | F.TURN_OFF

    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        a = self.attrs
        if a.get("device_class") == "tv":
            self._attr_device_class = MediaPlayerDeviceClass.TV
        self._max = float(a.get("volume_max") or 100)
        features = self.base_features
        for key, flag in OPTIONAL_FEATURES.items():
            if a.get(key):
                features |= flag
        self._attr_supported_features = features

    @property
    def state(self) -> MediaPlayerState | None:
        on = self.data.get("on")
        return None if on is None else (MediaPlayerState.ON if on else MediaPlayerState.OFF)

    async def async_turn_on(self) -> None:
        await self.command("turn_on", {"on": True})

    async def async_turn_off(self) -> None:
        await self.command("turn_off", {"on": False})

    async def async_volume_up(self) -> None:
        await self.command("volume_up")

    async def async_volume_down(self) -> None:
        await self.command("volume_down")

    async def async_mute_volume(self, mute: bool) -> None:
        await self.command("mute", {"muted": mute}, muted=mute)

    async def async_set_volume_level(self, volume: float) -> None:
        await self.command("set_volume", volume=round(volume * self._max))

    async def async_media_play(self) -> None:
        await self.command("play")

    async def async_media_pause(self) -> None:
        await self.command("pause")

    async def async_media_stop(self) -> None:
        await self.command("stop")


class SavantRoom(SavantPlayer):
    base_features = F.TURN_ON | F.TURN_OFF | F.SELECT_SOURCE

    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        self._attr_source_list = list(self.attrs.get("sources") or [])
        if self.attrs.get("device_class") != "tv":
            self._attr_device_class = MediaPlayerDeviceClass.RECEIVER

    @property
    def source(self) -> str | None:
        return self.data.get("source")

    @property
    def volume_level(self) -> float | None:
        vol = self.data.get("volume")
        return None if vol is None else min(1.0, vol / self._max)

    @property
    def is_volume_muted(self) -> bool | None:
        return self.data.get("muted")

    @property
    def app_name(self) -> str | None:
        return self.data.get("app")

    async def async_turn_off(self) -> None:
        await self.command("turn_off", {"on": False, "source": None})

    async def async_select_source(self, source: str) -> None:
        await self.command("select_source", {"on": True, "source": source}, source=source)


class SavantSource(SavantPlayer):
    _attr_icon = "mdi:play-box-outline"

    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        if apps := self.attrs.get("sources"):
            self._attr_source_list = list(apps)
            self._attr_supported_features |= F.SELECT_SOURCE

    @property
    def source(self) -> str | None:
        return self.data.get("source") if self.source_list else None

    @property
    def app_name(self) -> str | None:
        return self.source

    async def async_select_source(self, source: str) -> None:
        await self.command("select_source", {"on": True, "source": source}, source=source)
