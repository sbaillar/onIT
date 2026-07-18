#!/usr/bin/env python3
"""
Busylight state tester — drive the display by hand, no Teams needed.

Usage:
  .venv/bin/python test_states.py            # interactive prompt
  .venv/bin/python test_states.py meeting    # start in a state

Type a state (or its first letter) and hit enter:
  available | meeting | muted | sharing | off   (a/m/u/s/o)
Ctrl-C or 'q' to quit. Device CMD lines (e.g. tap -> toggle-mute) are printed.
"""

import glob
import sys
import threading
import time

import serial

SERIAL_GLOBS = ["/dev/cu.wchusbserial*", "/dev/cu.usbmodem*", "/dev/cu.usbserial*"]
BAUD = 115200
HEARTBEAT = 2.0  # firmware watchdog is 5s
STATES = ["available", "meeting", "muted", "sharing", "off"]

state = "off"
running = True


def open_port():
    for pat in SERIAL_GLOBS:
        for path in glob.glob(pat):
            try:
                port = serial.Serial(path, BAUD, timeout=0)
                time.sleep(0.5)
                print(f"connected: {path}")
                return port
            except serial.SerialException:
                pass
    sys.exit("no serial device found (is the agent still holding the port?)")


def pump(port):
    """Heartbeat current state + print device commands."""
    last = 0.0
    while running:
        now = time.monotonic()
        if now - last > HEARTBEAT:
            last = now
            port.write(f"STATE:{state}\n".encode())
        while port.in_waiting:
            line = port.readline().decode(errors="ignore").strip()
            if line:
                print(f"\ndevice: {line}\n> ", end="", flush=True)
        time.sleep(0.1)


def resolve(text):
    text = text.strip().lower()
    for s in STATES:
        if text == s or (len(text) == 1 and s.startswith(text)):
            return s
    return None


def main():
    global state, running
    if len(sys.argv) > 1:
        state = resolve(sys.argv[1]) or state
    port = open_port()
    threading.Thread(target=pump, args=(port,), daemon=True).start()
    print(f"states: {' | '.join(STATES)} (first letter works), q to quit")
    print(f"state -> {state}")
    try:
        while True:
            text = input("> ")
            if text.strip().lower() in ("q", "quit", "exit"):
                break
            new = resolve(text)
            if new:
                state = new
                port.write(f"STATE:{state}\n".encode())
                print(f"state -> {state}")
            elif text.strip():
                print("unknown state")
    except (KeyboardInterrupt, EOFError):
        pass
    running = False
    port.write(b"STATE:off\n")
    port.close()


if __name__ == "__main__":
    main()
