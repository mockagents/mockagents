"""Fail if an already-published PyPI file differs from the candidate."""
import hashlib
import json
import pathlib
import os
import sys
import urllib.error
import urllib.request

root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
try:
    url = os.environ.get("PYPI_JSON_URL", f"https://pypi.org/pypi/mockagents/{version}/json")
    with urllib.request.urlopen(url) as response:
        published = {item["filename"]: item["digests"]["sha256"] for item in json.load(response)["urls"]}
except urllib.error.HTTPError as exc:
    if exc.code == 404:
        published = {}
    else:
        raise

for candidate in root.iterdir():
    if candidate.is_file() and candidate.name in published:
        actual = hashlib.sha256(candidate.read_bytes()).hexdigest()
        if actual != published[candidate.name]:
            raise SystemExit(f"existing PyPI file differs from candidate: {candidate.name}")
        print(f"verified existing PyPI file: {candidate.name}")
