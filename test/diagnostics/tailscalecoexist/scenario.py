#!/usr/bin/env python3
"""Installer lifecycle with a live existing tailscaled; never log in."""
import json
import os
from pathlib import Path
import shutil
import sys
import tarfile
import traceback

sys.dont_write_bytecode=True
from common import digest, inventory, process_identity, run, wait_for, write_json
from service_fixture import Fixture as ServiceFixture
import tailscale

class Fixture(ServiceFixture):
    a,b='coexist-local-a','coexist-local-b'
    version=a

    def build(self):
        self.bundles={};self.observations=[]
        for path in (Path('/build'),Path('/build/tmp'),Path('/var/cache/shiki-go')):
            path.mkdir(mode=0o700,parents=True,exist_ok=True);os.chown(path,1000,1000)
        original=inventory('/source');shutil.copytree('/source','/work')
        probe=Path('/work/test/diagnostics/tailscalecoexist/nodeprobe')
        shutil.copytree('/tools/nodeprobe',probe)
        run(['chown','-R','1000:1000','/work'])
        self.baseline=tailscale.start(self)
        write_json(self.evidence/'existing-before-shiki.json',self.baseline)
        # This Go helper only negotiates/reads the actual private NodeControl.
        self.user(['go','build','-o','/build/node-probe','./test/diagnostics/tailscalecoexist/nodeprobe'],cwd='/work',timeout=180)
        run(['install','-D','-m','755','/build/node-probe','/usr/local/libexec/shiki-node-probe'])
        for version in (self.a,self.b):
            print('Building current-source Shiki bundle '+version,flush=True)
            build=self.user(['env','SHIKI_VERSION='+version,'bash','scripts/build.sh'],cwd='/work',timeout=900)
            release=self.user(['go','run','scripts/release.go','--dir','bin','--output','/build/'+version,'--version',version],cwd='/work',timeout=180)
            (self.evidence/(version+'-build.log')).write_text(build.stdout+build.stderr+release.stdout+release.stderr)
            archive=next(Path('/build',version).glob('*.tar.gz'));bundle=archive.parent/'bundle';bundle.mkdir(mode=0o755)
            with tarfile.open(archive) as packed:
                for entry in packed:
                    if not entry.isfile() or Path(entry.name).name!=entry.name or entry.mode & ~0o777:
                        raise RuntimeError('release payload must be flat regular unprivileged files')
                    with packed.extractfile(entry) as src,(bundle/entry.name).open('xb') as dst:
                        shutil.copyfileobj(src,dst)
                    os.chmod(bundle/entry.name,entry.mode);os.chown(bundle/entry.name,1000,1000)
            pin=Path(str(archive)+'.checksums-pin').read_text().strip();self.bundles[version]=(bundle,pin)
            write_json(self.evidence/(version+'-artifacts.json'),{'version':version,'archive_sha256':digest(archive),
                       'checksums_pin':pin,'files':{p.name:digest(p) for p in bundle.iterdir()},
                       'upgrade_scope':'same product source, version-label installer lifecycle; not historical/schema migration'})
            assert all(digest(Path('/work')/name)==value for name,value in original.items())
        self.bundle,self.pin=self.bundles[self.a]
        self.check('before-initial-shiki-install')

    def check(self,label):
        current=tailscale.observe()
        assert current==self.baseline,'existing tailscaled changed at '+label
        self.observations.append({'boundary':label,'observed':current})
        (self.evidence/'boundaries.json').write_text(json.dumps(self.observations,indent=2)+'\n')
        print('UNCHANGED existing tailscaled: '+label,flush=True)

    def operation(self,role,operation,version,expected=None,initial=False):
        self.check('before-'+role+'-'+operation+'-'+version)
        bundle,pin=self.bundles[version]
        args=[bundle/'shiki-install',operation,'--role',role,'--non-interactive']
        execute=self.user if role=='client' else run
        if role=='daemon':args+=['--user','shikitest','--prefix','/opt/shiki']
        if operation=='install':
            args+=['--version',version,'--bundle',bundle,'--checksums-sha256',pin]
            if role=='daemon':
                args+=['--config',self.daemon_config,'--state-dir',self.daemon_state,'--plugins-dir',self.daemon_state/'plugins',
                       '--bin-dir','/opt/shiki-commands','--projects-dir',self.home/'projects','--claude-executable',self.home/'claude-boot-fixture',
                       '--path','/home/shikitest/.local/bin:/usr/bin:/bin']
                if initial:args+=['--token-output',self.home/'reinstalled-device.token']
        response=execute(args,timeout=90,check=False)
        assert response.returncode in (0,3),'real helper failed '+role+' '+operation+': '+response.stderr
        result=json.loads(response.stdout)
        if operation!='uninstall':
            ready=wait_for(lambda:self.probe(role),timeout=45)
            assert ready['version']==(expected or version)
        self.check('after-'+role+'-'+operation+'-'+version)
        return result

    def live_shiki(self,project):
        rows=self.cli('projects','list')['projects']
        assert any(p['id']==project['id'] for p in rows),'original credential/project no longer usable'
        for role,prefix in (('daemon',Path('/opt/shiki')),('agent',self.prefix)):
            unit=wait_for(lambda:self.unit('shiki-'+role+'.service',agent=role=='agent'))
            assert unit['process']['uids']==[1000]*4
            assert unit['process']['executable']==str((prefix/'current').resolve() / ('shiki-'+role))
        assert (self.home/'projects/coexist/keep.txt').read_bytes()==b'preserve through actual installer lifecycle\n'


def pending_node(f):
    f.check('before-embedded-tsnet-unlogged-install')
    home=Path('/home/shikipending');run(['useradd','-m','-u','1001','shikipending']);os.chmod(home,0o700)
    def user(args,**kwargs):
        return run(['/usr/sbin/runuser','-u','shikipending','--','env','HOME='+str(home),'USER=shikipending',
                    'XDG_RUNTIME_DIR=/run/user/1001','PATH=/home/shikipending/.local/bin:/usr/bin:/bin',*args],**kwargs)
    config=home/'.shiki/config.yaml';user(['mkdir','-p','-m','700',config.parent])
    data=b'schema_version: 1\nplugins_dir: ~/.shiki/plugins\nagent:\n  socket: ~/.shiki/run/agent.sock\n  state_dir: ~/.shiki/agent\n  auto_start: false\ntailscale:\n  enabled: true\n  hostname: shiki-coexist-pending\n  state_dir: ~/.shiki/tailscale\n  control_url: https://controlplane.tailscale.com\nservers: {}\nprojects: {}\n'
    user(['python3','-c','import os,sys; fd=os.open(sys.argv[1],os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600); os.write(fd,sys.stdin.buffer.read()); os.close(fd)',config],input=data.decode())
    before=digest(config)
    run(['loginctl','enable-linger','shikipending']);run(['systemctl','start','user@1001.service'])
    wait_for(lambda:Path('/run/user/1001/bus').exists())
    # Only the already verified A bundle is shared as immutable input with the
    # second ordinary user. No existing user's credential/state is shared.
    bundle,pin=f.bundles[f.a]
    shared=Path('/opt/coexist-pending-bundle');shutil.copytree(bundle,shared)
    for path in shared.iterdir():
        os.chown(path,1001,1001)
    response=user([shared/'shiki-install','install','--role','client','--non-interactive','--version',f.a,'--bundle',shared,'--checksums-sha256',pin],timeout=90,check=False)
    assert response.returncode==3,'unauthenticated embedded node must report pending, not success'
    report=json.loads(response.stdout)
    write_json(f.evidence/'embedded-installer.json',{'exit':response.returncode,
               'installer':{k:report.get(k) for k in ('state','reason','node_state','node_reason','external_peer_acceptance')}})
    assert report['state']!='local_application_ready' and report['external_peer_acceptance']=='pending'
    assert not report.get('token_issued') and not report.get('node_addresses')
    last_probe={}
    def node():
        reply=user(['/usr/local/libexec/shiki-node-probe',home/'.shiki/run/agent.sock'],check=False)
        last_probe.update({'exit':reply.returncode,'safe_diagnostic':reply.stderr.strip()})
        return json.loads(reply.stdout) if reply.returncode==0 else None
    try:observed=wait_for(node,timeout=30)
    finally:write_json(f.evidence/'embedded-node-probe.json',last_probe)
    assert observed['uid']==1001 and observed['reason_code']!='NODE_DISABLED' and observed['instance_id']
    assert digest(config)==before
    state=home/'.shiki/tailscale';info=state.stat()
    assert info.st_uid==1001 and info.st_mode & 0o777==0o700
    files={str(p.relative_to(state)):{'size':p.stat().st_size,'mode':p.stat().st_mode & 0o777,'uid':p.stat().st_uid,'sha256':digest(p)} for p in state.rglob('*') if p.is_file()}
    assert files and any(p['size']>0 for p in files.values()),'enabled embedded tsnet did not initialize nonempty independent state files'
    assert all(p['uid']==1001 and p['mode'] & 0o077==0 for p in files.values())
    denied=user(['python3','-c','''import errno,json,socket,sys
result={}
for label,path in zip(('system_socket','system_state'),sys.argv[1:]):
    try:
        if label=='system_socket':
            with socket.socket(socket.AF_UNIX) as sock:sock.connect(path)
        else:
            with open(path,'rb') as state:state.read(1)
    except OSError as error:
        assert error.errno==errno.EACCES
        result[label]={'access':'denied','errno':error.errno,'uid':1001}
    else:raise RuntimeError('ordinary user accessed private existing system tailscale resource')
print(json.dumps(result))
''',tailscale.SOCKET,tailscale.STATE])
    isolation=json.loads(denied.stdout)
    mainpid=int(user(['systemctl','--user','show','shiki-agent.service','--property=MainPID','--value']).stdout)
    process=process_identity(mainpid);assert process['uids']==[1001]*4
    assert mainpid!=f.baseline['process']['pid']
    f.check('after-embedded-tsnet-unlogged-install')
    write_json(f.evidence/'embedded-pending.json',{'installer_exit':3,'installer':{k:report.get(k) for k in ('state','reason','node_state','node_reason','external_peer_acceptance')},
               'node_control':observed,'process':process,'private_state_path':str(state),'private_files':files,
               'system_tailscale_state_path':str(tailscale.STATE),'system_resource_access':isolation,
               'scope':'actual embedded tsnet enabled and unauthenticated; no login/up/key/external network'})


def main():
    f=Fixture();f.prepare();f.check('after-initial-daemon-and-client-install')
    f.write(f.home/'projects/coexist/keep.txt',b'preserve through actual installer lifecycle\n')
    project=f.cli('projects','create','coexist','--path',str(f.home/'projects/coexist'))
    original_token=digest(f.token);original_daemon=wait_for(lambda:f.probe('daemon'))['daemon_id']
    f.live_shiki(project)
    for role in ('daemon','client'):
        f.operation(role,'install',f.b);f.live_shiki(project)
    for role in ('client','daemon'):
        f.operation(role,'rollback',f.b,expected=f.a);f.live_shiki(project)
    f.operation('client','uninstall',f.a)
    f.operation('daemon','uninstall',f.a)
    f.operation('daemon','install',f.a,initial=True)
    f.operation('client','install',f.a)
    f.live_shiki(project)
    assert digest(f.token)==original_token and f.probe('daemon')['daemon_id']==original_daemon
    pending_node(f)
    f.check('final-before-poweroff')
    write_json(f.evidence/'acceptance.json',{'state':'passed','boundaries':len(f.observations),
               'original_tailscaled_same_process_unit_config_socket_state':True,'real_readonly_LocalAPI_every_boundary':True,
               'original_shiki_token_and_daemon_id_retained':True,'tls_only_both_role_install_upgrade_rollback_uninstall_reinstall':True,
               'embedded_tsnet_enabled_unauthenticated_separate_state':True,'external_authenticated_node_coexistence':'not tested',
               'release_compatibility':'same630-source version labels only','ordinary_shiki_uid':1000,'pending_shiki_uid':1001})
    print('PASS: real existing tailscaled unchanged through installer lifecycle and embedded unauthenticated startup',flush=True)

if __name__=='__main__':
    try:main()
    except Exception:
        traceback.print_exc()
        try:
            write_json('/evidence/failure-safe-tailscale.json',tailscale.observe())
        except Exception as diagnostic:
            Path('/evidence/failure-observation.txt').write_text(type(diagnostic).__name__+': '+str(diagnostic))
        raise
