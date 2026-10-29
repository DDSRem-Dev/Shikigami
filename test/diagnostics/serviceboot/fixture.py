"""Real installer and CLI fixture; only prepare() may start/register services."""
import json
import os
from pathlib import Path
import shutil
import tarfile

from common import digest, inventory, process_identity, run, wait_for, write_json

class Fixture:
    version = "service-boot-local"

    def __init__(self):
        assert os.geteuid() == 0 and Path('/proc/1/comm').read_text().strip() == 'systemd'
        self.home = Path('/home/shikitest')
        self.evidence = Path('/evidence')
        self.evidence.mkdir(mode=0o700, exist_ok=True)
        self.config = self.home / '.shiki/config.yaml'
        self.daemon_config = self.home / 'daemon.yaml'
        self.daemon_state = self.home / 'daemon-state'
        self.token = self.home / 'device.token'
        self.prefix = self.home / '.local/share/shiki/install'
        self.cli_path = self.home / '.local/bin/shiki'
        self.env = {**os.environ, 'HOME': str(self.home), 'PATH': '/usr/local/go/bin:/usr/bin:/bin',
                    'TMPDIR': '/build/tmp', 'GOCACHE': '/var/cache/shiki-go', 'GOPATH': '/go',
                    'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOMAXPROCS': '2',
                    'GOFLAGS': '-p=1 -mod=readonly -buildvcs=false', 'PYTHONDONTWRITEBYTECODE': '1'}

    def user(self, args, **kwargs):
        return run(['/usr/sbin/runuser', '-u', 'shikitest', '--', 'env', 'HOME='+str(self.home),
                    'USER=shikitest', 'XDG_RUNTIME_DIR=/run/user/1000',
                    'PATH=/home/shikitest/.local/bin:/usr/local/go/bin:/usr/bin:/bin', *args], env=self.env, **kwargs)

    def write(self, path, data):
        path = Path(path)
        self.user(['mkdir', '-p', '-m', '700', path.parent])
        # Writes originate from the same ordinary user as the product; do not
        # expose a temporary root-owned file to the running filesystem watcher.
        self.user(['python3','-c',
                   'import os,sys; fd=os.open(sys.argv[1],os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600); f=os.fdopen(fd,"wb"); f.write(sys.stdin.buffer.read()); f.flush(); os.fsync(f.fileno()); f.close()',
                   path],input=data.decode('utf-8'))

    def cli(self, *args):
        result = self.user([self.cli_path, '--config', self.config, '--json', '--timeout', '10s', *args], timeout=20)
        return json.loads(result.stdout)

    def ctl(self, *args, agent=False):
        return self.user(['systemctl', '--user', *args]) if agent else run(['systemctl', *args])

    def unit(self, name, agent=False):
        fields = ('MainPID','InvocationID','ActiveState','SubState','UnitFileState','ExecMainStartTimestampMonotonic')
        result = self.ctl('show', name, *['--property='+f for f in fields], agent=agent)
        values = dict(line.split('=',1) for line in result.stdout.splitlines() if '=' in line)
        if values.get('ActiveState') != 'active' or int(values.get('MainPID','0')) <= 1:
            return None
        values['process'] = process_identity(int(values['MainPID']))
        assert values['process']['uids'] == [1000]*4
        assert values['InvocationID'] and values['UnitFileState'] == 'enabled'
        return values

    def probe(self, role):
        args = ['/opt/shiki/current/shiki-install', 'probe', '--role', 'daemon', '--user', 'shikitest', '--prefix', '/opt/shiki', '--non-interactive']
        if role == 'client':
            args = [self.prefix/'current/shiki-install','probe','--role','client','--non-interactive']
        response = (self.user if role == 'client' else run)(args, check=False)
        if response.returncode not in (0,3):
            raise RuntimeError(response.stderr)
        value = json.loads(response.stdout)
        assert value['external_peer_acceptance'] == 'pending'
        return value if response.returncode == 0 and value['state'] == 'local_application_ready' else None

    def build(self):
        for path in (Path('/build'),Path('/build/tmp'),Path('/var/cache/shiki-go')):
            path.mkdir(mode=0o700,parents=True,exist_ok=True)
            os.chown(path,1000,1000)
        before=inventory('/source')
        shutil.copytree('/source','/work')
        run(['chown','-R','1000:1000','/work'])
        assert inventory('/work') == before
        build=self.user(['env','SHIKI_VERSION='+self.version,'bash','scripts/build.sh'],cwd='/work',timeout=900)
        release=self.user(['go','run','scripts/release.go','--dir','bin','--output','/build/release','--version',self.version],cwd='/work',timeout=180)
        (self.evidence/'build.log').write_text(build.stdout+build.stderr+release.stdout+release.stderr)
        archive=next(Path('/build/release').glob('*.tar.gz'))
        self.bundle=Path('/build/bundle');self.bundle.mkdir(mode=0o755)
        with tarfile.open(archive) as packed:
            for entry in packed:
                if not entry.isfile() or Path(entry.name).name != entry.name:
                    raise RuntimeError('release contains non-flat regular payload')
                with packed.extractfile(entry) as src,(self.bundle/entry.name).open('xb') as dst:
                    shutil.copyfileobj(src,dst)
                if entry.mode & ~0o777:raise RuntimeError('release contains privileged mode bits')
                os.chmod(self.bundle/entry.name,entry.mode)
                os.chown(self.bundle/entry.name,1000,1000)
        self.pin=Path(str(archive)+'.checksums-pin').read_text().strip()
        assert all(digest(Path('/work')/name)==value for name,value in before.items())
        write_json(self.evidence/'artifacts.json',{'version':self.version,'archive_sha256':digest(archive),
                   'checksums_pin':self.pin,'source_inputs_unchanged':len(before),
                   'binary_sha256':{p.name:digest(p) for p in self.bundle.iterdir()},
                   'go':self.user(['go','version']).stdout.strip(),'gomaxprocs':2,'goflags':self.env['GOFLAGS']})

    def prepare(self):
        assert self.home.stat().st_uid==1000,'named home volume has wrong owner'
        os.chmod(self.home,0o700)
        self.build()
        shell=self.home/'claude-boot-fixture'
        self.write(shell,b'#!/bin/sh\nif [ "${1:-}" = --version ]; then echo 0.0.0-boot-fixture; exit 0; fi\nif [ "${1:-} ${2:-} ${3:-}" = "auth status --json" ]; then echo \'{"loggedIn":true}\'; exit 0; fi\nwhile IFS= read -r line; do printf "REPLY:%s\\n" "$line"; done\n')
        os.chmod(shell,0o700)
        tls=self.home/'tls';self.user(['mkdir','-m','700',tls])
        self.user(['openssl','req','-x509','-newkey','ec','-pkeyopt','ec_paramgen_curve:P-256','-nodes','-keyout',tls/'key.pem','-out',tls/'cert.pem','-days','1','-subj','/CN=localhost','-addext','subjectAltName=DNS:localhost,IP:127.0.0.1'])
        self.user(['chmod','600',tls/'key.pem',tls/'cert.pem'])
        self.user([self.bundle/'shiki-daemon','init','--config',self.daemon_config,'--user','shikitest','--state-dir',self.daemon_state,'--projects-dir',self.home/'projects','--tailscale=false','--direct-tls-address','127.0.0.1:19527','--tls-cert',tls/'cert.pem','--tls-key',tls/'key.pem','--claude-executable',shell])
        result=run([self.bundle/'shiki-install','install','--role','daemon','--user','shikitest','--prefix','/opt/shiki','--non-interactive','--config',self.daemon_config,'--state-dir',self.daemon_state,'--plugins-dir',self.daemon_state/'plugins','--bin-dir','/opt/shiki-commands','--projects-dir',self.home/'projects','--claude-executable',shell,'--path','/home/shikitest/.local/bin:/usr/bin:/bin','--version',self.version,'--bundle',self.bundle,'--checksums-sha256',self.pin,'--token-output',self.token],timeout=90,check=False)
        assert result.returncode in (0,3),result.stderr
        wait_for(lambda:self.probe('daemon'))
        if not self.token.exists():
            run(['/opt/shiki/current/shiki-install','probe','--role','daemon','--user','shikitest','--prefix','/opt/shiki','--non-interactive','--token-output',self.token])
        run(['loginctl','enable-linger','shikitest'])
        run(['systemctl','start','user@1000.service'])
        wait_for(lambda:Path('/run/user/1000/bus').exists())
        self.write(self.config,b'schema_version: 1\nplugins_dir: ~/.shiki/plugins\nagent:\n  socket: ~/.shiki/run/agent.sock\n  state_dir: ~/.shiki/agent\n  auto_start: false\ntailscale:\n  enabled: false\n  hostname: service-boot-test\n  state_dir: ~/.shiki/tailscale\n  control_url: https://controlplane.tailscale.com\nservers: {}\nprojects: {}\n')
        result=self.user([self.bundle/'shiki-install','install','--role','client','--non-interactive','--version',self.version,'--bundle',self.bundle,'--checksums-sha256',self.pin],timeout=90,check=False)
        assert result.returncode in (0,3),result.stderr
        wait_for(lambda:self.probe('client'))
        self.cli('connect','boot','--tls-address','127.0.0.1:19527','--tls-server-name','localhost','--tls-ca',str(tls/'cert.pem'),'--token-file',str(self.token))
        for plugin in ('sync','port'):
            self.cli('--server','boot','plugins','enable',plugin,'--side','remote')
            self.cli('plugins','enable',plugin,'--side','local')
        unit=Path('/etc/systemd/system/shiki-boot-echo.service')
        unit.write_text('[Unit]\nDescription=Explicit service-boot TCP fixture\n[Service]\nUser=shikitest\nExecStart=/usr/bin/python3 /tools/echo.py\n[Install]\nWantedBy=multi-user.target\n')
        run(['systemctl','daemon-reload']);run(['systemctl','enable','--now','shiki-boot-echo.service'])
        write_json(self.evidence/'units.json',{'daemon':self.ctl('cat','shiki-daemon.service').stdout,'agent':self.ctl('cat','shiki-agent.service',agent=True).stdout,'linger':Path('/var/lib/systemd/linger/shikitest').exists()})
