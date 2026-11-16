# Savant for Home Assistant

Home Assistant integration for Savant home automation systems.

You add it with the Savant host's IP address, user and password. Home Assistant logs in over SSH, installs a small agent on the host, and talks to that agent from then on. If the agent stops or goes missing, for example after a reboot or a firmware update, Home Assistant reinstalls or restarts it on its own.

## Features

- Lights, including DMX loads, with brightness and fade time
- Shades with open, close, stop and position
- Door locks
- A media player for each room, and one for each source in it (a PS5, a TV), with on/off, source, volume and mute
- The apps a TV has in Blueprint (Netflix, Hulu, YouTube...) as that TV's source list, with the running app shown on the room
- Savant scenes, including the ones you make in the Savant app
- Host CPU, memory, disk and uptime sensors
- A `savant.service_request` action for anything else, using the same service requests as Blueprint

Each Savant room shows up as an area, and each light, shade, lock and room media player is its own device under the Savant host. The host pushes changes, so Home Assistant updates about a second after something changes in Savant.

## Requirements

- A Savant host you can reach over SSH. Tested on an SHR-2000. Pro Mac hosts should work but haven't been tested yet.
- The host's SSH login, usually `RPM`.
- Port 8765 on the host reachable from Home Assistant.

## Installation

### HACS

1. In HACS, open the menu, choose Custom repositories, and add `https://github.com/Vhern/Savant-HomeAssistant` with the type Integration.
2. Install Savant, then restart Home Assistant.

### Manual

Download `savant.zip` from the latest release, unzip it into `config/custom_components/savant`, and restart Home Assistant.

## Setup

Go to Settings > Devices & services > Add integration, pick Savant, and enter the host's address, user and password.

The password is only used during setup. Home Assistant installs its own SSH key on the host and uses that from then on. If the key stops working, Home Assistant asks for the password again.

Removing the integration also removes the agent and the key from the host.

## How it works

The agent in `agent/` is a small Go program that runs on the Savant host as the Savant user, in `~/.savant-ha`. It uses Savant's `sclibridge` tool to read states and send requests. To find rooms, loads, sources and the requests each service supports, it reads the host's Blueprint configuration. It never changes that configuration.

Home Assistant connects to the agent over a WebSocket on port 8765. The agent checks Savant once a second and only sends what changed.

## Development

You need Go and Python 3.14 or newer.

```bash
make agent              # builds the agent into custom_components/savant/bin
make test-agent
pip install -r requirements_test.txt
make test-integration   # runs Home Assistant against the real agent and a fake sclibridge
make zip                # builds dist/savant.zip
```

To release, bump `version` in `custom_components/savant/manifest.json`, commit, and push a matching tag:

```bash
git tag v0.4.1
git push origin v0.4.1
```

The release workflow builds the agent and publishes the release with `savant.zip` already attached, titled after the commit message. Don't create the release on GitHub yourself; if one already exists for the tag, the workflow only uploads the zip to it.

You can also run the agent by hand on a host:

```bash
~/.savant-ha/savant-ha discover
~/.savant-ha/savant-ha read "Room 1.ActiveService"
~/.savant-ha/savant-ha request "Room 1-PS5-Game_console-1-SVC_AV_GAME" PowerOn
```

## Known limitations

- No climate (HVAC) support yet.
- No now-playing info yet.
- A room's volume level only shows if the room reports it. Volume up/down and mute work either way.
