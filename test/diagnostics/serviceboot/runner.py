#!/usr/bin/env python3
"""Opt-in real systemd PID1 second-start persistence acceptance."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import traceback
import uuid

sys.dont_write_bytecode=True
from common import digest, inventory, run, wait_for, write_json

LABEL='dev.shikigami.service-boot'

def main(args):
    repo=Path(__file__).resolve().parents[3]
    output=Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='shiki-service-boot-',dir='/tmp'))
    if output==repo or repo in output.parents:raise ValueError('output must be outside repository')
    output.mkdir(mode=0o700,parents=True,exist_ok=True);os.chmod(output,0o700)
    if any(output.iterdir()):raise ValueError('output must be empty')
    print('Evidence: '+str(output),flush=True)
    source=output/'source';tools=output/'tools'
    if args.source:
        if not args.manifest or not args.manifest_sha256:raise ValueError('source requires manifest and pinned manifest SHA256')
        manifest_path=Path(args.manifest)
        assert digest(manifest_path)==args.manifest_sha256
        expected=json.loads(manifest_path.read_text())
        if isinstance(expected,list):expected={e['path']:e['sha256'] for e in expected}
        assert inventory(args.source)==expected
        shutil.copytree(args.source,source)
    else:
        shutil.copytree(repo,source,ignore=shutil.ignore_patterns('.git','.tools','bin','dist','__pycache__','.DS_Store'))
        expected=inventory(source)
    shutil.copytree(Path(__file__).parent,tools,ignore=shutil.ignore_patterns('__pycache__'))
    shutil.copy2(repo/'scripts/test-service-boot.sh',tools/'wrapper.sh')
    tool_hashes=inventory(tools)
    assert inventory(source)==expected
    write_json(output/'source-sha256.json',expected);write_json(output/'tool-sha256.json',tool_hashes)
    info=json.loads(run(['docker','info','--format','{{json .}}']).stdout)
    assert info['OSType']=='linux' and info['CgroupVersion']=='2'
    modules=Path(run(['go','env','GOMODCACHE'],cwd=source).stdout.strip()).resolve()
    name='shiki-service-boot-'+uuid.uuid4().hex[:12];image=name+':local';volume=name+'-home';cid=None
    evidence={'source_files':len(expected),'source_manifest_sha256':digest(output/'source-sha256.json'),
              'tools_manifest_sha256':digest(output/'tool-sha256.json'),'container_name':name,'volume':volume,
              'forced_cleanup':False,'started':time.time(),'boots':[],
              'scope':'same container two real systemd PID1 starts; persistent named home volume and retained rootfs; no physical kernel reboot or external account acceptance'}
    created_volume=False;created_container=False;image_created=False
    try:
        print('Building fixed-base systemd image',flush=True)
        build=run(['docker','build','--platform','linux/arm64','-t',image,tools],timeout=600)
        image_created=True;(output/'image-build.log').write_text(build.stdout+build.stderr)
        run(['docker','volume','create','--label',LABEL+'='+name,volume]);created_volume=True
        created_container=True
        cid=run(['docker','create','--platform','linux/arm64','--name',name,'--label',LABEL+'='+name,
                 '--cgroupns','private','--network','none','--cap-add','SYS_ADMIN',
                 '--tmpfs','/run','--tmpfs','/run/lock','--tmpfs','/tmp',
                 '--mount',f'type=bind,source={source},target=/source,readonly',
                 '--mount',f'type=bind,source={tools},target=/tools,readonly',
                 '--mount',f'type=bind,source={modules},target=/go/pkg/mod,readonly',
                 '--mount',f'type=volume,source={volume},target=/home/shikitest',image,
                 '/bin/bash','-c','set -euo pipefail; mount -o remount,rw /sys/fs/cgroup; exec /sbin/init --log-target=console --log-level=info']).stdout.strip()
        (output/'container.cid').write_text(cid+'\n')
        for number,mode in ((1,'prepare'),(2,'verify')):
            print(f'BOOT {number}: docker start same container; scenario {mode}',flush=True)
            run(['docker','start',cid])
            wait_for(lambda:run(['docker','exec',cid,'systemctl','is-system-running'],check=False).stdout.strip()=='running',timeout=60)
            inspect=json.loads(run(['docker','inspect',cid]).stdout)[0]
            assert inspect['Id']==cid and inspect['Config']['Labels'][LABEL]==name
            host=inspect['HostConfig']
            assert not host['Privileged'] and host['CgroupnsMode']=='private' and host['PidMode']!='host' and host['IpcMode']!='host'
            assert host['NetworkMode']=='none' and not host['PortBindings']
            assert [x.removeprefix('CAP_') for x in host['CapAdd']]==['SYS_ADMIN']
            assert not host['SecurityOpt'],'default seccomp must remain in effect'
            mounts=inspect['Mounts'];assert len(mounts)==4
            assert all(not m['RW'] for m in mounts if m['Type']=='bind')
            assert [m['Name'] for m in mounts if m['Type']=='volume']==[volume]
            write_json(output/f'boot{number}-inspect.json',inspect)
            entry={'started_at':inspect['State']['StartedAt'],'host_pid':inspect['State']['Pid'],'container_id':cid}
            if evidence['boots']:
                assert entry['started_at']!=evidence['boots'][0]['started_at']
                # Host PIDs may be reused too; StartedAt and inside PID1 start ticks
                # establish a distinct process lifetime without assuming otherwise.
            evidence['boots'].append(entry)
            with (output/f'boot{number}-scenario.log').open('x') as log:
                process=subprocess.run(['docker','exec',cid,'python3','-B','/tools/scenario.py',mode],stdout=log,stderr=subprocess.STDOUT,timeout=1500 if number==1 else 300)
            run(['docker','cp',cid+':/evidence/.',output/'container-evidence'],check=process.returncode==0)
            if process.returncode:raise RuntimeError(f'boot{number} scenario exit {process.returncode}; see retained scenario log')
            # Never docker stop/restart in the accepted path: PID1 handles poweroff.
            print(f'BOOT {number}: requesting actual systemctl poweroff',flush=True)
            run(['docker','exec',cid,'systemctl','poweroff'])
            exited=run(['docker','wait',cid],timeout=60)
            state=json.loads(run(['docker','inspect','--format','{{json .State}}',cid]).stdout)
            assert exited.stdout.strip()=='0' and not state['Running'] and state['Pid']==0 and not state['OOMKilled']
            write_json(output/f'boot{number}-shutdown.json',state)
            entry['normal_poweroff_exit']=0
        evidence['acceptance']='passed'
    finally:
        signal.signal(signal.SIGINT,signal.SIG_IGN);signal.signal(signal.SIGTERM,signal.SIG_IGN)
        if created_container:
            found=run(['docker','inspect',cid or name],check=False)
            if found.returncode==0:
                inspect=json.loads(found.stdout)[0]
                assert inspect['Config']['Labels'].get(LABEL)==name
                cid=inspect['Id'];logs=run(['docker','logs',cid],check=False)
                (output/'systemd.log').write_text(logs.stdout+logs.stderr)
                run(['docker','cp',cid+':/evidence/.',output/'container-evidence'],check=False)
                if inspect['State']['Running']:
                    for label,command in [('journal',['journalctl','--no-pager']),('processes',['ps','-eo','pid,ppid,uid,stat,comm'])]:
                        result=run(['docker','exec',cid,*command],check=False)
                        (output/f'failure-{label}.log').write_text(result.stdout+result.stderr)
                    evidence['forced_cleanup']=True
                    run(['docker','exec',cid,'systemctl','poweroff'],check=False)
                    try:run(['docker','wait',cid],timeout=30)
                    except subprocess.TimeoutExpired:run(['docker','stop','--time','20',cid],check=False,timeout=30)
                write_json(output/'cleanup-state.json',json.loads(run(['docker','inspect','--format','{{json .State}}',cid]).stdout))
                run(['docker','rm','-f',cid])
            absent=run(['docker','inspect',cid or name],check=False)
            ids=run(['docker','ps','-a','--no-trunc','--format','{{.ID}}']).stdout.splitlines()
            evidence['container_absent']=absent.returncode!=0 and 'no such object' in absent.stderr.lower() and cid not in ids
        if created_volume:
            v=json.loads(run(['docker','volume','inspect',volume]).stdout)[0]
            assert v['Labels'].get(LABEL)==name
            run(['docker','volume','rm',volume])
            absent=run(['docker','volume','inspect',volume],check=False)
            names=run(['docker','volume','ls','--format','{{.Name}}']).stdout.splitlines()
            evidence['volume_absent']=absent.returncode!=0 and 'no such volume' in absent.stderr.lower() and volume not in names
        if image_created:run(['docker','image','rm',image],check=False)
        evidence['source_unchanged']=inventory(source)==expected
        evidence['tools_unchanged']=inventory(tools)==tool_hashes
        evidence['container_id']=cid;evidence['finished']=time.time()
        write_json(output/'evidence.json',evidence)
    assert evidence['acceptance']=='passed' and not evidence['forced_cleanup']
    assert all(evidence[k] for k in ('source_unchanged','tools_unchanged','container_absent','volume_absent'))
    print('PASS: '+str(output/'evidence.json'),flush=True)

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output');parser.add_argument('--source');parser.add_argument('--manifest');parser.add_argument('--manifest-sha256')
    args=parser.parse_args();interrupted_signal=signal.SIGINT
    def interrupt(signum,frame):
        global interrupted_signal
        interrupted_signal=signum
        raise KeyboardInterrupt
    signal.signal(signal.SIGINT,interrupt);signal.signal(signal.SIGTERM,interrupt)
    try:main(args)
    except KeyboardInterrupt:sys.exit(128+interrupted_signal)
    except Exception:traceback.print_exc();sys.exit(1)
