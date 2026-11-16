"""Diagnostics for Savant."""

from __future__ import annotations

from typing import Any

from homeassistant.components.diagnostics import async_redact_data
from homeassistant.core import HomeAssistant

from . import SavantConfigEntry
from .const import CONF_HOST_KEY, CONF_PRIVATE_KEY, CONF_TOKEN

REDACT = {CONF_PRIVATE_KEY, CONF_TOKEN, CONF_HOST_KEY, "secret"}


async def async_get_config_entry_diagnostics(hass: HomeAssistant, entry: SavantConfigEntry) -> dict[str, Any]:
    hub = entry.runtime_data
    try:
        agent = await hub.async_diagnostics()
    except Exception as err:  # noqa: BLE001
        agent = {"error": str(err)}
    return {
        "entry": async_redact_data(dict(entry.data), REDACT),
        "connected": hub.connected,
        "host_available": hub.host_available,
        "agent_version": hub.agent_version,
        "last_repair": hub.last_repair,
        "host": hub.info,
        "entities": async_redact_data(list(hub.entities.values()), REDACT),
        "states": hub.states,
        "agent": agent,
    }
