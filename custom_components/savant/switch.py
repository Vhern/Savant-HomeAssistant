"""Savant lighting scenes, and pool and spa auxiliaries."""

from __future__ import annotations

from typing import Any

from homeassistant.components.switch import SwitchEntity
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantSwitch(hub, m) for m in metas(hub, "switch"))


class SavantSwitch(SavantEntity, SwitchEntity):
    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        # Pool auxiliaries don't report their state, so it stays unknown.
        self._attr_assumed_state = bool(self.attrs.get("assumed"))

    @property
    def is_on(self) -> bool | None:
        return None if self._attr_assumed_state else self.data.get("on")

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self.command("turn_on", None if self._attr_assumed_state else {"on": True})

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self.command("turn_off", None if self._attr_assumed_state else {"on": False})
