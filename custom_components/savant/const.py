"""Constants for Savant."""

from homeassistant.const import Platform

DOMAIN = "savant"

CONF_SSH_PORT = "ssh_port"
CONF_PRIVATE_KEY = "private_key"
CONF_HOST_KEY = "host_key"
CONF_TOKEN = "token"
CONF_AGENT_PORT = "agent_port"

DEFAULT_USERNAME = "RPM"
DEFAULT_SSH_PORT = 22
DEFAULT_AGENT_PORT = 8765

AGENT_DIR = ".savant-ha"
AGENT_BIN = "savant-ha"
KEY_COMMENT = "home-assistant-savant"

PLATFORMS = [
    Platform.BINARY_SENSOR,
    Platform.BUTTON,
    Platform.COVER,
    Platform.LIGHT,
    Platform.LOCK,
    Platform.MEDIA_PLAYER,
    Platform.SCENE,
    Platform.SENSOR,
]

SERVICE_REQUEST = "service_request"
