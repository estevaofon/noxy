import sys

def is_gil_enabled():
    return sys._is_gil_enabled()

if is_gil_enabled():
    print("GIL is enabled")
else:
    print("GIL is disabled (free-threading build active)")

