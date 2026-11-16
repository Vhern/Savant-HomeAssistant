"""Savant shades."""

from __future__ import annotations

from typing import Any

from homeassistant.components.cover import ATTR_POSITION, CoverDeviceClass, CoverEntity, CoverEntityFeature
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantShade(hub, m) for m in metas(hub, "cover"))


class SavantShade(SavantEntity, CoverEntity):
    _attr_device_class = CoverDeviceClass.SHADE

    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        features = CoverEntityFeature.OPEN | CoverEntityFeature.CLOSE
        if self.attrs.get("stop"):
            features |= CoverEntityFeature.STOP
        if self.attrs.get("position"):
            features |= CoverEntityFeature.SET_POSITION
        else:
            self._attr_assumed_state = True
        self._attr_supported_features = features

    @property
    def current_cover_position(self) -> int | None:
        return self.data.get("position")

    @property
    def is_closed(self) -> bool | None:
        pos = self.current_cover_position
        return None if pos is None else pos == 0

    async def async_open_cover(self, **kwargs: Any) -> None:
        await self.command("open")

    async def async_close_cover(self, **kwargs: Any) -> None:
        await self.command("close")

    async def async_stop_cover(self, **kwargs: Any) -> None:
        await self.command("stop")

    async def async_set_cover_position(self, **kwargs: Any) -> None:
        await self.command("set_position", {"position": kwargs[ATTR_POSITION]}, position=kwargs[ATTR_POSITION])
