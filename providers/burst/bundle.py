#!/usr/bin/env python3
"""Build step (make bundle, scripts/bundle.sh): copies Burst's providers.json and icons into this package.

The file is copied as it is: Burst's scripts/builtin.py keeps a "builtin" part in every provider, which
is what Elementum reads. Icons are scaled down to 96 pixels to keep the binary small.

Usage: bundle.py [path to script.elementum.burst]   (default: $ELEMENTUM_BURST_PATH, or next to the Elementum checkout)
"""
import json
import os
import shutil
import sys


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    burst = sys.argv[1] if len(sys.argv) > 1 else os.environ.get("ELEMENTUM_BURST_PATH") or os.path.join(here, "..", "..", "..", "script.elementum.burst")
    providers = os.path.join(burst, "burst", "providers")
    shutil.copyfile(os.path.join(providers, "providers.json"), os.path.join(here, "providers.json"))
    try:
        from PIL import Image
    except ImportError:
        Image = None
        print("Pillow is not installed, icons are copied without scaling them down")
    target = os.path.join(here, "icons")
    os.makedirs(target, exist_ok=True)
    count = 0
    with open(os.path.join(here, "providers.json"), encoding="utf-8") as f:
        ids = set(json.load(f))
    for old in os.listdir(target):
        os.remove(os.path.join(target, old))
    for name in sorted(os.listdir(os.path.join(providers, "icons"))):
        if name.endswith(".png") and name[:-4] in ids:
            if Image is None:
                shutil.copyfile(os.path.join(providers, "icons", name), os.path.join(target, name))
            else:
                image = Image.open(os.path.join(providers, "icons", name)).convert("RGBA")
                image.thumbnail((96, 96), Image.LANCZOS)
                image.save(os.path.join(target, name), optimize=True)
            count += 1
    print("providers.json and %d icons copied from %s" % (count, os.path.normpath(burst)))


if __name__ == "__main__":
    main()
