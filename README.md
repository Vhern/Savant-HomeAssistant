# Savant for Home Assistant

Home Assistant integration for Savant home automation systems.

You add it with the Savant host's IP address, user and password. Home Assistant logs in over SSH, installs a small agent on the host, and talks to that agent from then on. If the agent stops or goes missing, for example after a reboot or a firmware update, Home Assistant reinstalls or restarts it on its own.

## Features

- Lights with brightness and fade time. RGB DMX loads get color, and tunable white loads get color temperature
- Lighting scenes from Blueprint as switches, and keypad buttons as buttons (unnamed or repeated ones start disabled)
- Shades with open, close, stop and position, including shade groups
- Door locks
- A media player for each room, and one for each source in it (a PS5, a TV, Savant Music), with on/off, source, volume, mute and playback controls
- What's playing on music servers that report it (Savant Music, Sonos): title, artist, artwork, shuffle and repeat
- The apps a TV has in Blueprint, and the app shortcuts on an Apple TV, as that source's list
- A remote for each source with on-screen controls, for d-pad cards and automations (`up`, `select`, `menu` or any request name)
- Cameras set up in Blueprint, with snapshots and the live stream
- Energy: whole-home power, today's usage, and power per group and circuit
- Thermostats (single and dual setpoint, fan mode, humidity), garage doors and gates
- Alarm panels (arm away, home, night, vacation, disarm), with each security zone's status
- Pool and spa auxiliaries, EV chargers and water heaters
- Status for each device Savant controls: online, power and input, with its make and model
- Savant Pro Remotes: battery, Wi-Fi, current room and firmware, as the remote reports them
- Savant scenes, including the ones you make in the Savant app
- Host CPU, memory, disk and uptime sensors
- Entities grouped into devices per room: Lighting, Shades, Keypads, Locks, Climate, Garage, Media, plus one device per source, camera and remote
- A `savant.service_request` action for anything else, using the same service requests as Blueprint

Each Savant room shows up as an area with a few devices in it (Lighting, Shades, Keypads, Media) under the Savant host. The host pushes changes, so Home Assistant updates about a second after something changes in Savant.

## Requirements

- A Savant host you can reach over SSH. Tested on an SHR-2000 and a Pro Mac host on Savant 12.
- The host's SSH login, usually `RPM`.
- Port 8765 on the host reachable from Home Assistant.

## Installation

### HACS

1. In HACS, open the menu, choose Custom repositories, and add `https://github.com/Vhern/Savant-HomeAssistant` with the type Integration.
2. Install Savant, then restart Home Assistant.

### Manual

Download `savant.zip` from the latest release, unzip it into `config/custom_components/savant`, and restart Home Assistant.

## Setup

Home Assistant finds Savant hosts on your network by itself. They show up under Settings > Devices & services as discovered, so you only enter the user and password. To add one by hand, go to Add integration, pick Savant, and enter the host's address too.

The password is only used during setup. Home Assistant installs its own SSH key on the host and uses that from then on. If the key stops working, Home Assistant asks for the password again.

Removing the integration also removes the agent and the key from the host.

## Several Savant systems

One Home Assistant can run any number of Savant hosts, for example every unit in a building. Each host is added on its own, with its own agent and login.

With one host nothing changes. When you add a second one, setup also asks for a unit name, like "Unit 101". That host's rooms then show up as "Unit 101 Kitchen", and its building-wide devices (energy, cameras, security) go in a "Unit 101" area. That way two units' kitchens never end up in the same area. You can set or change the unit name later under the host's Configure button. Its devices move along, except any you've put in another area yourself.

To group each unit on the Areas dashboard, put its areas on a floor in Home Assistant.

With more than one host, the `savant.service_request` action needs to know which host to send to.

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

- A room's volume level only shows if the room reports it. Volume up/down and mute work either way.
- Camera snapshots and streams come straight from each camera, so Home Assistant needs to reach the cameras' network.
- Thermostats, garage doors, alarms, pool and spa, EV chargers and water heaters use Savant's own request names and the names in each host's config, but haven't been tried on a real install yet. The Savant entry's diagnostics show what the agent read.
- Pool and spa auxiliaries don't report their state, so their switches stay unknown.
- An alarm shows a state only for Savant's own arming words (ArmedAway, Disarmed...). Zones, EV chargers, water heater extras and Pro Remotes show Savant's values as they are, since their formats and units aren't in the config.
- No fans, and no full Apple TV app list: what their values mean isn't defined anywhere on the host.
