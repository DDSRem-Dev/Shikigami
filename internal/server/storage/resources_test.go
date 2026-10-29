package storage

import (
	"encoding/json"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func resourceFixture(t *testing.T) *Snapshot {
	t.Helper()
	s := &Snapshot{SchemaVersion: 1, Buckets: map[string]map[string]json.RawMessage{}, Requests: map[string]RequestRecord{}}
	if err := s.Put(ProjectsBucket, "proj_a", &pb.Project{Id: "proj_a", Name: "app", Path: "/projects/app", User: "user", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(SessionsBucket, "sess_a", &pb.Session{Id: "sess_a", ProjectId: "proj_a", Name: "main", WorkingDir: "/projects/app/src", CreatedAt: 1, Lifecycle: "ready"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestResourceReferencesAndRuntimeFields(t *testing.T) {
	validate := ValidateDaemon("user")
	s := resourceFixture(t)
	if err := validate(s); err != nil {
		t.Fatal(err)
	}
	delete(s.Buckets[ProjectsBucket], "proj_a")
	if err := validate(s); err == nil {
		t.Fatal("orphan session accepted")
	}
	s = resourceFixture(t)
	var session pb.Session
	if err := s.Get(SessionsBucket, "sess_a", &session); err != nil {
		t.Fatal(err)
	}
	session.ClaudePid = 123
	if err := s.Put(SessionsBucket, "sess_a", &session); err != nil {
		t.Fatal(err)
	}
	if err := validate(s); err == nil {
		t.Fatal("stale PID accepted")
	}
	session.ClaudePid = 0
	session.WorkingDir = "/projects/app-other"
	if err := s.Put(SessionsBucket, "sess_a", &session); err != nil {
		t.Fatal(err)
	}
	if err := validate(s); err == nil {
		t.Fatal("directory prefix escape accepted")
	}
}

func TestSnapshotSurvivesSourceSessionButBlocksProjectDeletion(t *testing.T) {
	s := resourceFixture(t)
	snapshot := &pb.Snapshot{Id: "snap_a", SessionId: "sess_a", ProjectId: "proj_a", CreatedAt: 1, SchemaVersion: 1, WorkingDir: "src"}
	if err := s.Put(SnapshotsBucket, "snap_a", snapshot); err != nil {
		t.Fatal(err)
	}
	delete(s.Buckets[SessionsBucket], "sess_a")
	if err := ValidateDaemon("user")(s); err != nil {
		t.Fatal("source session deletion invalidated snapshot", err)
	}
	delete(s.Buckets[ProjectsBucket], "proj_a")
	if err := ValidateDaemon("user")(s); err == nil {
		t.Fatal("project deleted while referenced by snapshot")
	}
}

func TestProjectRootsCannotOverlap(t *testing.T) {
	s := resourceFixture(t)
	if err := s.Put(ProjectsBucket, "proj_b", &pb.Project{Id: "proj_b", Name: "nested", User: "user", Path: "/projects/app/nested", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDaemon("user")(s); err == nil {
		t.Fatal("nested project roots accepted")
	}
}

func TestCleanupDerivedSessionFieldsAreNeverPersistentAuthority(t *testing.T) {
	for _, change := range []func(*pb.Session){func(s *pb.Session) { s.CleanupStatus = "warned" }, func(s *pb.Session) { v := int64(10); s.CleanupWarningAt = &v }, func(s *pb.Session) { v := int64(20); s.CleanupAfter = &v }} {
		snapshot := resourceFixture(t)
		var s pb.Session
		if err := snapshot.Get(SessionsBucket, "sess_a", &s); err != nil {
			t.Fatal(err)
		}
		change(&s)
		if err := snapshot.Put(SessionsBucket, s.Id, &s); err != nil {
			t.Fatal(err)
		}
		if err := ValidateDaemon("user")(snapshot); err == nil {
			t.Fatal("derived cleanup field persisted")
		}
	}
}
