# Code signing policy

*Ukratko na hrvatskom:* Windows programi goCOP-a (`gocop.exe`, Postava i
instalacijski program) potpisivat će se besplatnim certifikatom SignPath
Foundationa. Ovdje piše što se potpisuje, tko odobrava potpis i što program
šalje u mrežu. Potpisivanje još nije uključeno: prijava je poslana
12. 10. 2026. i čeka odgovor, a ova stranica je uvjet za prijavu.

Free code signing provided by [SignPath.io](https://signpath.io), certificate
by [SignPath Foundation](https://signpath.org).

## What is signed

Only Windows executables built from this repository by GitHub Actions:

- `gocop-windows-amd64.exe`, the goCOP node (workflow `izdanje.yml`, on a
  `v*` tag);
- `gocop-postava.exe` and the installer `goCOP-postava-<version>.exe`
  (workflow `postava.yml`, on a `postava-v*` tag).

Product name and version are written into the executables at build time
(`rsrc_windows_amd64.syso`). Nothing built outside CI is signed, and no
third-party binaries are signed.

Independently of the operating system signature, every release of the goCOP
node (tag `v*`) also carries `SHA256SUMS` and `SHA256SUMS.sig`, signed with
the project's release key; Postava checks that signature before it installs
or updates the node (see [INSTALACIJA.md](INSTALACIJA.md), chapter 8).
Releases of Postava itself (tag `postava-v*`) carry their checksums in
`SHA256SUMS-postava`, without a release key signature.

## Team and roles

| role | who |
|---|---|
| authors (commit without further review) | [Tomislav Kraljević](https://github.com/tkraljevic) |
| reviewers (review every pull request from others) | [Tomislav Kraljević](https://github.com/tkraljevic) |
| approvers (approve every signing request) | [Tomislav Kraljević](https://github.com/tkraljevic) |

The same person develops and maintains the project and the repository
[tkraljevic/goCOP](https://github.com/tkraljevic/goCOP). Every release is
approved manually before signing: the release draft is published only after
`SHA256SUMS` has been signed by hand with the release key, which never leaves
the maintainer's computer. Multi-factor authentication is required for
GitHub and SignPath.

## Privacy policy

This program transfers information to other networked systems only as
listed below. A few public downloads are on by default; everything else
happens only when a user asks for it or an administrator sets it up.

goCOP is a flood defence system run by the organisation that installs it.
This is everything it sends over the network, and who turns it on. The same
list, in Croatian, is in [INSTALACIJA.md](INSTALACIJA.md), chapter 2.

On by default, without any setting:

- **Weather.** The node downloads public weather and warnings of DHMZ, the
  Croatian meteorological service (meteo.hr, vrijeme.hr, prognoza.hr,
  hidro.hr), for the dashboard, and weather from Open-Meteo for a new daily
  logbook sheet. The Open-Meteo request carries the coordinates of the
  logbook's place or of the defended area, and the date. These downloads
  cannot be switched off yet.
- **Map background.** The user's browser loads map tiles from the tile server
  set in `[karta] plocice`, by default OpenStreetMap
  (tile.openstreetmap.org). The tile server sees only the address of the
  goCOP site, without the page path. The node itself loads the same tiles
  when it draws a map into a PDF. An empty setting turns map backgrounds
  off, except on the map of territorial units, which still loads
  OpenStreetMap tiles.
- **Updates.** Postava asks GitHub for the list of releases when it starts
  and every six hours. It sends nothing but the request. This check cannot be
  switched off yet; a setting for it is planned.

Only when a user or an administrator asks for it:

- **Water gauges.** The gauge form loads the public list of stations from
  vodostaji.voda.hr (Hrvatske vode), and a manual download on a gauge page
  reads that gauge's source.
- **Place lookup.** When a global administrator looks up the coordinates of a
  defended area, the place name or address is sent to OpenStreetMap
  Nominatim.
- **Network check button.** When a global administrator presses *Provjeri
  vezu*, the node sends the same STUN requests as described under WEB sync
  below, also when WEB sync is off: it opens UDP port 4713 for the check only
  and closes it afterwards. `[povezivost] stun = []` turns this off too.

Only when the administrator sets it up:

- **Other goCOP nodes of the same organisation.** Records are exchanged only
  with computers that an administrator has paired into the organisation's
  network, encrypted with the nodes' own keys (TLS 1.3): directly in the
  local network, through the organisation's own public node (HTTPS tunnel),
  or over Tailscale when both computers have it. goCOP itself only connects to
  the Tailscale addresses of the organisation's own nodes.
- **Network check for WEB sync**, only with `[povezivost] web_sync_prijava =
  true` (off by default). When the node starts, it sends a STUN Binding
  request from UDP port 4713 to stun.l.google.com and stun.cloudflare.com, to
  learn under which public address they see it. These servers see the
  computer's public IP address and port, and nothing else.
  `[povezivost] stun = []` turns the requests off.
- **E-mail** through the organisation's own Exchange server (sign-in PIN,
  notifications, acts, company address book), only when the administrator
  sets up the server.
- **Water level, rainfall and forecast sources**, only on a node to which the
  administrator gives the role of downloading water levels or issuing the
  forecast: public services (Hrvatske vode, DHMZ, Open-Meteo, pljusak.com,
  ICPDR DanubeHIS and national services of neighbouring countries, for
  example Hungary, Serbia, Slovenia, Slovakia, Austria and Germany) and
  systems of Hrvatske vode that need the organisation's sign-in (HydroView,
  mLetva, letva.voda.hr). The program downloads published values and sends
  nothing but the request and, where needed, that sign-in.

No data is ever sent to the authors of the program, and there is no
telemetry. The administrator can switch off automatic exchange with other
nodes (`[sync] auto_sync = '0'`), exchange over Tailscale
(`[sync] tailscale = false`), WEB sync and its network check
(`[povezivost] web_sync_prijava = false`, the default), downloads from
sources (node roles) and e-mail. Weather downloads and the update check cannot be switched off yet.

## Reporting

Security issues: see [SECURITY.md](../SECURITY.md).
