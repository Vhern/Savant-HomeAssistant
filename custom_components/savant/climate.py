"""Savant thermostats."""

from __future__ import annotations

from typing import Any

from homeassistant.components.climate import (
    ATTR_HVAC_MODE,
    ATTR_TARGET_TEMP_HIGH,
    ATTR_TARGET_TEMP_LOW,
    ClimateEntity,
    ClimateEntityFeature,
    HVACAction,
    HVACMode,
)
from homeassistant.const import ATTR_TEMPERATURE, UnitOfTemperature
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas

MODES = {"off": HVACMode.OFF, "heat": HVACMode.HEAT, "cool": HVACMode.COOL, "auto": HVACMode.HEAT_COOL}
ACTIONS = {
    "heating": HVACAction.HEATING,
    "radiant": HVACAction.HEATING,
    "cooling": HVACAction.COOLING,
    "fan": HVACAction.FAN,
    "dehumidifying": HVACAction.DRYING,
    "idle": HVACAction.IDLE,
    "humidifying": HVACAction.IDLE,
    "protection": HVACAction.IDLE,
}


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantThermostat(hub, m) for m in metas(hub, "climate"))


class SavantThermostat(SavantEntity, ClimateEntity):

    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        a = self.attrs
        self._single = bool(a.get("single_setpoint"))
        self._attr_hvac_modes = [HVACMode.OFF, *(MODES[m] for m in a.get("modes") or () if m in MODES)]
        self._attr_temperature_unit = UnitOfTemperature.CELSIUS if a.get("unit") == "C" else UnitOfTemperature.FAHRENHEIT
        self._attr_target_temperature_step = 0.5 if a.get("unit") == "C" else 1
        if a.get("min_temp"):
            self._attr_min_temp, self._attr_max_temp = a["min_temp"], a["max_temp"]
        features = ClimateEntityFeature.TURN_OFF | ClimateEntityFeature.TARGET_TEMPERATURE
        if len(self._attr_hvac_modes) > 1:
            features |= ClimateEntityFeature.TURN_ON
        if HVACMode.HEAT_COOL in self._attr_hvac_modes and not self._single:
            features |= ClimateEntityFeature.TARGET_TEMPERATURE_RANGE
        if fans := a.get("fan_modes"):
            self._attr_fan_modes = list(fans)
            features |= ClimateEntityFeature.FAN_MODE
        if a.get("humidity"):
            features |= ClimateEntityFeature.TARGET_HUMIDITY
        self._attr_supported_features = features

    @property
    def hvac_mode(self) -> HVACMode | None:
        return MODES.get(self.data.get("mode"))

    @property
    def hvac_action(self) -> HVACAction | None:
        if self.hvac_mode == HVACMode.OFF:
            return HVACAction.OFF
        return ACTIONS.get(self.data.get("action"))

    @property
    def current_temperature(self) -> float | None:
        return self.data.get("temperature")

    @property
    def current_humidity(self) -> float | None:
        return self.data.get("humidity")

    @property
    def target_humidity(self) -> float | None:
        return self.data.get("target_humidity")

    @property
    def target_temperature(self) -> float | None:
        d = self.data
        if self._single:
            return d.get("target")
        return {"heat": d.get("heat"), "cool": d.get("cool")}.get(d.get("mode"))

    @property
    def target_temperature_low(self) -> float | None:
        return None if self._single else self.data.get("heat")

    @property
    def target_temperature_high(self) -> float | None:
        return None if self._single else self.data.get("cool")

    @property
    def fan_mode(self) -> str | None:
        return self.data.get("fan")

    async def async_set_hvac_mode(self, hvac_mode: HVACMode) -> None:
        mode = next(k for k, v in MODES.items() if v == hvac_mode)
        await self.command("set_mode", {"mode": mode}, mode=mode)

    async def async_turn_on(self) -> None:
        await self.async_set_hvac_mode(self._attr_hvac_modes[-1])

    async def async_turn_off(self) -> None:
        await self.async_set_hvac_mode(HVACMode.OFF)

    async def async_set_temperature(self, **kwargs: Any) -> None:
        if mode := kwargs.get(ATTR_HVAC_MODE):
            await self.async_set_hvac_mode(mode)
        params: dict[str, Any] = {}
        if ATTR_TARGET_TEMP_LOW in kwargs:
            params = {"heat": kwargs[ATTR_TARGET_TEMP_LOW], "cool": kwargs[ATTR_TARGET_TEMP_HIGH]}
        elif ATTR_TEMPERATURE in kwargs:
            temp = kwargs[ATTR_TEMPERATURE]
            if self._single:
                params = {"temperature": temp}
            else:
                params = {"cool": temp} if self.data.get("mode") == "cool" else {"heat": temp}
        if not params:
            return
        expect = {"target": params["temperature"]} if "temperature" in params else dict(params)
        await self.command("set_temperature", expect, **params)

    async def async_set_fan_mode(self, fan_mode: str) -> None:
        await self.command("set_fan", {"fan": fan_mode}, fan=fan_mode)

    async def async_set_humidity(self, humidity: int) -> None:
        await self.command("set_humidity", {"target_humidity": humidity}, humidity=humidity)
