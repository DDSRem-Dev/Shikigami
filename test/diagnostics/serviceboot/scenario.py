#!/usr/bin/env python3
"""Two phases separated by real PID1 shutdown; verify never starts a service."""
import json
import os
from pathlib import Path
import socket
import sys
import traceback

sys.dont_write_bytecode = True
from common import digest, inventory, process_identity, run, wait_for, write_json
from fixture import Fixture


def workers(f, parent, prefix):
    selected=(Path(prefix)/'current').resolve(strict=True)
    records=[]
    for path in Path('/proc').iterdir():
        if not path.name.isdigit():continue
        try:
            fields=(path/'stat').read_text().rsplit(')',1)[1].split()
            if int(fields[1])!=parent:continue
            command=Path(os.fsdecode((path/'cmdline').read_bytes().split(b'\0')[0])).name
            if command not in ('shiki-plugin-sync','shiki-plugin-port'):continue
            record=process_identity(int(path.name))
            assert record['uids']==[1000]*4
            assert record['executable']==str(selected/command)
            record['sha256']=digest(record['executable'])
            assert record['sha256']==digest(Path('/build/bundle')/command)
            records.append(record)
        except FileNotFoundError:continue
    if sorted(Path(r['executable']).name for r in records)!=['shiki-plugin-port','shiki-plugin-sync']:
        return None
    return records


def boot(f):
    assert Path('/proc/1/comm').read_text().strip() == 'systemd'
    daemon=wait_for(lambda:f.unit('shiki-daemon.service'),timeout=60)
    agent=wait_for(lambda:f.unit('shiki-agent.service',agent=True),timeout=60)
    assert '/system.slice/shiki-daemon.service' in daemon['process']['cgroup']
    assert '/user.slice/' in agent['process']['cgroup']
    for role,unit,prefix in (('daemon',daemon,Path('/opt/shiki')),('agent',agent,f.prefix)):
        executable=(prefix/'current').resolve(strict=True)/('shiki-'+role)
        assert unit['process']['executable']==str(executable)
        unit['process']['sha256']=digest(executable)
        assert unit['process']['sha256']==digest(Path('/build/bundle')/('shiki-'+role))
    assert (f.home/'.shiki/run/agent.sock').exists(), 'service must expose IPC before CLI use'
    assert Path('/var/lib/systemd/linger/shikitest').is_file()
    return {'pid1':process_identity(1),'kernel_boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            'workers':{'daemon':wait_for(lambda:workers(f,int(daemon['MainPID']),'/opt/shiki')),
                       'agent':wait_for(lambda:workers(f,int(agent['MainPID']),f.prefix))},
            'manager':run(['systemctl','show','--property=UserspaceTimestampMonotonic','--property=FinishTimestampMonotonic']).stdout,
            'systemd':run(['systemctl','--version']).stdout.splitlines()[0], 'daemon':daemon,'agent':agent}


def sync(f, mapping):
    return f.cli('sync','status',mapping['id'])


def settled(f,mapping):
    state=sync(f,mapping)
    return state['state']=='active' and int(state['pending_files'])==0 and int(state['conflict_count'])==0


def mappings(f):
    return {m['id']:m for m in f.cli('sync','mappings')['mappings']}


def ledger(f):
    return json.loads((f.home/'.shiki/agent/plugin-state/sync/ledger/state.json').read_text())


def tcp(port, payload):
    with socket.create_connection(('127.0.0.1',port),timeout=5) as stream:
        stream.settimeout(5);stream.sendall(payload);data=b''
        while len(data)<len(payload):
            block=stream.recv(len(payload)-len(data))
            if not block:break
            data+=block
        assert data==payload,'forward did not exchange fresh bytes'
    return True


def seed(f):
    f.prepare()
    state={'boot':boot(f),'mappings':{},'roots':[]}
    state['daemon_id']=wait_for(lambda:f.probe('daemon'))['daemon_id']
    for kind in ('clean','conflict','paused'):
        remote=f.home/'projects'/kind;local=f.home/'local'/kind
        for path in (remote,local):f.write(path/'same.txt',b'common baseline before boot\n')
        project=f.cli('projects','create','boot-'+kind,'--path',str(remote))
        records=f.cli('sync','map',project['id'],'--local',str(local),'--server','boot')['mappings']
        mapping=next(m for m in records if m['local_root']==str(local))
        f.cli('sync','resume',mapping['id']);wait_for(lambda:settled(f,mapping))
        if kind!='clean':
            f.cli('sync','pause',mapping['id']);wait_for(lambda:sync(f,mapping)['state']=='paused')
            f.write(local/'same.txt',(kind+' local edit\n').encode())
            if kind=='conflict':
                f.write(remote/'same.txt',b'conflict remote edit\n')
                f.cli('sync','resume',mapping['id'])
                wait_for(lambda:int(sync(f,mapping)['conflict_count'])==1)
        else:
            session=f.cli('sessions','create','boot-pane','--project',project['id'])
            state['session']=session
            state['snapshot']=f.cli('sessions','snapshot',session['id'],'--description','configuration survives PID1 restart')
            pane=int(f.user(['tmux','-L','shiki-1000','display-message','-p','-t','shiki-'+session['id'],'#{pane_pid}']).stdout.strip())
            state['pane']=process_identity(pane)
        state['mappings'][kind]=mappings(f)[mapping['id']]
        state['roots'] += [str(remote),str(local)]
    allow=f.home/'port-policy.json';f.write(allow,b'{"allow_ports":[19881]}\n')
    f.cli('--server','boot','plugins','config','port','--side','remote','--file',str(allow))
    wait_for(lambda:tcp(19881,b'backend boot one\n'))
    forward=f.cli('--server','boot','ports','forward','19881','--local-port','19882')
    state['forward']=forward['spec']
    tcp(int(forward['bound_port']),b'forward boot one\n')
    state['files']={root:inventory(root) for root in state['roots']}
    state['ledger']=ledger(f)
    state['conflicts']=f.cli('sync','conflicts',state['mappings']['conflict']['id'])['conflicts']
    state['tokens']=json.loads(f.user(['/opt/shiki/current/shiki-daemon','token','list','--config',f.daemon_config]).stdout)
    state['immutable']={str(p):digest(p) for p in (f.config,f.daemon_config,f.token,f.home/'tls/cert.pem',f.home/'tls/key.pem')}
    state['credentials']=inventory(f.home/'.shiki/agent/credentials')
    state['server']=f.cli('servers')['servers'][0]
    # A private checkpoint contains only fixture metadata; do not export its
    # authentication material or raw persistent state as host-side evidence.
    write_json('/checkpoint.json',state)
    write_json(f.evidence/'boot1.json',{'boot':state['boot'],'daemon_id':state['daemon_id'],
               'mapping_ids':{k:m['id'] for k,m in state['mappings'].items()},'desired_sync':{k:m['sync_enabled'] for k,m in state['mappings'].items()},
               'forward_id':state['forward']['id'],'session_id':state['session']['id'],'snapshot_id':state['snapshot']['id'],
               'pane':state['pane'],'conflicts':len(state['conflicts']),'credential_count':len(state['credentials']),
               'immutable_hashes':state['immutable'],'services_remain_active_for_poweroff':True})
    print('BOOT1 PASS: real services and persistent baseline/conflict/paused/forward/session state ready for poweroff',flush=True)


def verify(f):
    state=json.loads(Path('/checkpoint.json').read_text())
    # These are read-only systemd observations, before any product CLI/probe.
    current=boot(f)
    assert current['pid1']['start_ticks']!=state['boot']['pid1']['start_ticks'],'PID1 did not start again'
    for role in ('daemon','agent'):
        assert current[role]['InvocationID']!=state['boot'][role]['InvocationID'],'unit invocation did not change'
        assert current[role]['process']['start_ticks']!=state['boot'][role]['process']['start_ticks']
        previous={Path(p['executable']).name:p for p in state['boot']['workers'][role]}
        for worker in current['workers'][role]:
            assert worker['start_ticks']!=previous[Path(worker['executable']).name]['start_ticks'],'old worker generation survived'
    assert all(digest(path)==value for path,value in state['immutable'].items()),'configuration/token was replaced'
    assert inventory(f.home/'.shiki/agent/credentials')==state['credentials'],'stored credentials changed'
    assert wait_for(lambda:f.probe('daemon'))['daemon_id']==state['daemon_id']
    wait_for(lambda:f.probe('client'))
    assert f.cli('agent','status')['state']=='ready'
    assert mappings(f)=={m['id']:m for m in state['mappings'].values()},'mapping identity/revision/intent changed'
    wait_for(lambda:settled(f,state['mappings']['clean']))
    wait_for(lambda:int(sync(f,state['mappings']['conflict'])['conflict_count'])==1)
    wait_for(lambda:sync(f,state['mappings']['paused'])['state']=='paused')
    assert f.cli('sync','conflicts',state['mappings']['conflict']['id'])['conflicts']==state['conflicts'],'conflict lineage changed'
    assert {root:inventory(root) for root in state['roots']}==state['files'],'shutdown/restart changed project files'
    latest=ledger(f)
    for mapping in state['mappings'].values():
        key=mapping['id']
        assert latest['buckets']['sync_mappings'][key]['baselines']==state['ledger']['buckets']['sync_mappings'][key]['baselines'],'baseline changed'
    tokens=json.loads(f.user(['/opt/shiki/current/shiki-daemon','token','list','--config',f.daemon_config]).stdout)
    assert tokens==state['tokens'],'token registry changed'
    server=f.cli('servers')['servers'][0]
    for key in ('config','revision','credential_revision','credential_present'):
        assert server[key]==state['server'][key],'server generation or credential changed'
    # A host/PID namespace restart loses tmux. Only configuration is retained.
    session=f.cli('sessions','info',state['session']['id'])
    assert int(session['claude_pid'])==0 and session['claude_status'] in ('stopped','unknown'),session
    assert session['name']==state['session']['name'] and session['project_id']==state['session']['project_id']
    assert f.user(['tmux','-L','shiki-1000','has-session','-t','shiki-'+session['id']],check=False).returncode!=0,'old tmux session resurrected'
    snapshots=f.cli('sessions','snapshots')['snapshots']
    assert state['snapshot'] in snapshots,'configuration snapshot was lost'
    def forwarding():
        rows=f.cli('ports','list')['forwards']
        row=next((r for r in rows if r['spec']['id']==state['forward']['id']),None)
        if row and row['state']=='listening':
            assert row['spec']==state['forward'],'forward desired state changed'
            return row
    forward=wait_for(forwarding,timeout=45)
    tcp(int(forward['bound_port']),b'new connection after second PID1 boot\n')
    write_json(f.evidence/'boot2-persistence.json',{'boot':current,'automatic_units_before_cli':True,
               'immutable_config_credentials_identity':True,'mapping_revisions_baselines_conflict_paused':True,
               'original_token_authenticated':True,'old_tmux_absent_configuration_retained':True,'new_forward_tcp_passed':True})
    local=f.home/'local/clean';remote=f.home/'projects/clean'
    mapping=state['mappings']['clean']
    # Known common baselines permit automatic one-sided edits in both directions.
    f.write(local/'same.txt',b'automatic upload after second boot\n')
    wait_for(lambda:(remote/'same.txt').read_bytes()==b'automatic upload after second boot\n')
    wait_for(lambda:settled(f,mapping))
    f.write(remote/'same.txt',b'automatic download after second boot\n')
    wait_for(lambda:(local/'same.txt').read_bytes()==b'automatic download after second boot\n')
    wait_for(lambda:settled(f,mapping))
    # A never-observed path has no B: docs10 requires an explicit initial choice.
    for name,source,target,choice,payload in (
        ('after-boot.txt',local,remote,'keep-local',b'new upload explicitly chosen after boot two\n'),
        ('from-remote.txt',remote,local,'keep-remote',b'new download explicitly chosen after boot two\n')):
        f.write(source/name,payload)
        def initial_choice():
            conflicts=f.cli('sync','conflicts',mapping['id'])['conflicts']
            found=next((c for c in conflicts if c['path']==name),None)
            if found:
                assert not found.get('base'),'new path unexpectedly has a common baseline'
                assert not (target/name).exists(),'new path propagated without explicit choice'
                return found
        conflict=wait_for(initial_choice)
        assert sync(f,mapping)['reason_code']=='INITIAL_SYNC_CHOICE_REQUIRED'
        f.cli('sync','resolve',conflict['id'],'--mapping',mapping['id'],'--generation',conflict['generation'],'--choice',choice,'--yes')
        wait_for(lambda:(target/name).exists() and (target/name).read_bytes()==payload)
        wait_for(lambda:settled(f,mapping))
    restored=f.cli('sessions','restore',state['snapshot']['id'],'--name','explicit-new-after-boot','--yes')
    assert restored['id']!=session['id'],'explicit configuration restore reused original identity'
    wait_for(lambda:int(f.cli('sessions','info',restored['id'])['claude_pid'])>0)
    assert (remote/'after-boot.txt').read_bytes()==b'new upload explicitly chosen after boot two\n','snapshot restored old files'
    write_json(f.evidence/'boot2.json',{'boot':current,'automatic_units_observed_before_cli':True,
               'same_daemon_id_original_token_and_credentials':True,'mapping_revisions_and_desired_preserved':True,
               'baselines_and_unresolved_conflict_preserved':True,'paused_mapping_remains_paused':True,
               'forward_desired_preserved_and_fresh_tcp_passed':True,'existing_baseline_automatic_bidirectional_sync_passed':True,
               'new_paths_require_explicit_initial_choices':True,'new_bidirectional_files_after_choices_passed':True,
               'old_session_status':session['claude_status'],'old_tmux_not_restored':True,
               'snapshot_preserved_explicit_new_session':restored['id'],'files_not_rolled_back':True,
               'kernel_boot_id_unchanged':current['kernel_boot_id']==state['boot']['kernel_boot_id'],
               'external_peer_acceptance':'pending','physical_kernel_reboot_acceptance':'not performed'})
    print('BOOT2 PASS: units automatically started, persistence and fresh data plane verified',flush=True)

if __name__=='__main__':
    try:
        f=Fixture()
        if sys.argv[1]=='prepare':seed(f)
        elif sys.argv[1]=='verify':verify(f)
        else:raise ValueError('unknown phase')
    except Exception:
        traceback.print_exc()
        if 'f' in globals():
            diagnostic={}
            try:
                checkpoint=json.loads(Path('/checkpoint.json').read_text())
                for kind,mapping in checkpoint['mappings'].items():
                    diagnostic[kind]={'status':sync(f,mapping),'conflicts':f.cli('sync','conflicts',mapping['id'])}
                diagnostic['plugins_local']=f.cli('plugins','list','--side','local')
                diagnostic['plugins_remote']=f.cli('--server','boot','plugins','list','--side','remote')
                write_json(f.evidence/'failure-sync-diagnostics.json',diagnostic)
            except Exception as diagnostic_error:
                (f.evidence/'failure-diagnostic-error.txt').write_text(str(diagnostic_error))
        raise
