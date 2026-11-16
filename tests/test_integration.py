"""Runs Home Assistant against the real agent and a fake sclibridge."""

from __future__ import annotations

import shutil
import time
from unittest.mock import patch

from homeassistant.config_entries import ConfigEntryState
from homeassistant.core import HomeAssistant
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er

from custom_components.savant.const import CONF_PRIVATE_KEY, DOMAIN
from custom_components.savant.installer import AuthFailed, Installer

from .conftest import HOST_UID, OLD_VERSION, TOKEN, VERSION, wait_for


async def setup(hass: HomeAssistant, host) -> object:
    entry = host.entry()
    entry.add_to_hass(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert entry.state is ConfigEntryState.LOADED
    return entry


async def test_installs_agent_and_builds_entities(hass: HomeAssistant, savant_host) -> None:
    entry = await setup(hass, savant_host)
    assert (savant_host.agent_dir / "savant-ha").exists()
    assert entry.runtime_data.last_repair == "installed"

    media = hass.states.get("media_player.room_1_media")
    assert media is not None, sorted(hass.states.async_entity_ids())
    assert media.state == "off"
    for source in ("ps5", "hd_tv", "hd_tv_2"):
        assert hass.states.get(f"media_player.room_1_{source}").state == "off"
    assert hass.states.get("media_player.room_1_hd_tv").attributes["device_class"] == "tv"
    assert media.attributes["source_list"] == ["HD TV", "HD TV 2", "PS5"]
    assert media.attributes["device_class"] == "tv"
    assert hass.states.get("lock.room_1_lock").state == "unlocked"
    assert hass.states.get("scene.vhern_s_movie_time") is not None
    assert hass.states.get("sensor.vhern_s_cpu_usage").state == "5.4"
    assert hass.states.get("binary_sensor.vhern_s_connection").state == "on"

    devices = dr.async_get(hass)
    hub_dev = devices.async_get_device_by_identifier((DOMAIN, HOST_UID), entry.entry_id)
    for sid, name in (("media_player.room_1", "Media"), ("lock.door_lock_controller_1", "Lock")):
        dev = devices.async_get_device_by_identifier((DOMAIN, f"{HOST_UID}_{sid}"), entry.entry_id)
        assert dev.name == name and dev.suggested_area == "Room 1" and dev.via_device_id == hub_dev.id
        assert dev.area_id == "room_1"
    assert hub_dev.name == "Vhern's" and hub_dev.sw_version == "9.4.6:696" and hub_dev.model == "Smart Host"
    ent = er.async_get(hass).async_get("sensor.vhern_s_cpu_usage")
    assert ent.entity_category == "diagnostic" and ent.unique_id == f"{HOST_UID}_sensor.host_cpu"


async def test_commands_and_live_state(hass: HomeAssistant, savant_host) -> None:
    await setup(hass, savant_host)

    await hass.services.async_call(
        "media_player", "select_source", {"entity_id": "media_player.room_1_media", "source": "PS5"}, blocking=True
    )
    await wait_for(hass, lambda: hass.states.get("media_player.room_1_media").state == "on")
    assert hass.states.get("media_player.room_1_media").attributes["source"] == "PS5"
    assert "servicerequest Room 1 PS5 Game_console 1 SVC_AV_GAME PowerOn" in savant_host.log()

    await wait_for(hass, lambda: hass.states.get("media_player.room_1_ps5").state == "on")
    assert hass.states.get("media_player.room_1_hd_tv").state == "off"
    await hass.services.async_call("media_player", "volume_up", {"entity_id": "media_player.room_1_media"}, blocking=True)
    assert "servicerequest Room 1 PS5 Game_console 1 SVC_AV_GAME VolumeUp" in savant_host.log()

    await hass.services.async_call("media_player", "turn_off", {"entity_id": "media_player.room_1_media"}, blocking=True)
    await wait_for(hass, lambda: hass.states.get("media_player.room_1_media").state == "off")

    await hass.services.async_call("lock", "lock", {"entity_id": "lock.room_1_lock"}, blocking=True)
    await wait_for(hass, lambda: hass.states.get("lock.room_1_lock").state == "locked")
    assert "servicerequest Room 1 Door Lock Controller Door_lock 1 SVC_ENV_DOORLOCK LockDoorLock DoorLockAddress 1" in savant_host.log()

    await hass.services.async_call("scene", "turn_on", {"entity_id": "scene.vhern_s_movie_time"}, blocking=True)
    assert "activateScene Movie Time CCED55FE-A9E1 dealer@example.com" in savant_host.log()

    await hass.services.async_call(
        DOMAIN, "service_request",
        {"zone": "Room 1", "component": "HD TV", "logical_component": "HD_monitor", "service_type": "SVC_AV_TV",
         "request": "PowerOn"},
        blocking=True,
    )
    await wait_for(hass, lambda: hass.states.get("media_player.room_1_media").attributes.get("source") == "HD TV")


async def test_reinstalls_when_agent_is_wiped(hass: HomeAssistant, savant_host) -> None:
    entry = await setup(hass, savant_host)
    savant_host.kill_agent()
    shutil.rmtree(savant_host.agent_dir)

    await wait_for(hass, lambda: hass.states.get("binary_sensor.vhern_s_connection").state == "off")
    assert hass.states.get("media_player.room_1_media").state == "unavailable"
    await wait_for(hass, lambda: hass.states.get("binary_sensor.vhern_s_connection").state == "on", timeout=60)
    assert (savant_host.agent_dir / "savant-ha").exists()
    assert entry.runtime_data.last_repair == "installed"
    await wait_for(hass, lambda: hass.states.get("media_player.room_1_media").state == "off")


async def test_restarts_a_stopped_agent(hass: HomeAssistant, savant_host) -> None:
    entry = await setup(hass, savant_host)
    savant_host.kill_agent()
    await wait_for(hass, lambda: entry.runtime_data.last_repair == "started", timeout=60)
    await wait_for(hass, lambda: hass.states.get("binary_sensor.vhern_s_connection").state == "on")


async def test_updates_an_outdated_agent(hass: HomeAssistant, savant_host) -> None:
    with patch("custom_components.savant.installer.BIN_DIR", savant_host.bin_dirs[OLD_VERSION]):
        await Installer(hass, "127.0.0.1", 22, "RPM", private_key="x").ensure(TOKEN, savant_host.port)
    out = (savant_host.agent_dir / "agent.log").read_text()
    assert OLD_VERSION in out or out == ""

    entry = await setup(hass, savant_host)
    assert entry.runtime_data.last_repair == "updated"
    assert entry.runtime_data.agent_version == VERSION


async def test_user_flow(hass: HomeAssistant, savant_host) -> None:
    with (
        patch("custom_components.savant.config_flow.secrets.token_urlsafe", return_value=TOKEN),
        patch("custom_components.savant.config_flow.DEFAULT_AGENT_PORT", savant_host.port),
    ):
        result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
        assert result["type"] is FlowResultType.FORM
        result = await hass.config_entries.flow.async_configure(
            result["flow_id"], {"host": "127.0.0.1", "username": "RPM", "password": "secret", "ssh_port": 22}
        )
        await hass.async_block_till_done()
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["title"] == "Vhern's"
    assert result["result"].unique_id == HOST_UID
    assert "password" not in result["data"]
    keys = (savant_host.home / ".ssh" / "authorized_keys").read_text()
    assert "home-assistant-savant" in keys and result["data"][CONF_PRIVATE_KEY].startswith("-----BEGIN OPENSSH PRIVATE KEY")
    assert hass.states.get("media_player.room_1_media") is not None


async def test_user_flow_bad_password(hass: HomeAssistant) -> None:
    with patch("custom_components.savant.installer.Installer.connect", side_effect=AuthFailed("denied")):
        result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
        result = await hass.config_entries.flow.async_configure(
            result["flow_id"], {"host": "10.0.0.74", "username": "RPM", "password": "nope", "ssh_port": 22}
        )
    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": "invalid_auth"}


async def test_remove_cleans_host(hass: HomeAssistant, savant_host) -> None:
    entry = await setup(hass, savant_host)
    (savant_host.home / ".ssh").mkdir(exist_ok=True)
    (savant_host.home / ".ssh" / "authorized_keys").write_text("ssh-ed25519 AAAAother me@laptop\nssh-ed25519 AAAAha home-assistant-savant\n")
    assert await hass.config_entries.async_remove(entry.entry_id)
    await hass.async_block_till_done()
    assert not savant_host.agent_dir.exists()
    assert (savant_host.home / ".ssh" / "authorized_keys").read_text() == "ssh-ed25519 AAAAother me@laptop\n"


async def test_new_token_restarts_running_agent(hass: HomeAssistant, savant_host) -> None:
    installer = Installer(hass, "127.0.0.1", 22, "RPM", private_key="x")
    assert await installer.ensure("first-token", savant_host.port) == "installed"
    assert await installer.ensure("first-token", savant_host.port) == "running"
    assert await installer.ensure(TOKEN, savant_host.port) == "restarted"
    await setup(hass, savant_host)
    assert hass.states.get("binary_sensor.vhern_s_connection").state == "on"


async def test_taps_respond_before_savant_confirms(hass: HomeAssistant, savant_host) -> None:
    savant_host.env["FAKE_DELAY"] = "2"
    await setup(hass, savant_host)

    start = time.monotonic()
    await hass.services.async_call("lock", "lock", {"entity_id": "lock.room_1_lock"}, blocking=True)
    assert time.monotonic() - start < 1.5
    assert hass.states.get("lock.room_1_lock").state == "locking"
    await wait_for(hass, lambda: hass.states.get("lock.room_1_lock").state == "locked")

    await hass.services.async_call(
        "media_player", "select_source", {"entity_id": "media_player.room_1_media", "source": "PS5"}, blocking=True
    )
    media = hass.states.get("media_player.room_1_media")
    assert media.state == "on" and media.attributes["source"] == "PS5"


async def test_source_player_turns_on_its_source(hass: HomeAssistant, savant_host) -> None:
    await setup(hass, savant_host)
    await hass.services.async_call("media_player", "turn_on", {"entity_id": "media_player.room_1_hd_tv_2"}, blocking=True)
    await wait_for(hass, lambda: hass.states.get("media_player.room_1_media").attributes.get("source") == "HD TV 2")
    assert "servicerequest Room 1 HD TV 2 HD_monitor 1 SVC_AV_TV PowerOn" in savant_host.log()
    await wait_for(hass, lambda: hass.states.get("media_player.room_1_hd_tv_2").state == "on")
    await hass.services.async_call("media_player", "turn_off", {"entity_id": "media_player.room_1_hd_tv_2"}, blocking=True)
    await wait_for(hass, lambda: hass.states.get("media_player.room_1_media").state == "off")
