#!/bin/sh
# Fake sclibridge for tests. States are name=value lines in $FAKE_STATE.
cmd="$1"; shift
echo "$cmd $*" >> "$FAKE_LOG"

get() {
  awk -v k="$1" '{ key=$0; sub(/=.*/, "", key); if (key == k) { v=$0; sub(/^[^=]*=/, "", v); print v; f=1; exit } } END { if (!f) print "" }' "$FAKE_STATE"
}

put() {
  tmp="$FAKE_STATE.$$"
  awk -v k="$1" '{ key=$0; sub(/=.*/, "", key); if (key != k) print }' "$FAKE_STATE" > "$tmp"
  printf '%s=%s\n' "$1" "$2" >> "$tmp"
  mv "$tmp" "$FAKE_STATE"
}

case "$cmd" in
userzones) echo "Room 1" ;;
servicesforzone) [ "$1" = "Room 1" ] && cat "$FAKE_SERVICES" ;;
readstate) for n in "$@"; do get "$n"; done ;;
servicerequest)
  case "$6" in
  PowerOn) put "$1.ActiveService" "$1-$2-$3-$4-$5" ;;
  PowerOff) put "$1.ActiveService" "" ;;
  LockDoorLock) put "Door Lock Controller.Door_lock.DoorLockStatus_1" "Locked" ;;
  UnlockDoorLock) put "Door Lock Controller.Door_lock.DoorLockStatus_1" "Unlocked" ;;
  esac ;;
getSceneNames) echo "Movie Time,CCED55FE-A9E1,dealer@example.com" > "$1" ;;
activateScene | statenames) ;;
*) echo "unknown command $cmd" >&2; exit 2 ;;
esac
