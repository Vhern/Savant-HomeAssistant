"""Savant door locks."""

from __future__ import annotations

from typing import Any

from homeassistant.components.lock import LockEntity
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantLock(hub, m) for m in metas(hub, "lock"))


class SavantLock(SavantEntity, LockEntity):
    def _moving_to(self, locked: bool) -> bool:
        data = self.data
        return self.pending is not None and data.get("locked") is locked and self.real.get("locked") is not locked

    @property
    def is_locked(self) -> bool | None:
        return self.real.get("locked")

    @property
    def is_locking(self) -> bool:
        return self._moving_to(True)

    @property
    def is_unlocking(self) -> bool:
        return self._moving_to(False)

    @property
    def is_jammed(self) -> bool | None:
        return self.real.get("jammed")

    async def async_lock(self, **kwargs: Any) -> None:
        await self.command("lock", {"locked": True})

    async def async_unlock(self, **kwargs: Any) -> None:
        await self.command("unlock", {"locked": False})
