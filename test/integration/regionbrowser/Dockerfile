ARG SHIKI_TEST_BASE=golang@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0
FROM golang@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0 AS toolchain
FROM ${SHIKI_TEST_BASE}
COPY --from=toolchain /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH
ARG SHIKI_TEST_UID=1000

# Disposable Linux acceptance environment. Never mount host /etc, /var/lib,
# the host system bus, or a user browser profile into this container.
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::Retries=3 install -y --no-install-recommends \
       systemd systemd-sysv dbus dbus-user-session dbus-x11 libpam-systemd \
       sudo locales tzdata tmux iproute2 openssl ca-certificates curl gnupg build-essential \
       tigervnc-standalone-server tigervnc-common xauth x11-utils x11-xkb-utils xdotool openbox \
       fcitx5 fcitx5-chinese-addons fcitx5-mozc fcitx5-frontend-gtk3 \
       fonts-noto-cjk fonts-noto-color-emoji fonts-liberation \
       gnome-keyring libsecret-tools libsecret-1-0 libglib2.0-bin python3 \
    && useradd --create-home --uid "${SHIKI_TEST_UID}" --shell /bin/bash shikitest \
    && install -d -m 0700 -o shikitest -g shikitest /home/shikitest/.config \
    && localedef -i en_US -f UTF-8 en_US.UTF-8 \
    && localedef -i ja_JP -f UTF-8 ja_JP.UTF-8 \
    && rm -rf /var/lib/apt/lists/*

# Official Chrome package via its signed repository; do not substitute Chromium
# and then label that result as Google Chrome acceptance.
RUN curl --fail --silent --show-error --proto '=https' --tlsv1.2 \
       https://dl.google.com/linux/linux_signing_key.pub -o /tmp/google-key.pub \
    && gpg --batch --dearmor --output /usr/share/keyrings/shiki-test-google.gpg /tmp/google-key.pub \
    && printf '%s\n' 'deb [signed-by=/usr/share/keyrings/shiki-test-google.gpg] https://dl.google.com/linux/chrome/deb/ stable main' > /etc/apt/sources.list.d/shiki-test-chrome.list \
    && apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends google-chrome-stable \
    && rm -f /tmp/google-key.pub \
    && rm -rf /var/lib/apt/lists/*

ENV container=docker
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
