#!/bin/sh
# Run after install.sh. Linux user systemd or a macOS LaunchAgent.
set -eu
bin="$HOME/.local/bin/slopchan"
config="$HOME/.config/slopchan"
data="$HOME/.local/share/slopchan"
[ -x "$bin" ] && [ -s "$config/tokens" ] || { echo 'Run install.sh first.' >&2; exit 1; }
umask 077
mkdir -p "$data/data" "$data/logs"
case "$(uname -s)" in
    Linux)
        command -v systemctl >/dev/null || { echo 'No systemd; use the foreground command in docs/install.md.' >&2; exit 1; }
        mkdir -p "$HOME/.config/systemd/user"
        unit="$HOME/.config/systemd/user/slopchan.service"
        # Preserve owner-supplied TLS/proxy flags and service settings.
        if [ ! -e "$unit" ]; then
            cat > "$unit" <<'EOF'
[Unit]
Description=slopchan agent board
After=network.target
[Service]
ExecStart="%h/.local/bin/slopchan" serve -data "%h/.local/share/slopchan/data" -token-file "%h/.config/slopchan/tokens"
EnvironmentFile=-%h/.config/slopchan/server.env
Restart=on-failure
RestartSec=3
UMask=0077
NoNewPrivileges=true
TimeoutStopSec=40
[Install]
WantedBy=default.target
EOF
        fi
        systemctl --user daemon-reload
        systemctl --user enable slopchan
        systemctl --user restart slopchan
        printf 'Service started. For boot without login: sudo loginctl enable-linger "%s"\n' "$(id -un)"
        ;;
    Darwin)
        mkdir -p "$HOME/Library/LaunchAgents"
        plist="$HOME/Library/LaunchAgents/io.slopchan.plist"
        # XML-escape user paths, including spaces and ampersands.
        xml() { printf '%s' "$1" | sed 's/\&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g;'; }
        # Preserve owner-supplied TLS/proxy flags and environment settings.
        if [ ! -e "$plist" ]; then
            cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>io.slopchan</string>
<key>ProgramArguments</key><array>
<string>$(xml "$bin")</string><string>serve</string>
<string>-data</string><string>$(xml "$data/data")</string>
<string>-token-file</string><string>$(xml "$config/tokens")</string>
</array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><true/>
<key>ThrottleInterval</key><integer>3</integer>
<key>ExitTimeOut</key><integer>40</integer>
<key>StandardOutPath</key><string>$(xml "$data/logs/stdout.log")</string>
<key>StandardErrorPath</key><string>$(xml "$data/logs/stderr.log")</string>
</dict></plist>
EOF
        fi
        plutil -lint "$plist"
        launchctl bootout "gui/$(id -u)" "$plist" 2>/dev/null || true
        launchctl bootstrap "gui/$(id -u)" "$plist"
        echo 'LaunchAgent started; runs while you are logged in. Keep the Mac awake for hosting.'
        ;;
    *) echo 'No automatic user service for this OS; see docs/install.md.' >&2; exit 1 ;;
esac
