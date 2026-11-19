"""A host set up like Savant 12 on a Mac, where entities come from serviceImplementation.sqlite."""

from __future__ import annotations

import shutil
import subprocess

from homeassistant.components.camera import async_get_stream_source
from homeassistant.core import HomeAssistant
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from homeassistant.util.unit_system import US_CUSTOMARY_SYSTEM

from custom_components.savant.camera import split_login
from custom_components.savant.const import DOMAIN
from custom_components.savant.diagnostics import async_get_config_entry_diagnostics

from .conftest import FIXTURES, HOST_UID, wait_for
from .test_integration import setup

CONFIG_DIRS = (
    "Library/Application Support/RacePointMedia/userConfig.rpmConfig",
    "GNUstep/Library/ApplicationSupport/RacePointMedia/userConfig.rpmConfig",
)
STATES = {
    "Test Host.Lighting.CurrentColor_4_001": "000,000,000,000,000,100|3000,000,100|Tunable White",
    "Test Host.Lighting.CurrentDimmerLevel_4_001": "40",
    "Test Host.Lighting.CurrentDimmerLevel_2_002": "0",
    "Test Host.Lighting.IsSceneActive_Cove Scene": "1",
    "Test Host.Lighting.CurrentLEDState_1025_002": "1",
    "Test Host.Lighting.GroupShadeLevel_1_002": "35",
    "Energy.Total.Consumption.Power": "72",
    "Energy.TotalToday.Consumption.Power": "76.88",
    "Energy.Circuit.Pendants 1.Power": "20",
    "Den Tstat.HVAC_controller.ThermostatCurrentTemperature_1": "71",
    "Den Tstat.HVAC_controller.ThermostatCurrentHeatPoint_1": "68",
    "Den Tstat.HVAC_controller.ThermostatCurrentCoolPoint_1": "76",
    "Den Tstat.HVAC_controller.ThermostatMode_1": "Auto",
    "Den Tstat.HVAC_controller.ThermostatHVACState_1": "Cooling",
    "Garage Controller.Garage_controller.GarageDoorStatus_2": "Opening",
    "Den Charger.EV_charger.ChargerState_1": "Charging",
    "Den Charger.EV_charger.ChargerPower_1": "7200",
    "Den Heater.Water_heater.Temperature_1": "118",
    "Den Heater.Water_heater.SetPoint_1": "120",
    "Den Heater.Water_heater.Mode_1": "Eco",
    "Alarm Panel.Security_system.CurrentPartitionArmingStatus_1": "Disarmed",
    "Alarm Panel.Security_system.CurrentZoneStatus_3": "Open",
}


def add_app_database(host) -> None:
    sql = (FIXTURES / "appdb.sql").read_text()
    for rel in CONFIG_DIRS:
        cfg = host.home / rel
        subprocess.run(["sqlite3", str(cfg / "serviceImplementation.sqlite")], input=sql, text=True, check=True)
        shutil.copy(FIXTURES / "energyConfig.json", cfg / "energyConfig.json")
    with host.state_file.open("a") as f:
        f.writelines(f"{k}={v}\n" for k, v in STATES.items())


async def test_lights_scenes_keypads_and_shades(hass: HomeAssistant, savant_host) -> None:
    add_app_database(savant_host)
    await setup(hass, savant_host)

    cove = hass.states.get("light.den_lighting_cove")
    assert cove.state == "on"
    assert set(cove.attributes["supported_color_modes"]) == {"rgbw", "color_temp"}
    assert cove.attributes["color_mode"] == "color_temp" and cove.attributes["color_temp_kelvin"] == 3000
    assert cove.attributes["min_color_temp_kelvin"] == 2200 and cove.attributes["max_color_temp_kelvin"] == 6000
    assert hass.states.get("light.den_lighting_pendants").attributes["supported_color_modes"] == ["brightness"]

    await hass.services.async_call(
        "light", "turn_on", {"entity_id": "light.den_lighting_cove", "rgbw_color": [255, 0, 40, 0]}, blocking=True
    )
    assert (
        "servicerequest Den Test Host Lighting 1 SVC_ENV_LIGHTING DimmerSet Address1 001 Address2 4 DimmerLevel 40 "
        "bleColorRed 255 bleColorGreen 0 bleColorBlue 40 bleColorWhite 0" in savant_host.log()
    )
    assert hass.states.get("light.den_lighting_cove").attributes["color_mode"] == "rgbw"

    assert hass.states.get("switch.den_lighting_cove_scene").state == "on"
    await hass.services.async_call("switch", "turn_off", {"entity_id": "switch.den_lighting_cove_scene"}, blocking=True)
    assert "DeactivateScene Address1 Cove Scene" in savant_host.log()

    away = hass.states.get("button.den_keypads_away")
    assert away.attributes["led"] is True
    await hass.services.async_call("button", "press", {"entity_id": "button.den_keypads_away"}, blocking=True)
    assert "ButtonPressAndRelease Address1 002 Address2 1025 Address3 /button/37" in savant_host.log()
    reg = er.async_get(hass)
    hidden = [e for e in er.async_entries_for_config_entry(reg, setup_entry_id(hass)) if e.domain == "button" and e.disabled_by]
    assert len(hidden) == 2  # "1" and the second Away

    shade = hass.states.get("cover.den_shades_sheers")
    assert shade.attributes["current_position"] == 35
    await hass.services.async_call("cover", "stop_cover", {"entity_id": "cover.den_shades_sheers"}, blocking=True)
    assert "GroupShadeStop Address1 002 Address2 1 Address3 /zone/13" in savant_host.log()
    await hass.services.async_call(
        "cover", "set_cover_position", {"entity_id": "cover.den_shades_sheers", "position": 60}, blocking=True
    )
    assert "GroupShadeSet Address1 002 Address2 1 Address3 /zone/13 ShadeLevel 60" in savant_host.log()

    devices = dr.async_get(hass)
    lighting = devices.async_get_device_by_identifier((DOMAIN, f"{HOST_UID}_lighting_den"), setup_entry_id(hass))
    assert lighting.name == "Lighting" and lighting.area_id == "den"
    assert {e.entity_id for e in er.async_entries_for_device(reg, lighting.id)} == {
        "light.den_lighting_cove", "light.den_lighting_lamp", "light.den_lighting_pendants", "switch.den_lighting_cove_scene",
    }


def setup_entry_id(hass: HomeAssistant) -> str:
    return hass.config_entries.async_entries(DOMAIN)[0].entry_id


async def test_cameras_and_energy(hass: HomeAssistant, savant_host) -> None:
    add_app_database(savant_host)
    await setup(hass, savant_host)

    assert hass.states.get("camera.porch_camera") is not None
    assert hass.states.get("camera.blank_camera") is None
    src = await async_get_stream_source(hass, "camera.porch_camera")
    assert src == "rtsp://viewer:secret@10.9.9.2/cam/realmonitor?channel=1&subtype=0"

    assert hass.states.get("sensor.energy_power").state == "72"
    today = hass.states.get("sensor.energy_today")
    assert today.state == "76.88" and today.attributes["device_class"] == "energy"
    assert today.attributes["state_class"] == "total_increasing" and today.attributes["unit_of_measurement"] == "Wh"
    assert hass.states.get("sensor.energy_pendants_1_power").state == "20"

    diag = await async_get_config_entry_diagnostics(hass, hass.config_entries.async_get_entry(setup_entry_id(hass)))
    cam = next(e for e in diag["entities"] if e["id"] == "camera.porch_camera_security_camera1")
    assert cam["secret"] == "**REDACTED**" and "viewer:secret" not in str(diag)


def test_split_login() -> None:
    assert split_login("http://u:p%40ss@10.0.0.2:8080/snap.cgi?ch=1", None, None) == ("http://10.0.0.2:8080/snap.cgi?ch=1", "u", "p@ss")
    assert split_login("http://10.0.0.2/snap.cgi", "admin", "pw") == ("http://10.0.0.2/snap.cgi", "admin", "pw")


async def test_old_devices_are_removed(hass: HomeAssistant, savant_host) -> None:
    entry = savant_host.entry()
    entry.add_to_hass(hass)
    devices = dr.async_get(hass)
    old = devices.async_get_or_create(
        config_entry_id=entry.entry_id, identifiers={(DOMAIN, f"{HOST_UID}_light.vhern_s_001")}, name="Pendants"
    )
    reg = er.async_get(hass)
    stale = reg.async_get_or_create("light", DOMAIN, f"{HOST_UID}_light.gone", config_entry=entry, device_id=old.id)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    assert reg.async_get(stale.entity_id) is None
    assert devices.async_get(old.id) is None
    assert reg.async_get("lock.room_1_locks_lock") is not None

    add_app_database(savant_host)
    await entry.runtime_data.async_rediscover()
    await wait_for(hass, lambda: hass.states.get("light.den_lighting_cove") is not None)
    # The database has no locks, so the one from the data tables stays as it was.
    assert hass.states.get("lock.room_1_locks_lock") is not None
    assert devices.async_get_device_by_identifier((DOMAIN, f"{HOST_UID}_lighting_den"), setup_entry_id(hass)) is not None


async def test_source_remote(hass: HomeAssistant, savant_host) -> None:
    await setup(hass, savant_host)
    remote = hass.states.get("remote.room_1_hd_tv")
    assert "OSDCursorUp" in remote.attributes["commands"] and "PowerOn" not in remote.attributes["commands"]
    await hass.services.async_call(
        "remote", "send_command", {"entity_id": "remote.room_1_hd_tv", "command": ["up", "OSDSelect"]}, blocking=True
    )
    log = savant_host.log()
    assert "SVC_AV_TV OSDCursorUp" in log and "SVC_AV_TV OSDSelect" in log


async def test_thermostat_and_garage(hass: HomeAssistant, savant_host) -> None:
    hass.config.units = US_CUSTOMARY_SYSTEM
    add_app_database(savant_host)
    await setup(hass, savant_host)

    tstat = hass.states.get("climate.den_climate_main_floor")
    assert tstat.state == "heat_cool" and tstat.attributes["hvac_action"] == "cooling"
    assert tstat.attributes["current_temperature"] == 71
    assert tstat.attributes["target_temp_low"] == 68 and tstat.attributes["target_temp_high"] == 76
    assert set(tstat.attributes["hvac_modes"]) == {"off", "heat", "cool", "heat_cool"}
    await hass.services.async_call(
        "climate", "set_temperature",
        {"entity_id": "climate.den_climate_main_floor", "target_temp_low": 66, "target_temp_high": 75}, blocking=True,
    )
    log = savant_host.log()
    assert "SetHeatPointTemperature ThermostatAddress 1 HeatPointTemperature 66" in log
    assert "SetCoolPointTemperature ThermostatAddress 1 CoolPointTemperature 75" in log
    await hass.services.async_call(
        "climate", "set_hvac_mode", {"entity_id": "climate.den_climate_main_floor", "hvac_mode": "off"}, blocking=True
    )
    assert "SetHVACModeOff ThermostatAddress 1" in savant_host.log()

    door = hass.states.get("cover.den_garage_left_door")
    assert door.state == "opening" and door.attributes["device_class"] == "garage"
    await hass.services.async_call("cover", "close_cover", {"entity_id": "cover.den_garage_left_door"}, blocking=True)
    assert "CloseGarageDoor GarageDoorAddress 2" in savant_host.log()


async def test_ev_charger_and_water_heater(hass: HomeAssistant, savant_host) -> None:
    hass.config.units = US_CUSTOMARY_SYSTEM
    add_app_database(savant_host)
    await setup(hass, savant_host)

    assert hass.states.get("sensor.den_driveway_charger_status").state == "Charging"
    assert hass.states.get("sensor.den_driveway_charger_power").state == "7200"
    await hass.services.async_call("button", "press", {"entity_id": "button.den_driveway_charger_stop_charging"}, blocking=True)
    assert "StopCharging Address1 1" in savant_host.log()

    heater = hass.states.get("water_heater.den_water_heater")
    assert heater.attributes["current_temperature"] == 118 and heater.attributes["temperature"] == 120
    assert heater.state == "Eco"
    await hass.services.async_call(
        "water_heater", "set_temperature", {"entity_id": "water_heater.den_water_heater", "temperature": 125}, blocking=True
    )
    assert "SetWaterTemperature Address1 1 Temperature 125" in savant_host.log()


async def test_alarm_and_pool(hass: HomeAssistant, savant_host) -> None:
    add_app_database(savant_host)
    await setup(hass, savant_host)
    assert not hass.states.async_entity_ids("fan")

    panel = hass.states.get("alarm_control_panel.security_house")
    assert panel.state == "disarmed" and panel.attributes["savant_status"] == "Disarmed"
    await hass.services.async_call(
        "alarm_control_panel", "alarm_arm_away", {"entity_id": "alarm_control_panel.security_house", "code": "1234"},
        blocking=True,
    )
    assert "ArmAlarmAway PartitionNumber 1 UserCode 1234" in savant_host.log()
    assert hass.states.get("sensor.security_front_door").state == "Open"

    await hass.services.async_call("switch", "turn_on", {"entity_id": "switch.den_pool_spa_pool_light"}, blocking=True)
    assert "SetAuxiliary2On" in savant_host.log()
    light = hass.states.get("switch.den_pool_spa_pool_light")
    assert light.state == "unknown" and light.attributes["assumed_state"] is True
