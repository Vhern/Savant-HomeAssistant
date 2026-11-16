"""Savant scenes."""

from __future__ import annotations

from typing import Any

from homeassistant.components.scene import Scene
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantScene(hub, m) for m in metas(hub, "scene"))


class SavantScene(SavantEntity, Scene):
    _attr_icon = "mdi:palette"

    async def async_activate(self, **kwargs: Any) -> None:
        await self.command("activate")
