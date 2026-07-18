#!/usr/bin/env python3
"""
Teams Busylight Agent v2 (macOS) — bidirectional
Teams local WebSocket API (localhost:8124)  <->  ESP32-S3 round LCD (USB serial)

Down: STATE:available|meeting|muted|sharing|off
Up:   CMD:toggle-mute  ->  {"requestId":N,"apiVersion":"2.0.0","action":"toggle-mute"}

Firmware watchdog blanks after 5s of silence, so we heartbeat every 2s.

Setup:
  Teams > Settings > Privacy > Third-party app API > enable
  pip3 install websockets pyserial
  First run: join a test call, approve the pairing popup once.
"""

import asyncio
import glob
import json
import logging
import time
from pathlib import Path
from urllib.parse import urlencode

import serial
import websockets

TOKEN_FILE = Path.home() / ".teams_busylight_token"
WS_HOST, WS_PORT = "127.0.0.1", 8124
IDENTITY = {
    "protocol-version": "2.0.0",
    "manufacturer": "Sonny",
    "device": "BusyLight-Round",
    "app": "teams-busylight",
    "app-version": "2.0",
}
SERIAL_GLOBS = ["/dev/cu.wchusbserial*", "/dev/cu.usbmodem*", "/dev/cu.usbserial*"]
BAUD = 115200
HEARTBEAT = 2.0        # firmware watchdog is 5s
LOG = logging.getLogger("busylight")


class Light:
    def __init__(self):
        self.port = None
        self.state = "off"

    def _ensure(self):
        if self.port and self.port.is_open:
            return True
        for pat in SERIAL_GLOBS:
            for path in glob.glob(pat):
                try:
                    self.port = serial.Serial(path, BAUD, timeout=0)
                    time.sleep(0.5)
                    LOG.info("Serial connected: %s", path)
                    return True
                except serial.SerialException as exc:
                    LOG.debug("open %s failed: %s", path, exc)
        return False

    def send(self, state=None):
        if state is not None:
            self.state = state
        if not self._ensure():
            return
        try:
            self.port.write(f"STATE:{self.state}\n".encode())
        except serial.SerialException:
            try: self.port.close()
            finally: self.port = None

    def read_cmds(self):
        """Non-blocking: return list of CMD strings from the device."""
        cmds = []
        if not self._ensure():
            return cmds
        try:
            while self.port.in_waiting:
                line = self.port.readline().decode(errors="ignore").strip()
                if line.startswith("CMD:"):
                    cmds.append(line[4:])
        except serial.SerialException:
            try: self.port.close()
            finally: self.port = None
        return cmds


def load_token():
    try: return TOKEN_FILE.read_text().strip()
    except FileNotFoundError: return ""

def save_token(tok):
    TOKEN_FILE.write_text(tok)
    TOKEN_FILE.chmod(0o600)
    LOG.info("Token refreshed")

def map_state(ms: dict) -> str:
    if not ms.get("isInMeeting", False):
        return "available"
    if ms.get("isSharing") or ms.get("isRecordingOn"):
        return "sharing"
    if ms.get("isMuted"):
        return "muted"
    return "meeting"


async def run(light: Light):
    req_id = 0
    while True:
        params = dict(IDENTITY)
        if (tok := load_token()):
            params["token"] = tok
        uri = f"ws://{WS_HOST}:{WS_PORT}?{urlencode(params)}"
        try:
            async with websockets.connect(uri) as ws:
                LOG.info("Connected to Teams local API")
                light.send("available" if light.state == "off" else light.state)
                while True:
                    # heartbeat + poll device commands while waiting
                    try:
                        raw = await asyncio.wait_for(ws.recv(), timeout=0.25)
                    except asyncio.TimeoutError:
                        raw = None

                    for cmd in light.read_cmds():
                        req_id += 1
                        await ws.send(json.dumps({
                            "requestId": req_id,
                            "apiVersion": "2.0.0",
                            "action": cmd,          # e.g. toggle-mute
                        }))
                        LOG.info("Sent action: %s (#%d)", cmd, req_id)

                    now = time.monotonic()
                    if not hasattr(run, "_hb") or now - run._hb > HEARTBEAT:
                        run._hb = now
                        light.send()                # re-send current state

                    if raw is None:
                        continue
                    msg = json.loads(raw)
                    if "tokenRefresh" in msg:
                        save_token(msg["tokenRefresh"])
                    if "meetingUpdate" in msg:
                        ms = msg["meetingUpdate"].get("meetingState", {})
                        if ms:
                            st = map_state(ms)
                            if st != light.state:
                                LOG.info("state -> %s", st)
                            light.send(st)
        except (websockets.WebSocketException, ConnectionError, OSError) as exc:
            LOG.warning("Teams WS down (%s)", exc)
            light.send("off")
        await asyncio.sleep(5)


def main():
    logging.basicConfig(level=logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s")
    light = Light()
    light.send("off")
    asyncio.run(run(light))

if __name__ == "__main__":
    main()
