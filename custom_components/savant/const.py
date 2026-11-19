"""Constants for Savant."""

from homeassistant.const import Platform

DOMAIN = "savant"

CONF_SSH_PORT = "ssh_port"
CONF_PRIVATE_KEY = "private_key"
CONF_HOST_KEY = "host_key"
CONF_TOKEN = "token"
CONF_AGENT_PORT = "agent_port"
CONF_PRIMARY_UID = "primary_uid"
# Only used when one Home Assistant runs several Savant systems, like the units in a building.
CONF_UNIT = "unit"
CONF_PLACED_UNIT = "placed_unit"
SSH_SLOTS = 3

DEFAULT_USERNAME = "RPM"
DEFAULT_SSH_PORT = 22
DEFAULT_AGENT_PORT = 8765

AGENT_DIR = ".savant-ha"
AGENT_BIN = "savant-ha"
KEY_COMMENT = "home-assistant-savant"

PLATFORMS = [
    Platform.ALARM_CONTROL_PANEL,
    Platform.BINARY_SENSOR,
    Platform.BUTTON,
    Platform.CAMERA,
    Platform.CLIMATE,
    Platform.COVER,
    Platform.LIGHT,
    Platform.LOCK,
    Platform.MEDIA_PLAYER,
    Platform.REMOTE,
    Platform.SCENE,
    Platform.SENSOR,
    Platform.SWITCH,
    Platform.WATER_HEATER,
]

SERVICE_REQUEST = "service_request"
