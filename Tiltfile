# -*- mode: Starlark -*-
#
# The listener's dev loop with one command:
#
#   tilt up                    the listener, rebuilt on every Go change, and the tests beside it
#   tilt up -- --with-server   the same, plus the Now Playing server and web app from ../now-playing
#
#   listener  the real listener binary. With no audio hardware it "records" a test tone, so on any
#             machine it uploads clips and sends heartbeats to the server like a Pi would
#   test      gofmt, go vet and go test, rerun when a Go file changes
#   package   (button) the release tarballs, built exactly as a tag builds them
#
# Settings: .env here wins; then the server's own (../now-playing/.env), so the key matches; then
# fast dev defaults. To use a real sound card, put NPL_ALSA_DEVICE=plughw:1,0 in .env.

WINDOWS = os.name == 'nt'
EXE = '.exe' if WINDOWS else ''
SERVER_REPO = '../now-playing'

def load_env(path):
    """Reads KEY=value lines (quotes and # comments allowed) into a dict."""
    env = {}
    for line in str(read_file(path, default = '')).splitlines():
        line = line.strip()
        if not line or line.startswith('#') or '=' not in line:
            continue
        key, value = line.split('=', 1)
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in ['"', "'"]:
            value = value[1:-1]
        env[key.strip()] = value
    return env

config.define_bool('with-server')
if config.parse().get('with-server', False):
    include(SERVER_REPO + '/Tiltfile')

# 'bash' on Windows can be the WSL launcher; Git's bash is the one the scripts expect.
GIT_BASH = 'C:\\Program Files\\Git\\bin\\bash.exe'
BASH = GIT_BASH if WINDOWS and os.path.exists(GIT_BASH) else 'bash'

server = load_env(SERVER_REPO + '/.env.example')
server.update(load_env(SERVER_REPO + '/.env'))
own = load_env('.env')

env = {
    'NPL_SERVER_URL': server.get('NP_PUBLIC_URL', 'http://localhost:8080'),
    'NPL_API_KEY': server.get('NP_DEVICE_TOKEN', 'fake-device-token'),
    'NPL_AUDIO_FILE': 'tone',
    # Quick enough to watch: a clip every few seconds instead of every 30.
    'NPL_CLIP_SECONDS': '5',
    'NPL_SEARCH_INTERVAL': '5s',
    'NPL_TRACK_INTERVAL': '15s',
    'NPL_HEARTBEAT_INTERVAL': '5s',
    'NPL_LOG_LEVEL': 'debug',
}
env.update(own)
if 'NPL_ALSA_DEVICE' in own and 'NPL_AUDIO_FILE' not in own:
    env.pop('NPL_AUDIO_FILE')  # a real device was asked for

watch_settings(ignore = ['**/.git', '**/dist'])

GO_SOURCES = ['cmd', 'internal', 'go.mod']

listener_bin = os.path.join(os.getcwd(), 'dist', 'dev', 'listener' + EXE)
local_resource(
    'listener',
    cmd = ['go', 'build', '-trimpath', '-o', listener_bin, './cmd/listener'],
    deps = GO_SOURCES,
    ignore = ['**/*_test.go'],
    serve_cmd = [listener_bin, 'run'],
    serve_env = env,
    labels = ['listener'],
)

local_resource(
    'test',
    cmd = 'go vet ./... && go test ./...',
    deps = GO_SOURCES,
    labels = ['check'],
)

local_resource(
    'package',
    cmd = [BASH, 'scripts/package.sh', 'dev'],
    auto_init = False,
    trigger_mode = TRIGGER_MODE_MANUAL,
    labels = ['release'],
)
