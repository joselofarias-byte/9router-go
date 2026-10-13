import hashlib
import json
import subprocess
import time
import urllib.error
import urllib.request
import re
import xml.etree.ElementTree as ET
from pathlib import Path

PACKAGE = "com.jolufasoft.capimux"
ORIGIN = "http://127.0.0.1:20131"

def adb(*args):
    return subprocess.check_output(["adb", *args], text=True, timeout=30).strip()

def tap_button(label):
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
                if node.get("text") != label:
                    continue
                match = re.fullmatch(r"\[(\d+),(\d+)\]\[(\d+),(\d+)\]", node.get("bounds", ""))
                if match:
                    x1, y1, x2, y2 = map(int, match.groups())
                    adb("shell", "input", "tap", str((x1+x2)//2), str((y1+y2)//2))
                    return
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
# Exercise the stop button through UIAutomator without exporting the service.
tap_button("Detener")
wait_health(False)
print("APK_NATIVE_ENGINE_STOP_PASS")
tap_button("Iniciar")
wait_health(True)
assert token() == credential
assert request("/api/settings", credential).get("language") == "es"
print("APK_RESTART_PRESERVES_DATABASE_PASS")
adb("shell", "input", "keyevent", "3")
time.sleep(3)
wait_health(True)
print("APK_BACKGROUND_SERVICE_PASS")
