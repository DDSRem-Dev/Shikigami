FROM golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af
ARG DEBIAN_MIRROR=https://deb.debian.org/debian
RUN sed -i "s|http://deb.debian.org/debian|${DEBIAN_MIRROR}|g; s|http://deb.debian.org/debian-security|https://deb.debian.org/debian-security|g" /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Retries=3 update -qq \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq strace python3 \
    && useradd -m -u 1000 shikitest \
    && mkdir /cache && chown 1000:1000 /cache \
    && rm -rf /var/lib/apt/lists/*
