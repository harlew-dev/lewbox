# A macOS-style XFCE desktop on a virtual display, for browsing and computer use agents, with
# Chromium, a terminal and files in its dock, streamed to the browser as H.264 at up to 60 frames a
# second by Selkies, over a WebSocket on port 6080. Node, Claude Code and Codex are installed for
# agents that use it, with xdotool, scrot and ffmpeg to drive it, take screenshots and record it.
# Code is written over SSH, from an editor's remote mode on your own machine.
#
# The desktop starts with the sandbox, and again after every start, as does sshd. Selkies listens
# on 127.0.0.1 only, without a password or TLS, so it's reached only as a private port, through
# Lewbox's proxy, which gives both: give the template the ports {"port": 6080, "protocol": "http",
# "access": "private"}, then open a link to it, and {"port": 22, "protocol": "tcp"} for SSH, which
# takes keys only, such as the one on your profile. Give the template a default size of 4 vCPUs
# and 8192 MB, for 60 frames a second at 1080p, and DESKTOP_SIZE in its env, or a sandbox's, for
# another starting size; the browser resizes it to fit after that. The display is :0, so commands
# run in the sandbox reach it too. desktop.json beside it is this template with those ports, size
# and env, for the dashboard's Import JSON or the Admin API.
#
# The look is WhiteSur's GTK, window, icon and cursor themes, Inter and JetBrains Mono, a menu bar
# at the top and a dock at the bottom, without Apple's logos or wallpapers. Windows aren't
# composited, as without a GPU every shadow costs frames.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive LANG=C.UTF-8 DISPLAY=:0 DESKTOP_SIZE=1280x800 \
    XDG_RUNTIME_DIR=/tmp/runtime-root
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl gnupg git xz-utils build-essential python3 \
        ripgrep fd-find jq unzip less nano openssh-client openssh-server \
        xvfb xfce4-session xfwm4 xfce4-panel xfdesktop4 xfce4-settings xfce4-helpers xfce4-terminal thunar \
        dbus-x11 x11-utils x11-xserver-utils libgl1-mesa-dri librsvg2-common gtk-update-icon-cache \
        adwaita-icon-theme pulseaudio pulseaudio-utils \
        xdotool wmctrl xclip scrot ffmpeg xdg-utils \
        fonts-inter fonts-jetbrains-mono fonts-dejavu fonts-noto-core fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/*
# sshd takes keys only. The package's host keys are removed, so each sandbox makes its own as it
# first starts, rather than every sandbox sharing the template's.
RUN rm -f /etc/ssh/ssh_host_* \
    && mkdir -p /root/.ssh \
    && chmod 700 /root/.ssh \
    && printf '%s\n' 'PasswordAuthentication no' 'KbdInteractiveAuthentication no' \
        'PermitRootLogin prohibit-password' > /etc/ssh/sshd_config.d/lewbox.conf
RUN set -eu; \
    selkies=2.0.0; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) checksum=bbaa4d71012b9374a753b7dfddc1da07e31f34b04277fe4fb3d045f18fd88391 ;; \
        arm64) checksum=3900f3ba805898c495829629092553cc1cf4d5a864ffc4056f57d21646ad45e4 ;; \
        *) echo "Selkies has no build for this architecture" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo /tmp/selkies.deb "https://github.com/selkies-project/selkies/releases/download/${selkies}/selkies-${selkies}-ubuntu24.04-${arch}.deb"; \
    echo "${checksum}  /tmp/selkies.deb" | sha256sum -c -; \
    apt-get update; \
    apt-get install -y --no-install-recommends /tmp/selkies.deb; \
    rm -rf /tmp/selkies.deb /var/lib/apt/lists/*; \
    command -v selkies selkies-resize
# Chromium from the xtradeb PPA, as Ubuntu's own is a snap, pinned so nothing else comes from it,
# and the GitHub CLI from GitHub's.
RUN set -eu; \
    install -d -m 0755 /etc/apt/keyrings; \
    export GNUPGHOME="$(mktemp -d)"; \
    xtradeb=5301FA4FD93244FBC6F6149982BB6851C64F6880; \
    curl -fsSL "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x${xtradeb}" | gpg --dearmor -o /etc/apt/keyrings/xtradeb-apps.gpg; \
    gpg --show-keys --with-colons /etc/apt/keyrings/xtradeb-apps.gpg | grep -q "^fpr:::::::::${xtradeb}:"; \
    echo "deb [signed-by=/etc/apt/keyrings/xtradeb-apps.gpg] https://ppa.launchpadcontent.net/xtradeb/apps/ubuntu noble main" > /etc/apt/sources.list.d/xtradeb-apps.list; \
    printf '%s\n' 'Package: *' 'Pin: release o=LP-PPA-xtradeb-apps' 'Pin-Priority: 100' '' \
        'Package: chromium*' 'Pin: release o=LP-PPA-xtradeb-apps' 'Pin-Priority: 700' > /etc/apt/preferences.d/xtradeb-apps; \
    curl -fsSLo /etc/apt/keyrings/githubcli-archive-keyring.gpg https://cli.github.com/packages/githubcli-archive-keyring.gpg; \
    chmod go+r /etc/apt/keyrings/*.gpg; \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list; \
    rm -rf "$GNUPGHOME"; \
    apt-get update; \
    apt-get install -y --no-install-recommends chromium gh; \
    rm -rf /var/lib/apt/lists/*; \
    chromium --version; \
    gh --version
# Chromium refuses to run as root without --no-sandbox, and reads its flags from /etc/chromium.d.
RUN printf '%s\n' 'export CHROMIUM_FLAGS="$CHROMIUM_FLAGS --no-sandbox --test-type --no-first-run --password-store=basic"' \
        > /etc/chromium.d/lewbox
RUN set -eu; \
    node=24.22.0; \
    case "$(dpkg --print-architecture)" in \
        amd64) arch=x64; checksum=cc2f601cee82d9a5a233302fe307abc600727035fd7d2c3a3571755989e95008 ;; \
        arm64) arch=arm64; checksum=fef00bb4d36e1c50104a9775a10359beee98f162b4104dd12472c75065c6a143 ;; \
        *) echo "Node has no build for this architecture" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo /tmp/node.tar.xz "https://nodejs.org/dist/v${node}/node-v${node}-linux-${arch}.tar.xz"; \
    echo "${checksum}  /tmp/node.tar.xz" | sha256sum -c -; \
    tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1 --no-same-owner; \
    rm /tmp/node.tar.xz; \
    node --version
RUN npm install --global @anthropic-ai/claude-code@2.1.302 @openai/codex@0.163.0 \
    && npm cache clean --force \
    && claude --version \
    && codex --version
# WhiteSur's prebuilt Light theme (GTK, xfwm4), its icons and its cursors, at pinned releases.
RUN set -eu; \
    whitesur=2026-09-10; \
    cursors=e190baf618ed95ee217d2fd45589bd309b37672b; \
    cd /tmp; \
    curl -fsSLo gtk.tar.gz "https://github.com/vinceliuice/WhiteSur-gtk-theme/archive/refs/tags/${whitesur}.tar.gz"; \
    curl -fsSLo icons.tar.gz "https://github.com/vinceliuice/WhiteSur-icon-theme/archive/refs/tags/${whitesur}.tar.gz"; \
    curl -fsSLo cursors.tar.gz "https://github.com/vinceliuice/WhiteSur-cursors/archive/${cursors}.tar.gz"; \
    printf '%s\n' \
        '65516c8f45da45c602471e31b4de0b6e9aed6eb3fc3820df16161d077245a1a2  gtk.tar.gz' \
        '406c9cd59705583f1754b0eaca96cc48bafda042b88ef143f16d8ae1820ecd95  icons.tar.gz' \
        '35c817c2c26bc4d2c06801cfe6c924a068ad771d3846c5203b04d1c893dd5664  cursors.tar.gz' | sha256sum -c -; \
    mkdir gtk icons cursors; \
    tar -xzf gtk.tar.gz -C gtk --strip-components=1; \
    tar -xzf icons.tar.gz -C icons --strip-components=1; \
    tar -xzf cursors.tar.gz -C cursors --strip-components=1; \
    tar -xJf gtk/release/WhiteSur-Light.tar.xz -C /usr/share/themes; \
    ./icons/install.sh > /dev/null; \
    cp -r cursors/dist /usr/share/icons/WhiteSur-cursors; \
    rm -rf /tmp/gtk* /tmp/icons* /tmp/cursors*; \
    test -f /usr/share/themes/WhiteSur-Light/xfwm4/themerc; \
    test -f /usr/share/icons/WhiteSur-light/index.theme
# A soft gradient in place of XFCE's default wallpaper, which the desktop shows at any size.
RUN dpkg-divert --local --rename --add /usr/share/backgrounds/xfce/xfce-shapes.svg \
    && printf '%s\n' \
        '<svg xmlns="http://www.w3.org/2000/svg" width="3840" height="2160" viewBox="0 0 3840 2160">' \
        '  <defs>' \
        '    <linearGradient id="sky" x1="0" y1="0" x2="1" y2="1">' \
        '      <stop offset="0" stop-color="#2f5fd8"/>' \
        '      <stop offset="0.55" stop-color="#7a5cd6"/>' \
        '      <stop offset="1" stop-color="#eea3c3"/>' \
        '    </linearGradient>' \
        '    <radialGradient id="glow" cx="0.78" cy="0.2" r="0.6">' \
        '      <stop offset="0" stop-color="#ffffff" stop-opacity="0.35"/>' \
        '      <stop offset="1" stop-color="#ffffff" stop-opacity="0"/>' \
        '    </radialGradient>' \
        '  </defs>' \
        '  <rect width="3840" height="2160" fill="url(#sky)"/>' \
        '  <rect width="3840" height="2160" fill="url(#glow)"/>' \
        '</svg>' \
        > /usr/share/backgrounds/xfce/xfce-shapes.svg
# The desktop's settings: themes and fonts, macOS's window buttons on the left, a menu bar with the
# apps, the tray and the clock, and a dock with Files, Chromium and the terminal, then the open
# windows. Chromium is the default browser.
RUN xfconf=/root/.config/xfce4/xfconf/xfce-perchannel-xml \
    && mkdir -p "$xfconf" /root/workspace \
    && printf '%s\n' \
        '<?xml version="1.0" encoding="UTF-8"?>' \
        '<channel name="xsettings" version="1.0">' \
        '  <property name="Net" type="empty">' \
        '    <property name="ThemeName" type="string" value="WhiteSur-Light"/>' \
        '    <property name="IconThemeName" type="string" value="WhiteSur-light"/>' \
        '  </property>' \
        '  <property name="Gtk" type="empty">' \
        '    <property name="FontName" type="string" value="Inter 10"/>' \
        '    <property name="MonospaceFontName" type="string" value="JetBrains Mono 10"/>' \
        '    <property name="CursorThemeName" type="string" value="WhiteSur-cursors"/>' \
        '    <property name="CursorThemeSize" type="int" value="24"/>' \
        '    <property name="DecorationLayout" type="string" value="close,minimize,maximize:"/>' \
        '  </property>' \
        '  <property name="Xft" type="empty">' \
        '    <property name="Antialias" type="int" value="1"/>' \
        '    <property name="Hinting" type="int" value="1"/>' \
        '    <property name="HintStyle" type="string" value="hintslight"/>' \
        '    <property name="RGBA" type="string" value="none"/>' \
        '  </property>' \
        '</channel>' \
        > "$xfconf/xsettings.xml" \
    && printf '%s\n' \
        '<?xml version="1.0" encoding="UTF-8"?>' \
        '<channel name="xfwm4" version="1.0">' \
        '  <property name="general" type="empty">' \
        '    <property name="theme" type="string" value="WhiteSur-Light"/>' \
        '    <property name="title_font" type="string" value="Inter Semi-Bold 10"/>' \
        '    <property name="title_alignment" type="string" value="center"/>' \
        '    <property name="button_layout" type="string" value="CHM|"/>' \
        '    <property name="use_compositing" type="bool" value="false"/>' \
        '    <property name="workspace_count" type="int" value="1"/>' \
        '  </property>' \
        '</channel>' \
        > "$xfconf/xfwm4.xml" \
    && printf '%s\n' \
        '<?xml version="1.0" encoding="UTF-8"?>' \
        '<channel name="xfce4-desktop" version="1.0">' \
        '  <property name="desktop-icons" type="empty">' \
        '    <property name="style" type="int" value="0"/>' \
        '  </property>' \
        '</channel>' \
        > "$xfconf/xfce4-desktop.xml" \
    && printf '%s\n' \
        '<?xml version="1.0" encoding="UTF-8"?>' \
        '<channel name="xfce4-terminal" version="1.0">' \
        '  <property name="font-name" type="string" value="JetBrains Mono 11"/>' \
        '  <property name="misc-menubar-default" type="bool" value="false"/>' \
        '</channel>' \
        > "$xfconf/xfce4-terminal.xml" \
    && printf '%s\n' \
        '<?xml version="1.0" encoding="UTF-8"?>' \
        '<channel name="xfce4-panel" version="1.0">' \
        '  <property name="configver" type="int" value="2"/>' \
        '  <property name="panels" type="array">' \
        '    <value type="int" value="1"/>' \
        '    <value type="int" value="2"/>' \
        '    <property name="dark-mode" type="bool" value="false"/>' \
        '    <property name="panel-1" type="empty">' \
        '      <property name="position" type="string" value="p=6;x=0;y=0"/>' \
        '      <property name="length" type="uint" value="100"/>' \
        '      <property name="position-locked" type="bool" value="true"/>' \
        '      <property name="icon-size" type="uint" value="16"/>' \
        '      <property name="size" type="uint" value="28"/>' \
        '      <property name="plugin-ids" type="array">' \
        '        <value type="int" value="1"/>' \
        '        <value type="int" value="2"/>' \
        '        <value type="int" value="3"/>' \
        '        <value type="int" value="4"/>' \
        '      </property>' \
        '    </property>' \
        '    <property name="panel-2" type="empty">' \
        '      <property name="position" type="string" value="p=10;x=0;y=0"/>' \
        '      <property name="length" type="uint" value="1"/>' \
        '      <property name="position-locked" type="bool" value="true"/>' \
        '      <property name="icon-size" type="uint" value="40"/>' \
        '      <property name="size" type="uint" value="56"/>' \
        '      <property name="plugin-ids" type="array">' \
        '        <value type="int" value="5"/>' \
        '        <value type="int" value="6"/>' \
        '        <value type="int" value="8"/>' \
        '        <value type="int" value="9"/>' \
        '        <value type="int" value="10"/>' \
        '      </property>' \
        '    </property>' \
        '  </property>' \
        '  <property name="plugins" type="empty">' \
        '    <property name="plugin-1" type="string" value="applicationsmenu">' \
        '      <property name="button-icon" type="string" value="view-app-grid"/>' \
        '      <property name="show-button-title" type="bool" value="false"/>' \
        '    </property>' \
        '    <property name="plugin-2" type="string" value="separator">' \
        '      <property name="expand" type="bool" value="true"/>' \
        '      <property name="style" type="uint" value="0"/>' \
        '    </property>' \
        '    <property name="plugin-3" type="string" value="systray">' \
        '      <property name="square-icons" type="bool" value="true"/>' \
        '    </property>' \
        '    <property name="plugin-4" type="string" value="clock">' \
        '      <property name="digital-layout" type="uint" value="3"/>' \
        '      <property name="digital-time-format" type="string" value="%a %-d %b  %H:%M"/>' \
        '    </property>' \
        '    <property name="plugin-5" type="string" value="launcher">' \
        '      <property name="items" type="array">' \
        '        <value type="string" value="thunar.desktop"/>' \
        '      </property>' \
        '    </property>' \
        '    <property name="plugin-6" type="string" value="launcher">' \
        '      <property name="items" type="array">' \
        '        <value type="string" value="chromium.desktop"/>' \
        '      </property>' \
        '    </property>' \
        '    <property name="plugin-8" type="string" value="launcher">' \
        '      <property name="items" type="array">' \
        '        <value type="string" value="xfce4-terminal.desktop"/>' \
        '      </property>' \
        '    </property>' \
        '    <property name="plugin-9" type="string" value="separator"/>' \
        '    <property name="plugin-10" type="string" value="tasklist">' \
        '      <property name="show-labels" type="bool" value="false"/>' \
        '      <property name="flat-buttons" type="bool" value="true"/>' \
        '      <property name="show-handle" type="bool" value="false"/>' \
        '      <property name="grouping" type="uint" value="1"/>' \
        '    </property>' \
        '  </property>' \
        '</channel>' \
        > "$xfconf/xfce4-panel.xml" \
    && printf '%s\n' 'WebBrowser=chromium' 'TerminalEmulator=xfce4-terminal' > /root/.config/xfce4/helpers.rc \
    && printf '%s\n' '[Default Applications]' 'x-scheme-handler/http=chromium.desktop' \
        'x-scheme-handler/https=chromium.desktop' 'text/html=chromium.desktop' > /root/.config/mimeapps.list
# Xvfb runs the display, sized to DESKTOP_SIZE before the desktop starts, with PulseAudio for the
# sound Selkies streams. Selkies streams it, as H.264 by default, on 127.0.0.1 only, without TLS,
# which Lewbox's proxy and the node's HTTPS give, or a password, which the private port's link
# replaces. The browser's CSS pixels set the desktop's size, so a HiDPI screen doesn't quadruple
# what's encoded. Ctrl+Shift with F, M, X and G reach the desktop's apps rather than Selkies' own
# menu. Every viewer shares the one desktop, and Selkies is started again if it stops; the desktop
# stays up as long as Xvfb does. A start after a stop finds the last run's sockets, locks and
# runtime files in the sandbox's snapshot, so they're cleared first, as are the logs. sshd makes the
# sandbox's host keys as it first starts, and keeps them after that.
#
# Selkies' Computer-Use server listens on 127.0.0.1:6081 for agents in the sandbox: POST
# /computer-use with {"action": "screenshot"}, "left_click", "type", "key" and the rest of
# Anthropic's computer use actions. It has no authentication, so it isn't exposed as a port.
RUN printf '%s\n' \
        '#!/bin/sh' \
        'set -eu' \
        'display="${DISPLAY#:}"' \
        'rm -rf "$XDG_RUNTIME_DIR" /tmp/.X11-unix "/tmp/.X${display}-lock"' \
        'mkdir -p "$XDG_RUNTIME_DIR"' \
        'chmod 700 "$XDG_RUNTIME_DIR"' \
        'mkdir -p /run/sshd' \
        'ssh-keygen -A > /dev/null' \
        '/usr/sbin/sshd -E /tmp/sshd.log || true' \
        'Xvfb "$DISPLAY" -screen 0 8192x4096x24 -s 0 -dpms -ac -nolisten tcp -noreset \' \
        '    +extension GLX +extension RANDR +extension RENDER +extension MIT-SHM +extension XFIXES +extension XTEST \' \
        '    > /tmp/xvfb.log 2>&1 &' \
        'server=$!' \
        'for attempt in $(seq 1 50); do xdpyinfo > /dev/null 2>&1 && break; sleep 0.2; done' \
        'selkies-resize "$DESKTOP_SIZE" > /dev/null 2>&1 || true' \
        'xset s off -dpms > /dev/null 2>&1 || true' \
        'audio=--audio-enabled=true' \
        'pulseaudio --daemonize=yes --exit-idle-time=-1 --log-target=file:/tmp/pulseaudio.log || audio=--audio-enabled=false' \
        'dbus-launch --exit-with-session startxfce4 > /tmp/xfce.log 2>&1 &' \
        ': > /tmp/selkies.log' \
        'while true; do' \
        '    selkies --addr=127.0.0.1 --port=6080 --enable-basic-auth=false --enable-https=false "$audio" \' \
        '        --use-css-scaling=true --enable-sharing=false --printing-enabled=false --gamepad-enabled=false \' \
        '        --keyboard-shortcuts="false|locked" --computer-use-bind=127.0.0.1:6081 \' \
        '        --ui-title=Desktop --ui-show-logo=false --ui-sidebar-show-sharing=false --ui-sidebar-show-gamepads=false \' \
        '        --ui-sidebar-show-shortcuts=false >> /tmp/selkies.log 2>&1 || true' \
        '    sleep 1' \
        'done &' \
        'wait "$server"' \
        > /usr/local/bin/start-desktop \
    && chmod 755 /usr/local/bin/start-desktop
EXPOSE 6080
WORKDIR /root/workspace
CMD ["/usr/local/bin/start-desktop"]
