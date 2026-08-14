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
import os
import re
import json
import threading
import signal

DRIVER_PROTO = 2

# Frida 17 dropped the legacy Java/ObjC/Swift globals from the agent runtime.
# When a script references one, we prepend the matching bridge implementation
# (shipped by frida-tools, extracted by the Go side into bridgesDir) wrapped the
# same way frida-tools' repl agent does: run the bridge source, which defines a
# local `bridge`, then publish it as the global. Without this, pinning-bypass
# scripts fail with "ReferenceError: 'Java' is not defined".
_BRIDGES = [("Java", "java.js"), ("ObjC", "objc.js"), ("Swift", "swift.js")]

# Every selected script is loaded as ONE frida script rather than one script
# each. Frida gives each script its own isolated JS realm, so a per-script
# prologue meant a per-script copy of the Java bridge — and two bridges patching
# ART in the same process crash the target app on startup. Measured on an API 34
# emulator: one Java-using script works, two kill the process before any hook
# runs, and merging them into a single script with one bridge works. This is also
# what the `frida` CLI does with several `-l` files.
#
# Merging costs the per-script identity frida would otherwise stamp on each
# message, so the bodies run inside per-script scopes whose `console` and `send`
# are shadowed by tagged shims; the driver unwraps the tag and restores the
# script name the UI groups and filters by.
_TAG = "__adbq"

# _RUNTIME defines the shim factory. It captures the real `send` before any user
# body can shadow it, and mirrors frida's console semantics (strings verbatim,
# everything else JSON-encoded, warn/error carrying their level).
_RUNTIME = """
(function(){
  var emit = send;
  // Mirror frida's own console formatting, or scripts that print a buffer or a
  // bare undefined would render differently here than under the frida CLI.
  var fmt = function(a){
    if (typeof a === 'string') return a;
    if (a === undefined) return 'undefined';
    if (a === null) return 'null';
    if (a instanceof ArrayBuffer) { try { return hexdump(a); } catch (e) { /* fall through */ } }
    if (a instanceof Error) return (a.stack || String(a));
    try {
      var s = JSON.stringify(a);
      return (s === undefined) ? String(a) : s;
    } catch (e) { return String(a); }
  };
  var maker = function(name, level){
    return function(){
      var parts = [];
      for (var i = 0; i < arguments.length; i++) parts.push(fmt(arguments[i]));
      emit({ %(tag)s: name, log: { level: level, text: parts.join(' ') } });
    };
  };
  Object.defineProperty(globalThis, '%(ctx)s', { configurable: true, value: function(name){
    return {
      console: {
        log: maker(name, 'info'), info: maker(name, 'info'), debug: maker(name, 'info'),
        warn: maker(name, 'warning'), error: maker(name, 'error')
      },
      send: function(payload, data){ emit({ %(tag)s: name, payload: payload }, data); }
    };
  }});
})();
""".strip() % {"tag": json.dumps(_TAG), "ctx": _TAG + "_ctx"}


def needed_bridges(sources):
    """Bridge globals referenced by any of the sources, in _BRIDGES order."""
    return [(name, fname) for name, fname in _BRIDGES
            if any(re.search(r"\b" + name + r"\b", s) for s in sources)]


def bridge_prologue(sources, bridges_dir):
    """The bridge implementations the given sources need, emitted once each."""
    if not bridges_dir or not os.path.isdir(bridges_dir):
        return ""
    parts = []
    for name, fname in needed_bridges(sources):
        try:
            with open(os.path.join(bridges_dir, fname), "r", encoding="utf-8") as fh:
                src = fh.read().strip()
        except Exception:
            continue
        if not src:
            continue
        # Keep it on one line so user-script line numbers barely shift (the
        # bridge sources are minified single-line bundles).
        parts.append(
            "(function(){ %s\n;Object.defineProperty(globalThis,'%s',{value:bridge,configurable:true}); })();"
            % (src, name)
        )
    return "".join(parts)


def build_agent(scripts, bridges_dir):
    """Compose the single agent source, and a line -> script-name index.

    The index lets an error frida reports by line number be attributed back to
    the script that caused it, which merging would otherwise erase.
    """
    sources = [sc.get("source", "") for sc in scripts]
    parts = [bridge_prologue(sources, bridges_dir), _RUNTIME]
    line_owner = []  # (first_line_of_body, name), ascending

    def line_count(s):
        return s.count("\n") + 1

    cursor = sum(line_count(p) for p in parts) + 1
    for i, sc in enumerate(scripts):
        name = sc.get("name") or ("script-%d" % i)
        head = ("(function(){ var __c = %s(%s); var console = __c.console, send = __c.send;\n"
                % (_TAG + "_ctx", json.dumps(name)))
        body = sc.get("source", "")
        tail = "\n})();"
        line_owner.append((cursor + line_count(head) - 1, name))
        chunk = head + body + tail
        parts.append(chunk)
        cursor += line_count(chunk)
    return "\n".join(parts), line_owner


def owner_of_line(line_owner, line):
    """The script owning a reported line number ('' when it predates the bodies)."""
    owner = ""
    for start, name in line_owner:
        if line >= start:
            owner = name
        else:
            break
    return owner


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
    bridges_dir = job.get("bridgesDir") or ""

    try:
        import frida
    except Exception as e:
        emit({"type": "fatal", "error": "no-frida", "detail": str(e)})
        return 1

    emit({"type": "ready", "driverProto": DRIVER_PROTO, "fridaVersion": getattr(frida, "__version__", "")})

    # Resolve the device. Normally by adb serial (multi-device safe), which uses
    # frida's Android backend — but that backend only ever dials frida's default
    # port on the device, so a server on any other port is reached through an adb
    # forward the Go side opened for us, as a remote device.
    remote_address = job.get("remoteAddress") or ""
    device_manager = frida.get_device_manager()
    try:
        if remote_address:
            device = device_manager.add_remote_device(remote_address)
        elif serial:
            device = frida.get_device(serial, timeout=10)
        else:
            device = frida.get_usb_device(timeout=10)
    except Exception as e:
        if remote_address:
            detail = ("could not reach frida-server on port %s through %s (%s)"
                      % (job.get("port") or "?", remote_address, e))
        else:
            detail = "could not reach the device — is it connected and frida-server running? (%s)" % e
        emit({"type": "fatal", "error": "no-device", "detail": detail})
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

    def make_handler(line_owner):
        def as_text(payload):
            if isinstance(payload, str):
                return payload
            try:
                return json.dumps(payload)
            except Exception:
                return str(payload)

        def on_message(message, data):
            mtype = message.get("type")
            if mtype == "send":
                payload = message.get("payload")
                # A tagged envelope came from a script body's shimmed console/send
                # (see _RUNTIME); unwrap it so the message carries the name the
                # user gave that script. Anything else — the bridges' own sends,
                # or a script that grabbed the real `send` — stays unattributed.
                if isinstance(payload, dict) and _TAG in payload:
                    name = payload.get(_TAG) or ""
                    if "log" in payload:
                        entry = payload.get("log") or {}
                        emit({"type": "log", "script": name,
                              "level": entry.get("level", "info"),
                              "payload": entry.get("text", "")})
                        return
                    emit({"type": "send", "script": name,
                          "payload": as_text(payload.get("payload")),
                          "binary": (len(data) if data else 0)})
                    return
                emit({"type": "send", "payload": as_text(payload),
                      "binary": (len(data) if data else 0)})
            elif mtype == "error":
                emit({"type": "error",
                      "script": owner_of_line(line_owner, message.get("lineNumber", 0) or 0),
                      "payload": message.get("description", ""),
                      "stack": message.get("stack", ""),
                      "fileName": message.get("fileName", ""),
                      "lineNumber": message.get("lineNumber", 0)})
            elif mtype == "log":
                emit({"type": "log", "level": message.get("level", "info"),
                      "payload": message.get("payload", "")})
            else:
                emit({"type": "log", "level": "info", "payload": json.dumps(message)})
        return on_message

    def on_agent_log(level, text):
        # frida-python never routes console.* through the "message" callback: it
        # intercepts type=="log" in Script._on_message and calls the script's log
        # handler, whose default prints info to stdout and everything else to
        # stderr. That bypassed our JSON protocol entirely — warn/error output
        # vanished into stderr, and info lines raced the JSON writes on stdout.
        # Script bodies are shimmed to route console through send instead, so
        # what reaches here is the bridges' own output, which belongs to no
        # single script.
        emit({"type": "log", "level": level or "info",
              "payload": text if isinstance(text, str) else str(text)})

    loaded = 0
    names = [sc.get("name") or ("script-%d" % i) for i, sc in enumerate(scripts)]
    if scripts:
        agent_source, line_owner = build_agent(scripts, bridges_dir)
        try:
            script = session.create_script(agent_source)
            script.on("message", make_handler(line_owner))
            # Older bindings may lack set_log_handler; there console.log still
            # reaches us as a raw stdout line the Go side wraps as a log entry.
            try:
                script.set_log_handler(on_agent_log)
            except Exception:
                pass
            script.load()
            loaded = len(scripts)
            for name in names:
                emit({"type": "loaded", "script": name})
        except Exception as e:
            # One agent means one failure: a syntax error in any body stops them
            # all, so name them rather than blaming an arbitrary one.
            emit({"type": "error", "script": ", ".join(names),
                  "payload": "failed to load script: %s" % e})

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
    # Drop the remote device too, or frida keeps the forwarded connection open
    # and the Go side's adb forward outlives the session it belonged to.
    if remote_address:
        try:
            device_manager.remove_remote_device(remote_address)
        except Exception:
            pass
    emit({"type": "status", "stage": "stopped"})
    return 0


if __name__ == "__main__":
    sys.exit(main())
