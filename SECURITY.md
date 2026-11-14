# Security policy

## Threat model

sshud is built so that a compromise of any one part gives away as little as
possible.

- **The sshud server holds one restricted key.** On first start it creates a
  dedicated ed25519 key in its data directory (mode 600). The private key is
  never stored in the database, returned by the API or written to logs.
- **Monitored machines only have one `authorized_keys` line**, of the form
  `restrict,command="…probe…"[,from="…"] ssh-ed25519 … sshud`. `command=`
  makes sshd run the read-only probe whatever the client asks for, and
  `restrict` turns off forwarding, PTYs and `~/.ssh/rc`. Someone who steals
  the key can read the probe output (CPU, memory, disks, processes,
  containers) and nothing else: no shell, no files, no tunnels.
- **Bootstrap credentials are used once.** When you add a machine with a
  password or private key, sshud uses it for that single request to install
  the line, then drops it. It is not written to the database, disk, logs or
  error messages.
- **Host keys are pinned** after the first connection (trust on first use,
  like `StrictHostKeyChecking=accept-new`). A changed key stops the
  connection before any credential is sent.
- **The web UI** is protected by one admin password (bcrypt) and signed,
  HttpOnly, SameSite=Strict session cookies, and only answers to the host
  names it is configured for, to block DNS rebinding.

Known limits, documented in the README: the very first connection to a
machine is not verified, so a password typed for it can leak to an attacker
in the middle (a private key cannot); and until a password is set, the first
visitor sets it, so set `SSHUD_PASSWORD` or keep sshud off untrusted
networks.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's
[Private Vulnerability Reporting](https://github.com/vinci7/sshud/security/advisories/new)
("Report a vulnerability" on the Security tab). Do not open a public issue.

Include the version (`sshud --version`), what an attacker needs, and steps
to reproduce. sshud is a personal project maintained in spare time, so
please allow reasonable time for a fix before disclosing.

## Supported versions

Only the latest minor release gets security fixes. Upgrade to it before
reporting, if you can.
