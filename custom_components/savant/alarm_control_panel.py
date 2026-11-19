"""Savant security partitions."""

from __future__ import annotations

from homeassistant.components.alarm_control_panel import (
    AlarmControlPanelEntity,
    AlarmControlPanelEntityFeature,
    AlarmControlPanelState,
    CodeFormat,
)
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import SavantConfigEntry
from .entity import SavantEntity, metas

F = AlarmControlPanelEntityFeature
FEATURES = {"arm_away": F.ARM_AWAY, "arm_home": F.ARM_HOME, "arm_night": F.ARM_NIGHT, "arm_vacation": F.ARM_VACATION}


async def async_setup_entry(
    hass: HomeAssistant, entry: SavantConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    hub = entry.runtime_data
    async_add_entities(SavantAlarm(hub, m) for m in metas(hub, "alarm_control_panel"))


class SavantAlarm(SavantEntity, AlarmControlPanelEntity):
    _attr_code_format = CodeFormat.NUMBER
    _attr_code_arm_required = True

    def __init__(self, hub, meta) -> None:
        super().__init__(hub, meta)
        features = AlarmControlPanelEntityFeature(0)
        for mode in self.attrs.get("modes") or ():
            features |= FEATURES.get(mode, 0)
        self._attr_supported_features = features

    @property
    def alarm_state(self) -> AlarmControlPanelState | None:
        state = self.data.get("state")
        return AlarmControlPanelState(state) if state else None

    @property
    def extra_state_attributes(self) -> dict[str, str] | None:
        status = self.data.get("status")
        return {"savant_status": status} if status else None

    async def _send(self, action: str, code: str | None) -> None:
        await self.command(action, code=code or "")

    async def async_alarm_disarm(self, code: str | None = None) -> None:
        await self._send("disarm", code)

    async def async_alarm_arm_away(self, code: str | None = None) -> None:
        await self._send("arm_away", code)

    async def async_alarm_arm_home(self, code: str | None = None) -> None:
        await self._send("arm_home", code)

    async def async_alarm_arm_night(self, code: str | None = None) -> None:
        await self._send("arm_night", code)

    async def async_alarm_arm_vacation(self, code: str | None = None) -> None:
        await self._send("arm_vacation", code)
