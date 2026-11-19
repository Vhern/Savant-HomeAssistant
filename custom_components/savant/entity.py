"""Base class for Savant entities."""

from __future__ import annotations

import time
from typing import Any

from homeassistant.const import EntityCategory
from homeassistant.core import CALLBACK_TYPE, callback
from homeassistant.helpers.entity import Entity
from homeassistant.helpers.event import async_call_later

from .hub import SavantHub

PENDING_TIMEOUT = 10

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
        if device := meta.get("device"):
            self._attr_name = None if self.attrs.get("use_device_name") else meta["name"]
            self._attr_device_info = hub.entity_device(
                device["id"], device["name"], zone, device.get("model") or "Savant", device.get("manufacturer")
            )
        else:
            self._attr_name = meta["name"]
            self._attr_device_info = hub.host_device()
        if self.attrs.get("entity_category") == "diagnostic":
            self._attr_entity_category = EntityCategory.DIAGNOSTIC
        if self.attrs.get("disabled"):
            self._attr_entity_registry_enabled_default = False
        if icon := self.attrs.get("icon"):
            self._attr_icon = icon
        self.pending: dict[str, Any] | None = None
        self._pending_until = 0.0
        self._pending_timer: CALLBACK_TYPE | None = None

    @property
    def real(self) -> dict[str, Any]:
        return self.hub.states.get(self.sid) or {}

    @property
    def data(self) -> dict[str, Any]:
        real = self.real
        if self.pending is not None:
            confirmed = all(real.get(k) == v for k, v in self.pending.items())
            if not confirmed and time.monotonic() < self._pending_until:
                return {**real, **self.pending}
            self._clear_pending()
        return real

    @property
    def available(self) -> bool:
        return self.hub.connected and self.hub.host_available and self.hub.states.get(self.sid) is not None

    async def async_added_to_hass(self) -> None:
        self.async_on_remove(self.hub.async_add_listener(self.sid, self.async_write_ha_state))

    @callback
    def _clear_pending(self) -> None:
        self.pending = None
        if self._pending_timer:
            self._pending_timer()
            self._pending_timer = None

    async def command(self, action: str, expect: dict[str, Any] | None = None, **params: Any) -> None:
        """`expect` is shown right away, until Savant reports it or 10 seconds pass."""
        if expect:
            self._clear_pending()
            self.pending = expect
            self._pending_until = time.monotonic() + PENDING_TIMEOUT
            self._pending_timer = async_call_later(self.hass, PENDING_TIMEOUT + 0.1, self._pending_expired)
            self.async_write_ha_state()
        try:
            await self.hub.async_command(self.sid, action, params)
        except Exception:
            self._clear_pending()
            self.async_write_ha_state()
            raise

    @callback
    def _pending_expired(self, _now: Any) -> None:
        self._pending_timer = None
        self.async_write_ha_state()

    async def async_will_remove_from_hass(self) -> None:
        self._clear_pending()
