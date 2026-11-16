"""Base class for Savant entities."""

from __future__ import annotations

from typing import Any

from homeassistant.const import EntityCategory
from homeassistant.helpers.entity import Entity

from .hub import SavantHub

MODELS = {
    "light": "Savant lighting load",
    "cover": "Savant shade",
    "lock": "Savant door lock",
    "media_player": "Savant room audio/video",
}


def metas(hub: SavantHub, platform: str) -> list[dict[str, Any]]:
    return [m for m in hub.entities.values() if m["platform"] == platform]


class SavantEntity(Entity):
    _attr_should_poll = False
    _attr_has_entity_name = True

    def __init__(self, hub: SavantHub, meta: dict[str, Any]) -> None:
        self.hub = hub
        self.sid: str = meta["id"]
        self.attrs: dict[str, Any] = meta.get("attributes") or {}
        zone = meta.get("zone")
        self._attr_unique_id = f"{hub.uid}_{self.sid}"
        if zone:
            platform = meta["platform"]
            name = "Media" if platform == "media_player" else meta["name"]
            self._attr_name = None
            self._attr_device_info = hub.entity_device(self.sid, name, zone, MODELS.get(platform, "Savant"))
        else:
            self._attr_name = meta["name"]
            self._attr_device_info = hub.host_device()
        if self.attrs.get("entity_category") == "diagnostic":
            self._attr_entity_category = EntityCategory.DIAGNOSTIC
        if icon := self.attrs.get("icon"):
            self._attr_icon = icon

    @property
    def data(self) -> dict[str, Any]:
        return self.hub.states.get(self.sid) or {}

    @property
    def available(self) -> bool:
        return self.hub.connected and self.hub.host_available and self.hub.states.get(self.sid) is not None

    async def async_added_to_hass(self) -> None:
        self.async_on_remove(self.hub.async_add_listener(self.sid, self.async_write_ha_state))

    async def command(self, action: str, **params: Any) -> None:
        await self.hub.async_command(self.sid, action, params)
