"""A media player for each Savant room."""

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


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantRoom(hub, m) for m in metas(hub, "media_player"))


class SavantRoom(SavantEntity, MediaPlayerEntity):
    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        a = self.attrs
        self._attr_source_list = list(a.get("sources") or [])
        self._attr_device_class = (
            MediaPlayerDeviceClass.TV if a.get("device_class") == "tv" else MediaPlayerDeviceClass.RECEIVER
        )
        self._max = float(a.get("volume_max") or 100)
        f = MediaPlayerEntityFeature.TURN_ON | MediaPlayerEntityFeature.TURN_OFF | MediaPlayerEntityFeature.SELECT_SOURCE
        if a.get("volume_step"):
            f |= MediaPlayerEntityFeature.VOLUME_STEP
        if a.get("volume_mute"):
            f |= MediaPlayerEntityFeature.VOLUME_MUTE
        if a.get("volume_set"):
            f |= MediaPlayerEntityFeature.VOLUME_SET
        self._attr_supported_features = f

    @property
    def state(self) -> MediaPlayerState | None:
        on = self.data.get("on")
        return None if on is None else (MediaPlayerState.ON if on else MediaPlayerState.OFF)

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

    async def async_turn_on(self) -> None:
        await self.command("turn_on")

    async def async_turn_off(self) -> None:
        await self.command("turn_off")

    async def async_select_source(self, source: str) -> None:
        await self.command("select_source", source=source)

    async def async_volume_up(self) -> None:
        await self.command("volume_up")

    async def async_volume_down(self) -> None:
        await self.command("volume_down")

    async def async_mute_volume(self, mute: bool) -> None:
        await self.command("mute", muted=mute)

    async def async_set_volume_level(self, volume: float) -> None:
        await self.command("set_volume", volume=round(volume * self._max))
