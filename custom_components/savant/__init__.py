"""Savant integration for Home Assistant."""

from __future__ import annotations

import logging

import voluptuous as vol
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import CONF_HOST, CONF_USERNAME, EVENT_HOMEASSISTANT_STOP
from homeassistant.core import HomeAssistant, ServiceCall
from homeassistant.exceptions import ConfigEntryAuthFailed, ConfigEntryNotReady, ServiceValidationError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers.typing import ConfigType

from .const import CONF_HOST_KEY, CONF_PRIVATE_KEY, CONF_SSH_PORT, DOMAIN, PLATFORMS, SERVICE_REQUEST
from .hub import SavantHub
from .installer import AuthFailed, HostKeyChanged, Installer, InstallError

_LOGGER = logging.getLogger(__name__)

type SavantConfigEntry = ConfigEntry[SavantHub]

CONFIG_SCHEMA = cv.config_entry_only_config_schema(DOMAIN)

SERVICE_REQUEST_SCHEMA = vol.Schema(
    {
        vol.Optional("config_entry_id"): cv.string,
        vol.Required("zone"): cv.string,
        vol.Required("component"): cv.string,
        vol.Optional("logical_component", default=""): cv.string,
        vol.Optional("variant", default="1"): cv.string,
        vol.Required("service_type"): cv.string,
        vol.Required("request"): cv.string,
        vol.Optional("arguments", default={}): {cv.string: cv.string},
    }
)


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    async def service_request(call: ServiceCall) -> None:
        entries = [e for e in hass.config_entries.async_loaded_entries(DOMAIN)]
        if eid := call.data.get("config_entry_id"):
            entries = [e for e in entries if e.entry_id == eid]
        if not entries:
            raise ServiceValidationError("No loaded Savant host matches")
        hub: SavantHub = entries[0].runtime_data
        await hub.async_service_request(
            zone=call.data["zone"],
            component=call.data["component"],
            logical_component=call.data["logical_component"],
            variant=call.data["variant"],
            service_type=call.data["service_type"],
            request=call.data["request"],
            args=call.data["arguments"],
        )

    hass.services.async_register(DOMAIN, SERVICE_REQUEST, service_request, schema=SERVICE_REQUEST_SCHEMA)
    return True


async def async_setup_entry(hass: HomeAssistant, entry: SavantConfigEntry) -> bool:
    hub = SavantHub(hass, entry)
    try:
        await hub.async_start()
    except (AuthFailed, HostKeyChanged) as err:
        raise ConfigEntryAuthFailed(str(err)) from err
    except Exception as err:
        await hub.async_stop()
        raise ConfigEntryNotReady(f"Savant host {hub.host} not ready: {err}") from err
    entry.runtime_data = hub
    host = dr.async_get(hass).async_get_or_create(config_entry_id=entry.entry_id, **hub.host_device())
    hub.host_device_id = host.id
    entry.async_on_unload(hub.async_stop)
    entry.async_on_unload(hass.bus.async_listen_once(EVENT_HOMEASSISTANT_STOP, hub.async_stop))
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    remove_stale(hass, entry, hub)
    return True


OWN_ENTITIES = ("connection", "restart_agent", "rediscover")


def remove_stale(hass: HomeAssistant, entry: SavantConfigEntry, hub: SavantHub) -> None:
    """Drops entities and devices the Savant config no longer has."""
    if not any(m.get("device") for m in hub.entities.values()):
        return  # the agent couldn't read the config; keep everything until it can
    ent_reg = er.async_get(hass)
    keep = {f"{hub.uid}_{sid}" for sid in hub.entities} | {f"{hub.uid}_{k}" for k in OWN_ENTITIES}
    for ent in er.async_entries_for_config_entry(ent_reg, entry.entry_id):
        if ent.unique_id not in keep:
            ent_reg.async_remove(ent.entity_id)
    dev_reg = dr.async_get(hass)
    devices = {(DOMAIN, hub.uid)} | {hub.device_identifier(m["device"]["id"]) for m in hub.entities.values() if m.get("device")}
    for dev in dr.async_entries_for_config_entry(dev_reg, entry.entry_id):
        if not dev.identifiers & devices:
            dev_reg.async_update_device(dev.id, remove_config_entry_id=entry.entry_id)


async def async_unload_entry(hass: HomeAssistant, entry: SavantConfigEntry) -> bool:
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)


async def async_remove_entry(hass: HomeAssistant, entry: SavantConfigEntry) -> None:
    installer = Installer(
        hass,
        entry.data[CONF_HOST],
        entry.data[CONF_SSH_PORT],
        entry.data[CONF_USERNAME],
        private_key=entry.data[CONF_PRIVATE_KEY],
        host_key=entry.data.get(CONF_HOST_KEY),
    )
    try:
        await installer.remove()
    except InstallError as err:
        _LOGGER.warning("Could not remove the Savant agent from %s: %s", entry.data[CONF_HOST], err)
