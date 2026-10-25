#!/bin/sh
set -eu
mkdir -p "${DATA_DIR:-/data}/images"
chown -R app:app "${DATA_DIR:-/data}"
exec su-exec app:app /usr/local/bin/classroom-notify
