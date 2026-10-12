# Code signing policy

*Ukratko na hrvatskom:* Windows programi goCOP-a (`gocop.exe`, Postava i
instalacijski program) potpisivat će se besplatnim certifikatom SignPath
Foundationa. Ovdje piše što se potpisuje, tko odobrava potpis i što program
šalje u mrežu. Potpisivanje još nije uključeno; ova stranica je uvjet za
prijavu.

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

Independently of the operating system signature, every release also carries
`SHA256SUMS` and `SHA256SUMS.sig`, signed with the project's release key;
Postava checks that signature before it installs or updates the node (see
[INSTALACIJA.md](INSTALACIJA.md), chapter 8).

## Team and roles

| role | who |
|---|---|
| authors (commit without further review) | [Tomislav Kraljević](https://github.com/tkraljevic) |
| reviewers (review every pull request from others) | [Tomislav Kraljević](https://github.com/tkraljevic) |
| approvers (approve every signing request) | [Tomislav Kraljević](https://github.com/tkraljevic) |

The same person develops and maintains the project and owns the repository
[tkraljevic/goCOP](https://github.com/tkraljevic/goCOP). Every release is
approved manually before signing: the release draft is published only after
`SHA256SUMS` has been signed by hand with the release key, which never leaves
the maintainer's computer. Multi-factor authentication is required for
GitHub and SignPath.

## Privacy policy

This program will not transfer any information to other networked systems
unless specifically requested by the user or the person installing or
operating it.

goCOP is a flood defence system run by the organisation that installs it.
What it sends over the network is set up by that organisation's
administrator:

- **Other goCOP nodes of the same organisation.** Records are exchanged only
  with computers that an administrator has paired into the organisation's
  network, encrypted with the nodes' own keys (TLS), directly or through the
  organisation's own public node (HTTPS tunnel).
- **Public hydrological and meteorological sources** chosen by the
  administrator (for example Hrvatske vode, DHMZ and neighbouring national
  services): the program downloads published water levels, discharges and
  rainfall. It sends nothing but the request.
- **E-mail** through the organisation's own Exchange server, only when the
  administrator enables it (sign-in PIN, notifications, acts).
- **Map background**: the user's browser loads map tiles from OpenStreetMap
  or Wikimedia when a map is shown.
- **Updates**: Postava asks GitHub for the latest release when it starts and
  every six hours. It sends nothing but the request.

No data is ever sent to the authors of the program, and there is no
telemetry. The administrator can switch off exchange with other nodes
(`[sync] auto_sync = '0'`), downloads from public sources (node roles) and
e-mail. The update check cannot be switched off yet; a setting for it is
planned.

## Reporting

Security issues: see [SECURITY.md](../SECURITY.md).
