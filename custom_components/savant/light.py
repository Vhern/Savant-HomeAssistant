"""Savant lights, including DMX loads."""

from __future__ import annotations

from typing import Any

from homeassistant.components.light import ATTR_BRIGHTNESS, ATTR_TRANSITION, ColorMode, LightEntity, LightEntityFeature
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback
from homeassistant.util.color import brightness_to_value, value_to_brightness

from . import SavantConfigEntry
from .entity import SavantEntity, metas

SCALE = (1, 100)


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantLight(hub, m) for m in metas(hub, "light"))


class SavantLight(SavantEntity, LightEntity):
    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        mode = ColorMode.BRIGHTNESS if self.attrs.get("dimmable") else ColorMode.ONOFF
        self._attr_color_mode = mode
        self._attr_supported_color_modes = {mode}
        if mode is ColorMode.BRIGHTNESS:
            self._attr_supported_features = LightEntityFeature.TRANSITION

    @property
    def is_on(self) -> bool | None:
        return self.data.get("on")

    @property
    def brightness(self) -> int | None:
        level = self.data.get("brightness")
        return value_to_brightness(SCALE, level) if level else None

    async def async_turn_on(self, **kwargs: Any) -> None:
        params: dict[str, Any] = {}
        if ATTR_BRIGHTNESS in kwargs:
            params["brightness"] = max(1, round(brightness_to_value(SCALE, kwargs[ATTR_BRIGHTNESS])))
        if ATTR_TRANSITION in kwargs:
            params["transition"] = kwargs[ATTR_TRANSITION]
        await self.command("turn_on", **params)

    async def async_turn_off(self, **kwargs: Any) -> None:
        params = {"transition": kwargs[ATTR_TRANSITION]} if ATTR_TRANSITION in kwargs else {}
        await self.command("turn_off", **params)
