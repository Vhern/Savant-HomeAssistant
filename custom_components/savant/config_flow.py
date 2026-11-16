"""Config flow for Savant."""

from __future__ import annotations

import logging
import secrets
from collections.abc import Mapping
from typing import Any

import aiohttp
import voluptuous as vol
from homeassistant.config_entries import ConfigEntryState, ConfigFlow, ConfigFlowResult
from homeassistant.const import CONF_HOST, CONF_PASSWORD, CONF_USERNAME
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from .const import (
    CONF_AGENT_PORT,
    CONF_HOST_KEY,
    CONF_PRIMARY_UID,
    CONF_PRIVATE_KEY,
    CONF_SSH_PORT,
    CONF_TOKEN,
    DEFAULT_AGENT_PORT,
    DEFAULT_SSH_PORT,
    DEFAULT_USERNAME,
    DOMAIN,
)
from .hub import AgentError, connect_agent
from .installer import (
    AuthFailed,
    CannotConnect,
    HostKeyChanged,
    Installer,
    InstallError,
    UnsupportedHost,
    generate_key,
)

_LOGGER = logging.getLogger(__name__)

USER_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_HOST): str,
        vol.Required(CONF_USERNAME, default=DEFAULT_USERNAME): str,
        vol.Required(CONF_PASSWORD): str,
        vol.Optional(CONF_SSH_PORT, default=DEFAULT_SSH_PORT): vol.All(int, vol.Range(1, 65535)),
    }
)
CONFIRM_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_USERNAME, default=DEFAULT_USERNAME): str,
        vol.Required(CONF_PASSWORD): str,
        vol.Optional(CONF_SSH_PORT, default=DEFAULT_SSH_PORT): vol.All(int, vol.Range(1, 65535)),
    }
)
REAUTH_SCHEMA = vol.Schema({vol.Required(CONF_PASSWORD): str})


class SavantConfigFlow(ConfigFlow, domain=DOMAIN):
    VERSION = 1

    async def _provision(
        self, host: str, port: int, username: str, password: str, token: str | None = None,
        agent_port: int | None = None,
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        agent_port = agent_port or DEFAULT_AGENT_PORT
        private_key, public_key = await self.hass.async_add_executor_job(generate_key)
        token = token or secrets.token_urlsafe(32)
        installer = Installer(self.hass, host, port, username, password=password)
        conn = await installer.connect()
        async with conn:
            await installer.authorize_key(conn, public_key)
            await installer.install(conn, token, agent_port)
            agent_port = await installer.start(conn, token, agent_port)

        keyed = Installer(self.hass, host, port, username, private_key=private_key, host_key=installer.host_key)
        async with await keyed.connect():
            pass

        session = async_get_clientsession(self.hass)
        ws, _, snapshot = await connect_agent(session, host, agent_port, token)
        await ws.close()
        data = {
            CONF_HOST: host,
            CONF_SSH_PORT: port,
            CONF_USERNAME: username,
            CONF_PRIVATE_KEY: private_key,
            CONF_HOST_KEY: installer.host_key,
            CONF_TOKEN: token,
            CONF_AGENT_PORT: agent_port,
        }
        return data, snapshot.get("host") or {}

    error_detail = ""

    async def _try(self, errors: dict[str, str], **kwargs: Any) -> tuple[dict[str, Any], dict[str, Any]] | None:
        self.error_detail = ""
        try:
            return await self._provision(**kwargs)
        except AuthFailed:
            errors["base"] = "invalid_auth"
        except (CannotConnect, HostKeyChanged) as err:
            _LOGGER.error("SSH to the Savant host failed: %s", err)
            errors["base"] = "cannot_connect"
            self.error_detail = str(err)
        except UnsupportedHost as err:
            _LOGGER.error("Unsupported Savant host platform: %s", err)
            errors["base"] = "unsupported_host"
            self.error_detail = str(err)
        except InstallError as err:
            _LOGGER.error("Installing the Savant agent failed: %s", err)
            errors["base"] = "install_failed"
            self.error_detail = str(err)
        except (AgentError, aiohttp.ClientError, TimeoutError, OSError) as err:
            _LOGGER.error("The Savant agent did not answer: %s", err)
            errors["base"] = "agent_unreachable"
            self.error_detail = str(err) or type(err).__name__
        except Exception:
            _LOGGER.exception("Unexpected error setting up Savant")
            errors["base"] = "unknown"
        return None

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            self._async_abort_entries_match({CONF_HOST: user_input[CONF_HOST]})
            res = await self._try(
                errors,
                host=user_input[CONF_HOST],
                port=user_input[CONF_SSH_PORT],
                username=user_input[CONF_USERNAME],
                password=user_input[CONF_PASSWORD],
            )
            if res:
                data, info = res
                await self.async_set_unique_id(info.get("uid") or data[CONF_HOST])
                self._abort_if_unique_id_configured(updates={CONF_HOST: data[CONF_HOST]})
                return self.async_create_entry(title=info.get("name") or data[CONF_HOST], data=data)
        return self.async_show_form(
            step_id="user",
            data_schema=self.add_suggested_values_to_schema(USER_SCHEMA, user_input),
            errors=errors,
            description_placeholders={"error": self.error_detail[-400:]},
        )

    async def async_step_zeroconf(self, discovery_info: ZeroconfServiceInfo) -> ConfigFlowResult:
        """A Savant host announcing itself as _pres_sdo._tcp."""
        uid = (discovery_info.properties.get("PrimaryUID") or "").lower()
        if not uid:
            return self.async_abort(reason="not_savant")
        ipv4 = [ip for ip in discovery_info.ip_addresses if ip.version == 4]
        host = str(ipv4[0] if ipv4 else discovery_info.ip_address)
        name = discovery_info.properties.get("SystemName") or discovery_info.hostname.removesuffix(".local.")

        for entry in self._async_current_entries(include_ignore=False):
            if entry.data.get(CONF_PRIMARY_UID) == uid:
                if entry.data.get(CONF_HOST) != host:
                    self.hass.config_entries.async_update_entry(entry, data={**entry.data, CONF_HOST: host})
                    if entry.state is ConfigEntryState.LOADED:
                        self.hass.config_entries.async_schedule_reload(entry.entry_id)
                return self.async_abort(reason="already_configured")
        self._async_abort_entries_match({CONF_HOST: host})
        await self.async_set_unique_id(uid)
        self._abort_if_unique_id_configured()

        self.discovered = {CONF_HOST: host, CONF_PRIMARY_UID: uid, "name": name}
        self.context["title_placeholders"] = {"name": name}
        return await self.async_step_zeroconf_confirm()

    async def async_step_zeroconf_confirm(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        found = self.discovered
        errors: dict[str, str] = {}
        if user_input is not None:
            res = await self._try(
                errors,
                host=found[CONF_HOST],
                port=user_input[CONF_SSH_PORT],
                username=user_input[CONF_USERNAME],
                password=user_input[CONF_PASSWORD],
            )
            if res:
                data, info = res
                data[CONF_PRIMARY_UID] = found[CONF_PRIMARY_UID]
                await self.async_set_unique_id(info.get("uid") or found[CONF_PRIMARY_UID], raise_on_progress=False)
                self._abort_if_unique_id_configured(updates={CONF_HOST: data[CONF_HOST]})
                return self.async_create_entry(title=info.get("name") or found["name"], data=data)
        return self.async_show_form(
            step_id="zeroconf_confirm",
            data_schema=self.add_suggested_values_to_schema(CONFIRM_SCHEMA, user_input),
            errors=errors,
            description_placeholders={
                "name": found["name"],
                "host": found[CONF_HOST],
                "error": self.error_detail[-400:],
            },
        )

    async def async_step_reauth(self, entry_data: Mapping[str, Any]) -> ConfigFlowResult:
        return await self.async_step_reauth_confirm()

    async def async_step_reauth_confirm(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        entry = self._get_reauth_entry()
        errors: dict[str, str] = {}
        if user_input is not None:
            res = await self._try(
                errors,
                host=entry.data[CONF_HOST],
                port=entry.data[CONF_SSH_PORT],
                username=entry.data[CONF_USERNAME],
                password=user_input[CONF_PASSWORD],
                token=entry.data[CONF_TOKEN],
                agent_port=entry.data[CONF_AGENT_PORT],
            )
            if res:
                data, info = res
                await self.async_set_unique_id(info.get("uid") or data[CONF_HOST])
                self._abort_if_unique_id_mismatch()
                return self.async_update_reload_and_abort(entry, data_updates=data)
        return self.async_show_form(
            step_id="reauth_confirm",
            data_schema=REAUTH_SCHEMA,
            errors=errors,
            description_placeholders={
                "host": entry.data[CONF_HOST],
                "username": entry.data[CONF_USERNAME],
                "error": self.error_detail[-400:],
            },
        )

    async def async_step_reconfigure(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        entry = self._get_reconfigure_entry()
        errors: dict[str, str] = {}
        if user_input is not None:
            res = await self._try(
                errors,
                host=user_input[CONF_HOST],
                port=user_input[CONF_SSH_PORT],
                username=user_input[CONF_USERNAME],
                password=user_input[CONF_PASSWORD],
                token=entry.data[CONF_TOKEN],
                agent_port=entry.data[CONF_AGENT_PORT],
            )
            if res:
                data, info = res
                await self.async_set_unique_id(info.get("uid") or data[CONF_HOST])
                self._abort_if_unique_id_mismatch()
                return self.async_update_reload_and_abort(entry, data_updates=data)
        suggested = {k: entry.data[k] for k in (CONF_HOST, CONF_USERNAME, CONF_SSH_PORT)}
        return self.async_show_form(
            step_id="reconfigure",
            data_schema=self.add_suggested_values_to_schema(USER_SCHEMA, user_input or suggested),
            errors=errors,
            description_placeholders={"error": self.error_detail[-400:]},
        )
