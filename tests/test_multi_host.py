"""Several Savant systems in one Home Assistant, like the units in a building."""

from __future__ import annotations

import pytest
from homeassistant.config_entries import ConfigEntryState
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ServiceValidationError
from homeassistant.helpers import area_registry as ar
from homeassistant.helpers import device_registry as dr

from custom_components.savant.const import DOMAIN

from .conftest import HOST_UID


async def setup_entry(hass: HomeAssistant, entry) -> None:
    entry.add_to_hass(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert entry.state is ConfigEntryState.LOADED


def area_of(hass: HomeAssistant, identifier: str, entry) -> str | None:
    dev = dr.async_get(hass).async_get_device_by_identifier((DOMAIN, identifier), entry.entry_id)
    return ar.async_get(hass).async_get_area(dev.area_id).name if dev.area_id else None


async def test_two_hosts_side_by_side(hass: HomeAssistant, savant_host, second_host) -> None:
    home = savant_host.entry()
    unit_b = second_host.entry(unit="Unit B")
    await setup_entry(hass, home)
    await setup_entry(hass, unit_b)

    # A host without a unit name keeps plain room names; the other one gets its unit in front.
    assert hass.states.get("media_player.room_1_media") is not None
    assert hass.states.get("media_player.unit_b_room_1_media") is not None
    assert area_of(hass, f"{HOST_UID}_media_room_1", home) == "Room 1"
    assert area_of(hass, f"{second_host.uid}_media_room_1", unit_b) == "Unit B Room 1"
    assert area_of(hass, second_host.uid, unit_b) == "Unit B"
    assert area_of(hass, HOST_UID, home) is None

    await hass.services.async_call(
        "media_player", "select_source", {"entity_id": "media_player.unit_b_room_1_media", "source": "PS5"}, blocking=True
    )
    assert "PowerOn" in second_host.log() and "PowerOn" not in savant_host.log()

    request = {"zone": "Room 1", "component": "PS5", "logical_component": "Game_console", "service_type": "SVC_AV_GAME",
               "request": "PowerOff"}
    with pytest.raises(ServiceValidationError):
        await hass.services.async_call(DOMAIN, "service_request", request, blocking=True)
    await hass.services.async_call(
        DOMAIN, "service_request", {**request, "config_entry_id": home.entry_id}, blocking=True
    )
    assert "PowerOff" in savant_host.log() and "PowerOff" not in second_host.log()


async def test_unit_name_moves_devices(hass: HomeAssistant, savant_host) -> None:
    entry = savant_host.entry()
    await setup_entry(hass, entry)
    devices, areas = dr.async_get(hass), ar.async_get(hass)
    lock = devices.async_get_device_by_identifier((DOMAIN, f"{HOST_UID}_locks_room_1"), entry.entry_id)
    devices.async_update_device(lock.id, area_id=areas.async_get_or_create("Garage").id)

    result = await hass.config_entries.options.async_init(entry.entry_id)
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"unit": "Unit A"})
    await hass.async_block_till_done()
    assert entry.options == {"unit": "Unit A"}
    assert area_of(hass, f"{HOST_UID}_media_room_1", entry) == "Unit A Room 1"
    assert area_of(hass, HOST_UID, entry) == "Unit A"
    assert area_of(hass, f"{HOST_UID}_locks_room_1", entry) == "Garage"

    result = await hass.config_entries.options.async_init(entry.entry_id)
    await hass.config_entries.options.async_configure(result["flow_id"], {"unit": ""})
    await hass.async_block_till_done()
    assert entry.options == {}
    assert area_of(hass, f"{HOST_UID}_media_room_1", entry) == "Room 1"
    assert area_of(hass, HOST_UID, entry) is None


async def test_unit_field_only_with_another_host(hass: HomeAssistant, savant_host) -> None:
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
    assert "unit" not in result["data_schema"].schema
    await setup_entry(hass, savant_host.entry())
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
    assert "unit" in result["data_schema"].schema
