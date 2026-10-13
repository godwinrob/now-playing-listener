# Now Playing listener

A small service for a Raspberry Pi that sits next to the turntable, records what is playing from an
audio input (the line-in of a USB sound card, or a microphone), and reports it to a
[Now Playing](../now-playing) server. The server asks AudD what the song is, finds the record in your
Discogs collection, and sets it as now playing.

```
turntable -> (phono preamp) -> USB sound card line-in -> this listener -> Now Playing server
```

It needs nothing from the server but an address and an API key.

## Before you start

- **A phono preamp.** A turntable's own output is phono level and needs an RIAA preamp before a
  line-in. Many turntables have one built in (a switch on the back), or use an amp or receiver with a
  phono stage and take its line-out.
- **A sound card with a real line-in.** Many cheap USB cards only have a mic jack, which clips on
  line-level audio. Look for a stereo line input.
- **An API key.** On the server's Host page, under **API keys**, make one named for this Pi. It
  starts `npk_` and is shown once.

## Releases

Every tagged release has files for both kinds of Raspberry Pi OS:

| File | For |
| --- | --- |
| `now-playing-listener_<version>_linux_arm64.tar.gz` | 64-bit Raspberry Pi OS (Pi 3, 4, 5, Zero 2 W) |
| `now-playing-listener_<version>_linux_armv6.tar.gz` | 32-bit Raspberry Pi OS (any Pi, including the Zero and 1) |
| `checksums.txt` | SHA-256 of every file above and the bare binaries |
| `now-playing-listener-linux-arm64`, `-armv6` | The bare binary, at a name that never changes |

Not sure which? `uname -m` says `aarch64` for 64-bit, `armv6l` or `armv7l` for 32-bit.

Each tarball holds the listener, `setup.sh`, the systemd unit and the settings example.

## Install on the Pi

Get the tarball for your Pi onto it. While this repository is private, download it with the GitHub
CLI on your computer (after `gh auth login`) and copy it over:

```sh
gh release download -R godwinrob/now-playing-listener -p '*_linux_arm64.tar.gz' -D .
scp now-playing-listener_*_linux_arm64.tar.gz pi@<pi>:
```

(On the Pi itself, `sudo apt install gh && gh auth login` works the same way. Once the repository is
public, plain `curl -fsSLO https://github.com/godwinrob/now-playing-listener/releases/download/<version>/<file>`
does.)

Then, on the Pi, with the sound card plugged in:

```sh
tar xzf now-playing-listener_*_linux_arm64.tar.gz
cd now-playing-listener_*_linux_arm64
arecord -l            # "card 1: Device [USB Audio Device], device 0" is plughw:1,0
sudo ./setup.sh --server http://tower.local:8080 --api-key npk_... --audio-device plughw:1,0
```

`setup.sh` installs the binary and a systemd service, so the listener starts on boot and restarts if
it stops. It checks first that the build runs on this Pi. It is safe to run again, and it tells you
what it changed.

**Upgrading** is the same: unpack the newer tarball and run `sudo ./setup.sh` again. The settings are
kept and the service restarts. To go back, unpack the older one. `now-playing-listener version` says
what is installed.

To check a download: `sha256sum -c checksums.txt --ignore-missing` next to the files.

## Check it

```sh
sudo systemctl stop now-playing-listener
sudo sh -c 'set -a; . /etc/now-playing/listener.env; now-playing-listener test-mic'
sudo systemctl start now-playing-listener
journalctl -u now-playing-listener -f
```

`test-mic` records a few seconds and draws the level. Play a record: you want it well above -45 dBFS
and nowhere near 0. Adjust the card's capture volume with `alsamixer -c <card>` (F4 for capture).
The Host page shows each listener as online or offline, with its level.

## Settings

In `/etc/now-playing/listener.env`; see `deploy/listener.env.example`.

| Setting | Default | |
| --- | --- | --- |
| `NPL_SERVER_URL` | | The server's address, e.g. `http://tower.local:8080` |
| `NPL_API_KEY` | | The API key (`NPL_DEVICE_TOKEN` is the old name and still works) |
| `NPL_ALSA_DEVICE` | `default` | The capture device; a USB card is usually `plughw:1,0` |
| `NPL_SAMPLE_RATE` | `44100` | 8000 to 48000 |
| `NPL_CLIP_SECONDS` | `10` | 5 to 30. The server accepts clips up to 2 MB: seconds x rate x 2 bytes |
| `NPL_SEARCH_INTERVAL` | `30s` | Between clips while no record is recognized |
| `NPL_TRACK_INTERVAL` | `3m` | Between clips once a record is recognized |
| `NPL_HEARTBEAT_INTERVAL` | `30s` | How often the server hears the listener is alive |
| `NPL_SILENCE_DBFS` | `-45` | Clips quieter than this are never sent |
| `NPL_LOG_LEVEL` | `info` | debug, info, warn or error |

## What it sends

Three calls, all with `Authorization: Bearer <API key>`:

| Call | Does |
| --- | --- |
| `POST /api/listen` | Uploads a WAV clip (multipart `audio`, `recordedAt`); the answer says what it was and what to do next |
| `POST /api/listener/heartbeat` | `{"level": dBFS, "mode": "searching" or "tracking"}` every 30 seconds |
| `DELETE /api/listener/heartbeat` | The listener is stopping, so the server shows it offline at once |

## Develop and release

```sh
make test                       # gofmt, go vet, go test
make package VERSION=v1.2.3     # the release files, in dist/, exactly as a tag builds them
make pi                         # just the linux/arm64 binary
```

To release, once `main` is what you want:

```sh
git tag v1.2.3
git push origin v1.2.3
```

The **Release** workflow tests, packages and publishes the GitHub release. Run it by hand from the
Actions tab to get the same files as a downloadable artifact without publishing.
