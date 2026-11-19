"""Savant water heaters."""

from __future__ import annotations

from typing import Any

from homeassistant.components.water_heater import WaterHeaterEntity, WaterHeaterEntityFeature
from homeassistant.const import ATTR_TEMPERATURE, UnitOfTemperature
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantWaterHeater(hub, m) for m in metas(hub, "water_heater"))


class SavantWaterHeater(SavantEntity, WaterHeaterEntity):
    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        celsius = self.attrs.get("unit") == "C"
        self._attr_temperature_unit = UnitOfTemperature.CELSIUS if celsius else UnitOfTemperature.FAHRENHEIT
        if self.attrs.get("settable"):
            self._attr_supported_features = WaterHeaterEntityFeature.TARGET_TEMPERATURE

    @property
    def current_temperature(self) -> float | None:
        return self.data.get("temperature")

    @property
    def target_temperature(self) -> float | None:
        return self.data.get("target")

    @property
    def current_operation(self) -> str | None:
        return self.data.get("mode")

    async def async_set_temperature(self, **kwargs: Any) -> None:
        temp = kwargs[ATTR_TEMPERATURE]
        await self.command("set_temperature", {"target": temp}, temperature=temp)
