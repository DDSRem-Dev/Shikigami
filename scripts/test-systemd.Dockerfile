FROM golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af
RUN sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Retries=3 update -qq \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd systemd-sysv dbus libpam-systemd tmux iproute2 openssl python3 \
    && useradd -m -u 1000 shikitest \
    && rm -rf /var/lib/apt/lists/*
ENV container=docker
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
