//go:build darwin && e2e

package e2e

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// These guard/failure tests mock subprocess calls; they never query or mutate
// the actual launchd domain, plist, tmux server or any process identity.
func TestLaunchdActivityCleanupFailsClosedAndContinuesIndependentResources(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	helper := filepath.Join(filepath.Dir(source), "../../scripts/test-launchd-activity-cleanup.py")
	script := `
import importlib.util,json,pathlib,subprocess,sys,tempfile
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('activity_cleanup',sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
def response(code=0,out='',err=''):
    return subprocess.CompletedProcess([],code,out,err)
def rejects(fn):
    try: fn()
    except RuntimeError: return
    raise AssertionError('unsafe or unproven state accepted')
with patch.object(m,'run',return_value=response(1,err='permission denied')):
    rejects(lambda:m.job_absent('gui/501','gui/501/dev.shikigami.agent'))
with patch.object(m,'run',side_effect=[response(),response(1,err='Could not find service')]):
    assert m.job_absent('gui/501','gui/501/dev.shikigami.agent')
with patch.object(m,'run',side_effect=[response(),response(1,err='domain disappeared')]):
    rejects(lambda:m.job_absent('gui/501','gui/501/dev.shikigami.agent'))
root=pathlib.Path('/private/tmp/shiki-launchd.mock')
record={'session_id':'sess_owned','tmux_id':'$4','pane_id':'%6','pid':123,'fixture':str(root/'runtime/terminal-fixture')}
wrong='shiki-sess_other\t$4\t%6\t123\t'+record['fixture']+'\n'
with patch.object(m,'run',return_value=response(out=wrong)) as called:
    rejects(lambda:m.clean_session(record,root));assert called.call_count==1
wrong='shiki-sess_owned\t$4\t%7\t123\t'+record['fixture']+'\n'
with patch.object(m,'run',return_value=response(out=wrong)) as called:
    rejects(lambda:m.clean_session(record,root));assert called.call_count==1
match='shiki-sess_owned\t$4\t%6\t123\t/bin/sh -c exec '+record['fixture']+'\n'
with patch.object(m,'run',side_effect=[response(out=match),response()]) as called:
    m.clean_session(record,root)
    assert called.call_args_list[-1].args[0][-3:]==['kill-session','-t','$4']
with patch.object(m,'run',return_value=response(out=json.dumps({'processes':[{'state':'identity-mismatch'}]}))):
    rejects(lambda:m.guard(root,root/'inventory'))
with tempfile.TemporaryDirectory(prefix='shiki-launchd.',dir='/private/tmp') as directory:
    root=pathlib.Path(directory);(root/'runtime').mkdir()
    (root/'runtime/activity-owned.json').write_text(json.dumps({'sessions':[record],'processes':[{'pid':123,'executable':str(root/'runtime/shiki-daemon')}]}))
    with patch.object(m.sys,'argv',['cleanup',str(root)]),patch.object(m,'clean_job',side_effect=RuntimeError('GUI inaccessible')),patch.object(m,'clean_session') as session,patch.object(m,'guard',return_value=[{'state':'absent'}]) as guard:
        rejects(m.main);assert session.call_count==1;assert guard.call_count>=1
print('cleanup guards: unreadable GUI, exact absence, foreign session, changed pane, exact target, PID mismatch, independent failure cleanup passed')
`
	command := exec.CommandContext(t.Context(), "python3", "-c", script, helper)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cleanup guard behavior: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}
