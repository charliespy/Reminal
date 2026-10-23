# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar

# A headless X11 desktop for measuring how responsive a mirrored window is:
# Xvfb + openbox, the tools the Linux window backend shells out to, ffmpeg
# for the H.264 path, and a probe window (probe.py) that changes colour on
# every key or click. run.sh copies a reminal binary in.
FROM debian:bookworm-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        xvfb x11-utils openbox wmctrl xdotool imagemagick ffmpeg python3 python3-tk procps ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN for f in /etc/ImageMagick-6/policy.xml /etc/ImageMagick-7/policy.xml; do \
        [ -f "$f" ] && sed -i 's/rights="none"/rights="read|write"/g' "$f" || true; \
    done
ENV DISPLAY=:99 HOME=/root/h SHELL=/bin/bash
COPY reminal /usr/local/bin/reminal
COPY entrypoint.sh probe.py procstat.py summarize.py /usr/local/bin/
RUN chmod +x /usr/local/bin/entrypoint.sh && mkdir -p /root/h
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
