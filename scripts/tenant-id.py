#!/usr/bin/env python3
import base64
import hashlib
import sys

if len(sys.argv) != 2 or not sys.argv[1].strip():
    raise SystemExit(f"usage: {sys.argv[0]} <authenticated-principal>")

principal = sys.argv[1].strip().lower()
digest = hashlib.sha256(("webnix:v1:" + principal).encode()).digest()
print(base64.b32encode(digest).decode().lower()[:26])
