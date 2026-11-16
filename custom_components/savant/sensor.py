"""Savant host sensors."""

from __future__ import annotations

from homeassistant.components.sensor import SensorDeviceClass, SensorEntity, SensorStateClass
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantSensor(hub, m) for m in metas(hub, "sensor"))


class SavantSensor(SavantEntity, SensorEntity):
    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        a = self.attrs
        self._attr_native_unit_of_measurement = a.get("unit")
        if dc := a.get("device_class"):
            self._attr_device_class = SensorDeviceClass(dc)
        if sc := a.get("state_class"):
            self._attr_state_class = SensorStateClass(sc)

    @property
    def native_value(self):
        return self.data.get("value")
