"""Connection status for the Savant host."""

from __future__ import annotations

from homeassistant.components.binary_sensor import BinarySensorDeviceClass, BinarySensorEntity
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .hub import SavantHub


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    async_add_entities([SavantConnection(entry.runtime_data)])


class SavantConnection(BinarySensorEntity):
    _attr_has_entity_name = True
    _attr_should_poll = False
    _attr_translation_key = "connection"
    _attr_device_class = BinarySensorDeviceClass.CONNECTIVITY
    _attr_entity_category = EntityCategory.DIAGNOSTIC

    def __init__(self, hub: SavantHub) -> None:
        self.hub = hub
        self._attr_unique_id = f"{hub.uid}_connection"
        self._attr_device_info = hub.host_device()

    @property
    def is_on(self) -> bool:
        return self.hub.connected and self.hub.host_available

    @property
    def extra_state_attributes(self) -> dict[str, str | None]:
        return {"agent_version": self.hub.agent_version, "last_repair": self.hub.last_repair}

    async def async_added_to_hass(self) -> None:
        self.async_on_remove(self.hub.async_add_listener(None, self.async_write_ha_state))
