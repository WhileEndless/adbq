#!/usr/bin/env python3
# adbq Frida session driver.
#
# Launched by the Go backend as:  python -u frida_driver.py <job.json>
# It spawns or attaches to an Android app, loads one or more JS scripts, and
# streams every console.log / send() / error as one compact JSON object per line
# on stdout (the Go side stamps each with a monotonic seq + timestamp).
#
# Control: stdin is the stop channel. Closing it (or sending a line containing
# "stop") detaches cleanly and exits — portable across platforms (Windows has no
# SIGTERM). We also handle SIGTERM/SIGINT for good measure.
#
# The job JSON: {"serial": str, "package": str, "mode": "spawn"|"attach",
#                "scripts": [{"name": str, "source": str}, ...]}

import sys
import json
import threading
import signal

DRIVER_PROTO = 1


def emit(obj):
    try:
        sys.stdout.write(json.dumps(obj) + "\n")
        sys.stdout.flush()
    except Exception:
        pass


def main():
    try:
        sys.stdout.reconfigure(line_buffering=True)
    except Exception:
        pass

    if len(sys.argv) < 2:
        emit({"type": "fatal", "error": "usage", "detail": "missing job file argument"})
        return 2

    try:
        with open(sys.argv[1], "r", encoding="utf-8") as fh:
            job = json.load(fh)
    except Exception as e:
        emit({"type": "fatal", "error": "bad-job", "detail": str(e)})
        return 2

    serial = job.get("serial") or None
    package = job.get("package")
    mode = job.get("mode", "spawn")
    scripts = job.get("scripts", [])

    try:
        import frida
    except Exception as e:
        emit({"type": "fatal", "error": "no-frida", "detail": str(e)})
        return 1

    emit({"type": "ready", "driverProto": DRIVER_PROTO, "fridaVersion": getattr(frida, "__version__", "")})

    # Resolve the device by adb serial (multi-device safe; needs frida-server
    # already running on the device).
    try:
        device = frida.get_device(serial, timeout=10) if serial else frida.get_usb_device(timeout=10)
    except Exception as e:
        emit({"type": "fatal", "error": "no-device",
              "detail": "could not reach the device — is it connected and frida-server running? (%s)" % e})
        return 1

    stop = threading.Event()
    pid = None
    session = None

    def fail_protocol_or(err, e):
        # A client/server version mismatch surfaces as a ProtocolError whose
        # message mentions matching major versions; flag it distinctly so the UI
        # can offer a one-click rebuild of a matching venv.
        msg = str(e)
        if "major version" in msg or "unable to communicate" in msg:
            emit({"type": "fatal", "error": "version-mismatch", "detail": msg,
                  "clientVersion": getattr(frida, "__version__", "")})
        else:
            emit({"type": "fatal", "error": err, "detail": msg})

    try:
        if mode == "attach":
            session = device.attach(package)
        else:
            pid = device.spawn([package])
            session = device.attach(pid)
    except frida.ProcessNotFoundError as e:
        emit({"type": "fatal", "error": "process-not-found", "detail": str(e)})
        return 1
    except frida.ProtocolError as e:
        fail_protocol_or("attach-failed", e)
        return 1
    except Exception as e:
        fail_protocol_or("attach-failed", e)
        return 1

    def on_detached(reason, *args):
        emit({"type": "detached", "reason": reason})
        stop.set()

    try:
        session.on("detached", on_detached)
    except Exception:
        pass

    def make_handler(name):
        def on_message(message, data):
            mtype = message.get("type")
            if mtype == "send":
                payload = message.get("payload")
                try:
                    text = payload if isinstance(payload, str) else json.dumps(payload)
                except Exception:
                    text = str(payload)
                emit({"type": "send", "script": name, "payload": text,
                      "binary": (len(data) if data else 0)})
            elif mtype == "error":
                emit({"type": "error", "script": name,
                      "payload": message.get("description", ""),
                      "stack": message.get("stack", ""),
                      "fileName": message.get("fileName", ""),
                      "lineNumber": message.get("lineNumber", 0)})
            elif mtype == "log":
                emit({"type": "log", "script": name,
                      "level": message.get("level", "info"),
                      "payload": message.get("payload", "")})
            else:
                emit({"type": "log", "script": name, "level": "info",
                      "payload": json.dumps(message)})
        return on_message

    loaded = 0
    for i, sc in enumerate(scripts):
        name = sc.get("name") or ("script-%d" % i)
        source = sc.get("source", "")
        try:
            script = session.create_script(source)
            script.on("message", make_handler(name))
            script.load()
            loaded += 1
            emit({"type": "loaded", "script": name})
        except Exception as e:
            emit({"type": "error", "script": name, "payload": "failed to load script: %s" % e})

    if mode != "attach" and pid is not None:
        try:
            device.resume(pid)
            emit({"type": "resumed", "pid": pid})
        except Exception as e:
            emit({"type": "fatal", "error": "resume-failed", "detail": str(e)})
            stop.set()

    emit({"type": "status", "stage": "running", "loaded": loaded, "pid": pid})

    # Stop on stdin close / "stop" line, or on a signal.
    def watch_stdin():
        try:
            for line in sys.stdin:
                if "stop" in line.lower():
                    break
        except Exception:
            pass
        stop.set()

    threading.Thread(target=watch_stdin, daemon=True).start()
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    try:
        signal.signal(signal.SIGTERM, lambda *_: stop.set())
    except Exception:
        pass

    stop.wait()

    # Clean teardown so we don't leave a gum agent resident in the target.
    try:
        if session is not None:
            session.detach()
    except Exception:
        pass
    emit({"type": "status", "stage": "stopped"})
    return 0


if __name__ == "__main__":
    sys.exit(main())
