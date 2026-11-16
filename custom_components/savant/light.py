"""Savant lights, including RGB and tunable white DMX loads."""

from __future__ import annotations

from typing import Any

from homeassistant.components.light import (
    ATTR_BRIGHTNESS,
    ATTR_COLOR_TEMP_KELVIN,
    ATTR_RGBW_COLOR,
    ATTR_TRANSITION,
    ColorMode,
    LightEntity,
    LightEntityFeature,
)
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback
from homeassistant.util.color import brightness_to_value, value_to_brightness

from . import SavantConfigEntry
from .entity import SavantEntity, metas

SCALE = (1, 100)
COLOR_MODES = {"rgbw": ColorMode.RGBW, "color_temp": ColorMode.COLOR_TEMP}


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantLight(hub, m) for m in metas(hub, "light"))


class SavantLight(SavantEntity, LightEntity):
    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        modes = {COLOR_MODES[m] for m in self.attrs.get("color_modes") or () if m in COLOR_MODES}
        if not modes:
            modes = {ColorMode.BRIGHTNESS if self.attrs.get("dimmable") else ColorMode.ONOFF}
        self._attr_supported_color_modes = modes
        self._default_mode = ColorMode.RGBW if ColorMode.RGBW in modes else next(iter(modes))
        if ColorMode.COLOR_TEMP in modes:
            self._attr_min_color_temp_kelvin = self.attrs.get("min_kelvin") or 2200
            self._attr_max_color_temp_kelvin = self.attrs.get("max_kelvin") or 6500
        if modes != {ColorMode.ONOFF}:
            self._attr_supported_features = LightEntityFeature.TRANSITION

    @property
    def color_mode(self) -> ColorMode:
        mode = COLOR_MODES.get(self.data.get("color_mode"))
        return mode if mode in self.supported_color_modes else self._default_mode

    @property
    def is_on(self) -> bool | None:
        return self.data.get("on")

    @property
    def brightness(self) -> int | None:
        level = self.data.get("brightness")
        return value_to_brightness(SCALE, level) if level else None

    @property
    def rgbw_color(self) -> tuple[int, int, int, int] | None:
        rgbw = self.data.get("rgbw")
        return tuple(rgbw) if rgbw and len(rgbw) == 4 else None

    @property
    def color_temp_kelvin(self) -> int | None:
        return self.data.get("kelvin")

    async def async_turn_on(self, **kwargs: Any) -> None:
        params: dict[str, Any] = {}
        expect: dict[str, Any] = {"on": True}
        if ATTR_BRIGHTNESS in kwargs:
            params["brightness"] = expect["brightness"] = max(1, round(brightness_to_value(SCALE, kwargs[ATTR_BRIGHTNESS])))
        if ATTR_RGBW_COLOR in kwargs:
            params["rgbw"] = expect["rgbw"] = list(kwargs[ATTR_RGBW_COLOR])
            expect["color_mode"] = "rgbw"
        if ATTR_COLOR_TEMP_KELVIN in kwargs:
            params["kelvin"] = expect["kelvin"] = kwargs[ATTR_COLOR_TEMP_KELVIN]
            expect["color_mode"] = "color_temp"
        if ATTR_TRANSITION in kwargs:
            params["transition"] = kwargs[ATTR_TRANSITION]
        await self.command("turn_on", expect, **params)

    async def async_turn_off(self, **kwargs: Any) -> None:
        params = {"transition": kwargs[ATTR_TRANSITION]} if ATTR_TRANSITION in kwargs else {}
        await self.command("turn_off", {"on": False}, **params)
