import hashlib
import json
import subprocess
import time
import urllib.error
import urllib.request
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

PACKAGE = "com.jolufasoft.capimux"
ORIGIN = "http://127.0.0.1:20131"

def adb(*args):
    return subprocess.check_output(["adb", *args], text=True, timeout=30).strip()

def tap_button(label, attribute="text", tap=True, checked=None):
    # Native startup may finish before WebView's first frame is accessible.
    deadline = time.monotonic() + 75
    while time.monotonic() < deadline:
        try:
            adb("shell", "input", "keyevent", "224")
            adb("shell", "wm", "dismiss-keyguard")
            adb("shell", "rm", "-f", "/sdcard/window.xml")
            adb("shell", "uiautomator", "dump", "--compressed", "/sdcard/window.xml")
            root = ET.fromstring(adb("shell", "cat", "/sdcard/window.xml"))
            for node in root.iter("node"):
                # Android themes may expose dialog action labels in uppercase.
                if node.get(attribute, "").casefold() != label.casefold():
                    continue
                if checked is not None and node.get("checked") != checked:
                    continue
                match = re.fullmatch(r"\[(\d+),(\d+)\]\[(\d+),(\d+)\]", node.get("bounds", ""))
                if match:
                    x1, y1, x2, y2 = map(int, match.groups())
                    if tap:
                        adb("shell", "input", "tap", str((x1+x2)//2), str((y1+y2)//2))
                    return node
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired, ET.ParseError):
            pass
        time.sleep(2)
    raise RuntimeError("Native button did not become accessible: " + label)

def request(path, token=None, payload=None, method=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["x-9r-cli-token"] = token
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(ORIGIN + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=3) as response:
        return json.loads(response.read())

def wait_health(online):
    for _ in range(90):
        try:
            available = request("/health").get("status") == "ok"
        except (OSError, urllib.error.URLError):
            available = False
        if available == online:
            return
        time.sleep(1)
    for command in [
        ("shell", "run-as", PACKAGE, "cat", "files/engine.log"),
        ("logcat", "-d", "-s", "AndroidRuntime"),
        ("shell", "dumpsys", "activity", "services", PACKAGE),
    ]:
        try:
            print(adb(*command))
        except subprocess.CalledProcessError:
            print("Diagnostic command unavailable")
    raise RuntimeError("Unexpected native engine health state")

def token():
    # Create the test-only CLI auth files through the real auth guard.
    try:
        request("/api/settings", token="invalid-probe")
    except urllib.error.HTTPError as error:
        assert error.code == 401
    machine = adb("shell", "run-as", PACKAGE, "cat", "files/engine/machine-id")
    secret = adb("shell", "run-as", PACKAGE, "cat", "files/engine/auth/cli-secret")
    return hashlib.sha256((machine + "9r-cli-auth" + secret).encode()).hexdigest()[:16]

def failure_details(exc_type, exc, tb):
    # Only selection/UI state: never print local auth secrets or tokens.
    try:
        print("Saved native selection:", adb("shell", "run-as", PACKAGE, "cat", "files/multiagent-policy.json"), flush=True)
        if "credential" in globals():
            print("Engine selection:", request("/api/multiagent/policy", credential), flush=True)
        with open("android/model-picker-failure.png", "wb") as output:
            subprocess.run(["adb", "exec-out", "screencap", "-p"], stdout=output, check=True, timeout=30)
        adb("shell", "uiautomator", "dump", "--compressed", "/sdcard/failure.xml")
        print("Native UI:", adb("shell", "cat", "/sdcard/failure.xml"), flush=True)
    except Exception as diagnostic:
        print("UI diagnostic unavailable:", type(diagnostic).__name__, flush=True)
    sys.__excepthook__(exc_type, exc, tb)

sys.excepthook = failure_details

adb("install", "-r", "android/app/build/outputs/apk/emulator/debug/app-emulator-debug.apk")
adb("shell", "pm", "grant", PACKAGE, "android.permission.POST_NOTIFICATIONS")
adb("forward", "tcp:20131", "tcp:20131")
adb("shell", "am", "start", "-W", "-n", PACKAGE + "/.MainActivity")
wait_health(True)
credential = token()
request("/api/settings", credential, {"language": "es"}, "PATCH")
assert request("/api/settings", credential).get("language") == "es"
assert request("/api/multiagent/policy", credential).get("max_models") == 8
adb("shell", "run-as", PACKAGE, "test", "-s", "files/engine/db/data.sqlite")
print("APK_NATIVE_ENGINE_START_PASS")
# Select through the actual native checklist and observe live policy without
# restarting the process. No inference or upstream network request is made.
catalog = request("/api/multiagent/models", credential)
assert catalog["mode"] == "connected"
entries = sorted(catalog["data"], key=lambda e: (e["provider"], e["id"].split("/", 1)[-1]))
assert entries, "No candidate models from connected/noAuth catalog"
first = entries[0]
label = first["provider"] + " · " + first["id"].split("/", 1)[-1]
pid = adb("shell", "pidof", "libcapimux.so")
tap_button("Modelos")
tap_button(label, "content-desc")
tap_button(label, "content-desc", tap=False, checked="true")
tap_button("Habilitar envíos a los modelos seleccionados")
tap_button("Guardar")
deadline = time.monotonic() + 20
while True:
    policy = request("/api/multiagent/policy", credential)
    if policy["models"] == [first["id"]] and policy["external_enabled"]:
        break
    if time.monotonic() >= deadline:
        raise AssertionError("Native selection was not saved: " + repr(policy))
    time.sleep(0.25)
assert policy["external_enabled"] is True
assert adb("shell", "pidof", "libcapimux.so") == pid
preview = request("/api/multiagent/run", credential, {
    "prompt": "Native picker smoke", "models": policy["models"], "dry_run": True
}, "POST")
assert preview["dry_run"] is True
print("APK_MODEL_PICKER_LIVE_SAVE_PASS")
tap_button("Modelos")
assert tap_button(label, "content-desc", tap=False)["checked"] == "true"
subprocess.run(["adb", "exec-out", "screencap", "-p"], stdout=open("android/model-picker.png", "wb"), check=True, timeout=30)
tap_button("Cancelar")
print("APK_MODEL_PICKER_REMEMBERS_SELECTION_PASS")
# Exercise the stop button through UIAutomator without exporting the service.
tap_button("Detener")
wait_health(False)
print("APK_NATIVE_ENGINE_STOP_PASS")
tap_button("Iniciar")
wait_health(True)
assert token() == credential
assert request("/api/settings", credential).get("language") == "es"
assert request("/api/multiagent/policy", credential)["models"] == [first["id"]]
print("APK_RESTART_PRESERVES_DATABASE_PASS")
adb("shell", "input", "keyevent", "3")
time.sleep(3)
wait_health(True)
print("APK_BACKGROUND_SERVICE_PASS")
