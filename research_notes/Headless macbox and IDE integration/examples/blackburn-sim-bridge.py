#!/usr/bin/env python3
"""Launch an already-built simulator .app; foreground SSH tunnel for Dart attach.

No Flutter compiler/reload ownership. Prefer xcross run for hot reload.
Run from the Flutter project, after selecting a dedicated mac-sim UDID.
Syntax checked only; end-to-end validation on macbox is still required.
"""
import argparse
import contextlib
import fcntl
import json
import os
import pathlib
import queue
import re
import shlex
import signal
import socket
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.parse
import urllib.request


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--host", default="macbox", help="SSH alias for macOS guest, not container")
    p.add_argument("--udid", required=True)
    p.add_argument("--app", required=True, type=pathlib.Path)
    p.add_argument("--uri-file", type=pathlib.Path, default=pathlib.Path(".dart_tool/blackburn-vm-service-uri"))
    a = p.parse_args()
    if not re.fullmatch(r"[0-9A-Fa-f-]{36}", a.udid):
        p.error("--udid must be an explicit simulator UUID")
    if not a.app.is_dir() or a.app.suffix != ".app":
        p.error("--app must name the built simulator .app directory")
    a.app = a.app.resolve()
    a.uri_file.parent.mkdir(parents=True, exist_ok=True)
    # One owner per workspace. Never replace another foreground bridge.
    lock = open(str(a.uri_file) + ".lock", "w")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    a.uri_file.unlink(missing_ok=True)
    ssh = ["ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15"]

    def remote(args, **kwargs):
        # SSH concatenates command arguments remotely: quote the complete argv.
        return subprocess.run(ssh + ["--", a.host, shlex.join(args)], check=True, **kwargs)

    def script(body):
        return remote(["/bin/bash", "-se"], input=body, text=True, capture_output=True).stdout.strip()

    stop = threading.Event()
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: stop.set())
    launch = tunnel = None
    stage = bundle = None
    launched = False
    try:
        # Validate selected UDID and guest architecture; bootstatus is authoritative.
        script("""u=%s
test "$(uname -m)" = x86_64
xcrun simctl list devices available -j | /usr/bin/python3 -c 'import json,sys; u=sys.argv[1]; ds=[d for xs in json.load(sys.stdin)["devices"].values() for d in xs]; sys.exit(not any(d["udid"].lower()==u.lower() for d in ds))' "$u"
if ! xcrun simctl boot "$u" 2>/dev/null; then
  xcrun simctl list devices -j | /usr/bin/python3 -c 'import json,sys; ds=[d for xs in json.load(sys.stdin)["devices"].values() for d in xs]; sys.exit(not any(d["udid"].lower()==sys.argv[1].lower() and d["state"]=="Booted" for d in ds))' "$u"
fi
xcrun simctl bootstatus "$u" -b
""" % shlex.quote(a.udid))
        stage = script('mktemp -d /tmp/blackburn-flutter.XXXXXXXX')
        if not re.fullmatch(r"/tmp/blackburn-flutter\.[A-Za-z0-9]+", stage):
            raise RuntimeError("unexpected guest staging directory")
        # Tar preserves executable modes and framework symlinks, without remote rsync.
        with tempfile.TemporaryFile() as archive:
            with tarfile.open(fileobj=archive, mode="w") as tf:
                tf.add(a.app, arcname=a.app.name)
            archive.seek(0)
            remote(["tar", "-C", stage, "-xf", "-"], stdin=archive)
        guest_app = stage + "/" + a.app.name
        bundle = script("""app=%s
exe=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$app/Info.plist")
/usr/bin/lipo -verify_arch x86_64 "$app/$exe"
/usr/bin/codesign --force --deep --sign - "$app" >&2
xcrun simctl install %s "$app" >&2
/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app/Info.plist"
""" % (shlex.quote(guest_app), shlex.quote(a.udid)))
        args = ["xcrun", "simctl", "launch", "--terminate-running-process", "--console-pty", a.udid, bundle,
                "--enable-dart-profiling", "--enable-checked-mode", "--vm-service-host=127.0.0.1", "--vm-service-port=0"]
        launch = subprocess.Popen(ssh + ["--", a.host, shlex.join(args)], stdout=subprocess.PIPE,
                                  stderr=subprocess.STDOUT, text=True, bufsize=1)
        launched = True
        messages = queue.Queue()

        def read_console():
            for line in launch.stdout:
                print(line, end="", file=sys.stderr, flush=True)
                m = re.search(r"The Dart VM service is listening on (http://(?:127\.0\.0\.1|localhost|\[::1\]):\d+/\S*)", line)
                if m:
                    messages.put(m.group(1))
        threading.Thread(target=read_console, daemon=True).start()
        deadline = time.monotonic() + 90
        guest_uri = None
        while not stop.is_set() and time.monotonic() < deadline:
            if launch.poll() is not None:
                raise RuntimeError("simctl exited before VM service discovery")
            try:
                guest_uri = urllib.parse.urlsplit(messages.get(timeout=0.2))
                break
            except queue.Empty:
                pass
        if guest_uri is None:
            raise RuntimeError("VM service discovery timed out or cancelled")
        # The listener is in macOS's network namespace. This is -L, not -R.
        # Retry selection if another process wins the short bind/close race.
        for attempt in range(5):
            with socket.socket() as s:
                s.bind(("127.0.0.1", 0))
                local_port = s.getsockname()[1]
            target = "[::1]" if guest_uri.hostname == "::1" else "127.0.0.1"
            forward = f"127.0.0.1:{local_port}:{target}:{guest_uri.port}"
            tunnel = subprocess.Popen(ssh + ["-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ExitOnForwardFailure=yes", "-N", "-L", forward, "--", a.host])
            local_uri = urllib.parse.urlunsplit(guest_uri._replace(netloc=f"127.0.0.1:{local_port}"))
            ready = False
            for _ in range(50):
                if stop.is_set() or tunnel.poll() is not None:
                    break
                try:
                    # Check the service through the tunnel, preserving its auth path.
                    with urllib.request.urlopen(local_uri + ("" if local_uri.endswith("/") else "/") + "getVM", timeout=0.4) as r:
                        ready = "result" in json.load(r)
                    if ready:
                        break
                except (OSError, ValueError):
                    time.sleep(0.1)
            if ready:
                break
            if tunnel.poll() is None:
                tunnel.terminate()
                tunnel.wait(timeout=5)
        else:
            raise RuntimeError("could not establish VM service tunnel")
        tmp = a.uri_file.with_name(a.uri_file.name + f".{os.getpid()}.tmp")
        tmp.write_text(local_uri + "\n")
        tmp.chmod(0o600)
        tmp.replace(a.uri_file)
        print("BLACKBURN_READY " + local_uri, flush=True)
        while not stop.wait(0.2):
            if launch.poll() is not None or tunnel.poll() is not None:
                raise RuntimeError("simulator console or tunnel exited")
    finally:
        a.uri_file.unlink(missing_ok=True)
        for proc in (tunnel, launch):
            if proc is not None and proc.poll() is None:
                proc.terminate()
                with contextlib.suppress(subprocess.TimeoutExpired):
                    proc.wait(timeout=5)
                if proc.poll() is None:
                    proc.kill()
                    proc.wait()
        # Only the selected bundle/device and this invocation's staging directory.
        # Never shutdown the simulator, pkill ssh, or kill arbitrary port listeners.
        if launched and bundle:
            with contextlib.suppress(Exception):
                remote(["xcrun", "simctl", "terminate", a.udid, bundle], timeout=10, capture_output=True)
        if stage and re.fullmatch(r"/tmp/blackburn-flutter\.[A-Za-z0-9]+", stage):
            with contextlib.suppress(Exception):
                remote(["rm", "-rf", "--", stage], timeout=10, capture_output=True)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.SubprocessError) as e:
        print(f"blackburn-sim: {e}", file=sys.stderr)
        sys.exit(1)
