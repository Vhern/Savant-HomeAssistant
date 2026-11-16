"""Buttons on the Savant host device."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass

from homeassistant.components.button import ButtonEntity, ButtonEntityDescription
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .hub import SavantHub


@dataclass(frozen=True, kw_only=True)
class SavantButtonDescription(ButtonEntityDescription):
    press: Callable[[SavantHub], Awaitable[None]]


BUTTONS = (
    SavantButtonDescription(
        key="restart_agent", translation_key="restart_agent", entity_category=EntityCategory.DIAGNOSTIC,
        press=lambda hub: hub.async_restart_agent(),
    ),
    SavantButtonDescription(
        key="rediscover", translation_key="rediscover", entity_category=EntityCategory.DIAGNOSTIC,
        press=lambda hub: hub.async_rediscover(),
    ),
)


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    async_add_entities(SavantButton(entry.runtime_data, d) for d in BUTTONS)


class SavantButton(ButtonEntity):
    _attr_has_entity_name = True
    entity_description: SavantButtonDescription

    def __init__(self, hub: SavantHub, description: SavantButtonDescription) -> None:
        self.hub = hub
        self.entity_description = description
        self._attr_unique_id = f"{hub.uid}_{description.key}"
        self._attr_device_info = hub.host_device()

    async def async_press(self) -> None:
        await self.entity_description.press(self.hub)
