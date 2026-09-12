# Veer

**Where to next?** A terminal proxy manager built with Go and Bubble Tea.

Veer runs Xray as an external core and provides a full-screen interface for local profiles, connections, traffic, logs, Geo assets and VLESS QR sharing. It runs on macOS, Linux and Windows.

## Quick start

Install Go 1.27+ and Xray, then build and run:

```sh
go build -o bin/veer .
./bin/veer
```

On Windows:

```powershell
go build -o bin/veer.exe .
.\bin\veer.exe
```

Use a terminal with at least **60 columns × 18 rows**; Windows Terminal is recommended on Windows.

1. Press **5**, then **e**, to set the Xray executable and optional Geo assets directory. Enter `xray` if the executable is in PATH, or provide its full path.
2. Press **Ctrl+S** to save. Press **v** to check the core version.
3. Press **a** to add an existing Xray JSON configuration.
4. In **Profiles**, focus a profile and press **Enter** to select it, then **c** to connect.
5. Press **3** to inspect logs, **s** to disconnect, and **q** to quit.

Profiles reference existing files. Veer does not install Xray automatically or provide a built-in JSON editor or subscription/share-link import.

## Pages and keyboard controls

| Page | Contents and actions |
| --- | --- |
| **1 Overview** | Connection status beside the selected profile, core version, traffic chart, mode, DNS and listeners. **i** opens process and path details. |
| **2 Profiles** | **/** filters names, engines and paths. **Enter** selects; **a** adds, **e** renames, **d** removes a profile entry. **y** shares the focused profile. |
| **3 Logs** | Colored severity levels, timestamps, sources and wrapped messages. **G** resumes following the latest output. |
| **4 Tools** | Geosite and GeoIP versions or file timestamps. **g** opens the Geo asset updater; **u** opens the Xray core updater. |
| **5 Settings** | **e** edits the core path, Geo directory and DNS preferences. **v** checks the core version. |

Outside forms and search:

| Keys | Action |
| --- | --- |
| **1–5**, **h/l**, **Tab / Shift+Tab** | Switch pages |
| **j/k**, **↑/↓** | Move through profiles or scroll |
| **gg / G**, **Home / End** | First/last row in Profiles and Logs |
| **Ctrl+d/u** | Half-page down/up |
| **Ctrl+f/b**, **Page Down / Page Up** | Full-page down/up |
| **c / s** | Connect the selected profile / stop |
| **?** | Contextual keyboard help |
| **q**, **Ctrl+C** | Quit; an active connection may prompt for confirmation |

In profile search, **Enter** keeps the filter, **Ctrl+U** clears the input, and **Esc** cancels editing. Outside search, **Esc** clears the applied filter. Profile actions use the focused visible row; connecting uses the selected profile.

### Forms and path completion

Use **Tab / Shift+Tab** to move between fields, **Ctrl+S** to submit, and **Esc** to cancel.

All filesystem path fields support asynchronous completion: profile JSON, Xray executable, Geo settings and Geo download destination. Absolute paths, relative paths and `~/` are supported.

- **↑/↓** selects a suggestion; **Tab** completes it.
- Completing a directory adds a separator so you can continue into it.
- **Enter** advances to the next field or submits the last field.
- With no matches, **Tab** advances normally. You can still enter new or unreadable paths manually.

Directory fields suggest directories; file fields also suggest regular files. Executable completion browses the filesystem; bare commands from PATH can still be entered manually.

### Traffic

Overview shows upload/download rates, cumulative outbound bytes and a chart with upload above the axis and download below it. New samples enter from the right, with a gap between bars; older samples move left and eventually disappear. Up to 120 one-second rate samples are retained, resetting on reconnect.

Upload and download scale independently. Each rate includes the peak for its currently visible samples. Waiting, unavailable and stale statistics are distinct from zero traffic. Counters include direct Xray outbounds; they do not represent whole-machine traffic.

Traffic sampling requires Xray's `api statsquery` command and simplified API listener support (Xray 1.8.12+). Veer enables statistics in a private temporary runtime configuration, preserves the original profile, and removes the temporary file on shutdown. Compatible numeric loopback API listeners are reused; unsupported existing API configurations remain unchanged and statistics show as unavailable.

### QR sharing

Press **y** on Overview to share the active profile, or on Profiles to share the focused profile. Sharing supports VLESS outbounds. In the QR dialog, **h/l** switches between VLESS outbounds and **Esc/q** closes it.

The QR code appears in a compact modal. If it cannot fit, Veer asks for a larger terminal. Share URIs are not printed in notices or logs.

## TUN and DNS

TUN connections request elevation through sudo on macOS/Linux or UAC on Windows. Only the TUN helper is elevated. Exiting Veer stops the managed core.

On macOS/Linux, cached sudo authorization is reused when available. Otherwise, a masked password dialog opens in the TUI: **Enter** submits, **Esc** cancels, and **Ctrl+T** switches to the system terminal prompt for Touch ID, MFA or other authentication policies. Passwords travel through sudo stdin, are not saved or logged, and are cleared from the input on submission.

On Windows, approve or cancel in the system UAC dialog while Veer displays a waiting modal.

| DNS mode | Behavior |
| --- | --- |
| **auto** (default) | On macOS/Linux, save the active network's DNS settings, wait for the new TUN interface, then use `1.1.1.1` and `8.8.8.8`. Restore the previous settings on disconnect. On Windows, leave DNS to the Xray TUN configuration. |
| **custom** | Use the DNS IPs entered in Settings. |
| **off** | Leave system DNS unchanged. |

DNS changes occur only for TUN connections. The macOS network service setting is an optional override. Linux requires `ip` and `resolvectl`; DNS changes apply to the default network interface while preserving other links' DNS routing policies. Existing settings with custom DNS retain their servers; previously blank DNS settings use auto mode.

Native Windows UAC/window behavior and real TUN networking require testing on the target host; cross-compilation does not validate them.

## Geo assets

In Tools, press **g**, choose a destination, then select **GitHub**, **jsDelivr** or **Fastly** using **h/l** or **←/→**. Press **Enter** on the source selector to download `geoip.dat` and `geosite.dat`. Downloads occur only after an explicit action; a failed update preserves the existing files and metadata.

Tools reads the configured Geo directory, or the selected profile's directory when no override is set. After downloading, it displays the chosen destination.

- Existing files display their local modification time.
- Downloads record their source, update time and SHA-256 in `.veer-geo.json` alongside the assets.
- A release version is displayed only when the content matches the upstream release digest and the saved metadata still matches the installed file.
- If release lookup is unavailable or CDN content does not match, the display falls back to timestamps. Larger terminals also show the last update time.

Veer manages client-side assets; server-user creation and server-config mutation are not exposed in the TUI.

## Xray core updates

Veer checks [XTLS/Xray-core releases](https://github.com/XTLS/Xray-core/releases)
in the background at startup. An available core update appears in the header.
In **Tools**, press **u** to open the core updater, **r** to check again,
and **j/k** or **↑/↓** to select a version. Only compatible versions newer than
the installed core are listed, newest first. Press **Enter** to review and confirm
downloading and replacing the configured Xray executable with the selected version.
**Esc** cancels a check or download, or returns to Tools.

Choose **Stable** with **h/←**, or explicitly select **Preview** with **l/→**.
**Stable** is the default and
excludes prereleases; **Preview** includes stable and prerelease releases. The
choice is saved. Versions are compared numerically and never downgraded, so
switching to Stable waits for a stable release newer than the installed preview.

You can stay connected while downloading and installing. The updater resolves the Xray path from Settings
(including a command in PATH or a symlink), downloads the matching official ZIP,
verifies SHA-256, and extracts only `xray` or `xray.exe`. Existing Geo assets,
configuration and other bundled files are retained. Each successful update keeps
the previous executable in its `.veer-update-*` directory beside Xray. A failed
download or verification preserves the executable; failed Windows replacement
attempts to restore the old file and retains any recovery backup.

Press **b** in the core updater to confirm restoring the previous local backup.
Recovery works offline and remains available after restarting Veer. It preserves
the replaced version as another backup and takes effect on the next connection.
The release list still offers only newer versions; explicit backup recovery can
return to an older version. Retained backups are not automatically deleted.

The current connection continues using the old core. After updating, Veer offers
**Restart now** or **Later**. Choosing Later keeps the connection and its running
version unchanged. Press **r** on Overview whenever you want to restart the
connection. Overview shows the running core's version while connected, and the
installed version while disconnected. Veer itself does not need to restart.
The executable's directory must be writable. Updates do not request elevation.
For package-manager installations, use the package manager when its directory
is not writable. Missing or unrecognized local Xray versions must be resolved
in Settings before updating.

## Settings storage

Settings live in `veer/settings.json` under the OS user-config directory. Set `VEER_CONFIG_DIR` to override the directory. Profiles remain at their original paths.

## Development

Install Go 1.27+, [just](https://github.com/casey/just) and golangci-lint v2, then run:

```sh
just fmt
just lint
just test
just build
```

Tests use subprocesses and fakes at OS boundaries and must not change the host network. To test against an installed Xray core using a local HTTP proxy:

```sh
VEER_TEST_XRAY=/path/to/xray go test ./session -run TestNativeXrayLocalHTTPProxy -v
```

Keep core integration in `engine`, process lifecycle in `session`, OS integration in `network`/`privilege`, and terminal interaction in `tui`. Veer is TUI-only; argument dispatch is reserved for the private elevation helper.
