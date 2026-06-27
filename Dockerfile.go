FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    iputils-ping traceroute \
    && rm -rf /var/lib/apt/lists/*
COPY bin/pp-starlink /usr/local/bin/pp-starlink
RUN chmod +x /usr/local/bin/pp-starlink
