"""Real offline tailscaled LocalAPI and immutable-state observations."""
import http.client
import json
import os
from pathlib import Path
import socket
import stat
import time

from common import digest, process_identity, run, wait_for, write_json

SOCKET=Path('/run/tailscale-existing/tailscaled.sock')
STATE=Path('/var/lib/tailscale-existing/tailscaled.state')
UNIT=Path('/etc/systemd/system/tailscaled.service')
CONFIG=Path('/etc/default/tailscaled')
BINARY=Path('/usr/local/sbin/tailscaled')

class UnixHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(str(SOCKET))


def status():
    connection=UnixHTTP('local-tailscaled.sock',timeout=5)
    try:
        connection.request('GET','/localapi/v0/status?peers=false')
        response=connection.getresponse()
        assert response.status==200,'LocalAPI status was not HTTP200'
        raw=response.read(1048577)
        assert len(raw)<=1048576,'LocalAPI status exceeded bound'
        value=json.loads(raw)
        assert not value.get('AuthURL'),'unexpected login URL; no authentication may be initiated'
        assert not value.get('TailscaleIPs'),'fixture unexpectedly joined a tailnet'
        if value['BackendState'] in ('NoState','Starting'):return None
        assert value['BackendState'] in ('NeedsLogin','Stopped'),'unexpected authenticated backend'
        assert value['Version'].startswith('1.98.10'),'running version differs from locked source'
        # Never export raw status, node/user metadata, login URLs or key material.
        return {'version':value['Version'],'backend_state':value['BackendState'],
                'has_auth_url':False,'tailnet_addresses':0,'localapi_http_status':200}
    finally:connection.close()


def file_record(path, socket_file=False):
    info=path.lstat()
    assert info.st_uid==0 and info.st_gid==0,'existing system files must retain root ownership'
    assert stat.S_ISSOCK(info.st_mode) if socket_file else stat.S_ISREG(info.st_mode)
    record={'device':info.st_dev,'inode':info.st_ino,'mode':stat.S_IMODE(info.st_mode),
            'uid':info.st_uid,'gid':info.st_gid,'size':info.st_size,'mtime_ns':info.st_mtime_ns}
    if not socket_file:record['sha256']=digest(path)
    return record


def observe():
    result=run(['systemctl','show','tailscaled.service','--property=MainPID','--property=InvocationID',
                '--property=ActiveState','--property=SubState','--property=UnitFileState','--property=NRestarts'])
    unit=dict(line.split('=',1) for line in result.stdout.splitlines() if '=' in line)
    assert unit['ActiveState']=='active' and unit['SubState']=='running' and unit['UnitFileState']=='enabled'
    process=process_identity(int(unit['MainPID']))
    assert process['uids']==[0]*4 and process['executable']==str(BINARY)
    assert '/system.slice/tailscaled.service' in process['cgroup']
    state=file_record(STATE)
    assert state['size']>0 and state['mode']==0o600,'state must be actual nonempty private file'
    for directory in (STATE.parent,SOCKET.parent):
        info=directory.stat();assert info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o700
    api=status()
    assert api is not None,'existing daemon regressed to startup state'
    return {'unit':unit,'process':process,'socket':file_record(SOCKET,True),
            'files':{str(p):file_record(p) for p in (UNIT,CONFIG,BINARY,STATE)},'localapi':api}


def start(f):
    build=f.user(['go','build','-trimpath','-o','/build/tailscaled','tailscale.com/cmd/tailscaled'],cwd='/work',timeout=900)
    (f.evidence/'tailscaled-build.log').write_text(build.stdout+build.stderr)
    build_info=f.user(['go','version','-m','/build/tailscaled']).stdout
    module_lines=[line.split() for line in build_info.splitlines() if len(line.split())>=3 and line.split()[0] in ('mod','dep') and line.split()[1]=='tailscale.com']
    assert len(module_lines)==1 and module_lines[0][2]=='v1.98.10','binary is not built from locked tailscale module'
    run(['install','-m','755','/build/tailscaled',BINARY])
    CONFIG.parent.mkdir(parents=True,exist_ok=True)
    CONFIG.write_text('FLAGS="--tun=userspace-networking --socket=/run/tailscale-existing/tailscaled.sock --state=/var/lib/tailscale-existing/tailscaled.state --port=0"\n')
    os.chmod(CONFIG,0o600)
    UNIT.write_text('[Unit]\nDescription=Existing real offline Tailscale system service\n[Service]\nType=simple\nUser=root\nEnvironment=TS_NO_LOGS_NO_SUPPORT=true\nEnvironmentFile=/etc/default/tailscaled\nExecStart=/usr/local/sbin/tailscaled $FLAGS\nRuntimeDirectory=tailscale-existing\nRuntimeDirectoryMode=0700\nStateDirectory=tailscale-existing\nStateDirectoryMode=0700\nUMask=0077\nNoNewPrivileges=true\nCapabilityBoundingSet=\nStandardOutput=null\nStandardError=null\n[Install]\nWantedBy=multi-user.target\n')
    run(['systemctl','daemon-reload']);run(['systemctl','enable','--now','tailscaled.service'])
    wait_for(status,timeout=30)
    # Do not suspend/freeze the daemon to keep its files stable. Establish an
    # actually quiescent logged-out baseline before any Shiki installer call.
    baseline=observe()
    for _ in range(3):
        time.sleep(0.25)
        assert observe()==baseline,'tailscaled did not settle before installer boundaries'
    version=run([BINARY,'--version']).stdout.strip()
    assert version.startswith('1.98.10')
    write_json(f.evidence/'tailscaled-build.json',{'build_info':build_info,'actual_version_output':version,
               'binary_sha256':digest(BINARY),'module_version':'tailscale.com v1.98.10',
               'distribution':'local build of pinned upstream source, not official release binary',
               'state_bytes':baseline['files'][str(STATE)]['size'],'authenticated_identity':'none; never logged in'})
    return baseline
