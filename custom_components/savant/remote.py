"""Remotes for Savant sources, for on-screen navigation and app shortcuts."""

from __future__ import annotations

import asyncio
from collections.abc import Iterable
from typing import Any

from homeassistant.components.remote import ATTR_DELAY_SECS, ATTR_NUM_REPEATS, RemoteEntity
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantRemote(hub, m) for m in metas(hub, "remote"))


class SavantRemote(SavantEntity, RemoteEntity):
    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        self._attr_extra_state_attributes = {"commands": list(self.attrs.get("commands") or [])}

    @property
    def is_on(self) -> bool | None:
        return self.data.get("on")

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self.command("turn_on", {"on": True})

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self.command("turn_off", {"on": False})

    async def async_send_command(self, command: Iterable[str], **kwargs: Any) -> None:
        commands = list(command)
        delay = kwargs.get(ATTR_DELAY_SECS) or 0
        for i in range(kwargs.get(ATTR_NUM_REPEATS) or 1):
            if i and delay:
                await asyncio.sleep(delay)
            await self.command("send_command", command=commands)
