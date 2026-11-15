//go:build linux

package runtime

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

type credentialACLEntry struct {
	tag, permission uint16
	id              uint32
}

func encodedCredentialACL(entries ...credentialACLEntry) []byte {
	data := make([]byte, 4+8*len(entries))
	binary.LittleEndian.PutUint32(data, 2)
	for index, entry := range entries {
		offset := 4 + 8*index
		binary.LittleEndian.PutUint16(data[offset:], entry.tag)
		binary.LittleEndian.PutUint16(data[offset+2:], entry.permission)
		binary.LittleEndian.PutUint32(data[offset+4:], entry.id)
	}
	return data
}

func TestCredentialACLPrivateSystemd255(t *testing.T) {
	const absent = ^uint32(0)
	acl := func(owner, named, group, mask, other uint16, uid uint32) []byte {
		return encodedCredentialACL(credentialACLEntry{1, owner, absent}, credentialACLEntry{2, named, uid}, credentialACLEntry{4, group, absent}, credentialACLEntry{16, mask, absent}, credentialACLEntry{32, other, absent})
	}
	for _, test := range []struct {
		name  string
		data  []byte
		mode  os.FileMode
		owner uint32
		valid bool
	}{
		{"actual-root-owned-directory-0550", acl(5, 5, 0, 5, 0, 1001), 0550, 0, true},
		{"actual-root-owned-file-0440", acl(4, 4, 0, 4, 0, 1001), 0440, 0, true},
		{"only-service-owner-and-named-root", acl(5, 5, 0, 5, 0, 0), 0550, 1001, true},
		{"private-minimal-acl", encodedCredentialACL(credentialACLEntry{1, 6, absent}, credentialACLEntry{4, 0, absent}, credentialACLEntry{32, 0, absent}), 0600, 0, true},
		{"owning-group-readable", acl(4, 4, 4, 4, 0, 1001), 0440, 0, false},
		{"world-readable", acl(4, 4, 0, 4, 4, 1001), 0444, 0, false},
		{"unexpected-named-user", acl(4, 4, 0, 4, 0, 1002), 0440, 0, false},
		{"unexpected-user-with-no-access", acl(4, 0, 0, 4, 0, 1002), 0440, 0, false},
		{"inconsistent-mode", acl(4, 4, 0, 4, 0, 1001), 0640, 0, false},
		{"duplicate-owner-entry", encodedCredentialACL(credentialACLEntry{1, 4, absent}, credentialACLEntry{1, 4, absent}, credentialACLEntry{4, 0, absent}, credentialACLEntry{32, 0, absent}), 0400, 0, false},
		{"duplicate-named-user", encodedCredentialACL(credentialACLEntry{1, 4, absent}, credentialACLEntry{2, 4, 1001}, credentialACLEntry{2, 4, 1001}, credentialACLEntry{4, 0, absent}, credentialACLEntry{16, 4, absent}, credentialACLEntry{32, 0, absent}), 0440, 0, false},
		{"named-owning-user", acl(4, 4, 0, 4, 0, 0), 0440, 0, false},
		{"named-group-even-with-no-access", encodedCredentialACL(credentialACLEntry{1, 4, absent}, credentialACLEntry{4, 0, absent}, credentialACLEntry{8, 0, 1001}, credentialACLEntry{16, 4, absent}, credentialACLEntry{32, 0, absent}), 0440, 0, false},
		{"named-user-without-mask", encodedCredentialACL(credentialACLEntry{1, 4, absent}, credentialACLEntry{2, 4, 1001}, credentialACLEntry{4, 0, absent}, credentialACLEntry{32, 0, absent}), 0400, 0, false},
		{"missing-owning-group", encodedCredentialACL(credentialACLEntry{1, 4, absent}, credentialACLEntry{16, 4, absent}, credentialACLEntry{32, 0, absent}), 0440, 0, false},
		{"noncanonical-order", encodedCredentialACL(credentialACLEntry{4, 0, absent}, credentialACLEntry{1, 4, absent}, credentialACLEntry{32, 0, absent}), 0400, 0, false},
		{"base-entry-with-named-id", encodedCredentialACL(credentialACLEntry{1, 4, 1001}, credentialACLEntry{4, 0, absent}, credentialACLEntry{32, 0, absent}), 0400, 0, false},
		{"unknown-permissions", acl(4, 8, 0, 4, 0, 1001), 0440, 0, false},
		{"unknown-format", []byte{3, 0, 0, 0}, 0400, 0, false},
		{"truncated-entry", []byte{2, 0, 0, 0, 1}, 0400, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := credentialACLPrivate(test.data, test.mode, test.owner, 1001); got != test.valid {
				t.Fatalf("credential ACL admission = %v, want %v", got, test.valid)
			}
		})
	}
}

func TestCredentialAccessDescriptorMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("test fixture, not a password"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	uid := uint32(os.Getuid())
	if err := checkCredentialAccess(file, uid, false); err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0640); err != nil {
		t.Fatal(err)
	}
	if err := checkCredentialAccess(file, uid, false); err == nil {
		t.Fatal("plain group-readable mode was accepted without a restrictive ACL")
	}
	if err := file.Chmod(0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, path+".hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := checkCredentialAccess(file, uid, false); err == nil {
		t.Fatal("multiply-linked credential was accepted")
	}
	if err := checkCredentialAccess(file, uid, true); err == nil {
		t.Fatal("regular credential accepted as a directory")
	}
}

func TestCredentialAccessDescriptorACL(t *testing.T) {
	const absent = ^uint32(0)
	uid, named := uint32(os.Getuid()), uint32(0)
	if uid == 0 {
		uid, named = 1001, 1001
	}
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("test fixture, not a password"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	set := func(name string, data []byte) {
		t.Helper()
		if err := unix.Fsetxattr(int(file.Fd()), name, data, 0); errors.Is(err, unix.ENOTSUP) {
			t.Skip("fixture filesystem does not support POSIX ACLs")
		} else if err != nil {
			t.Fatal(err)
		}
	}
	set("system.posix_acl_access", encodedCredentialACL(credentialACLEntry{1, 6, absent}, credentialACLEntry{2, 4, named}, credentialACLEntry{4, 0, absent}, credentialACLEntry{16, 4, absent}, credentialACLEntry{32, 0, absent}))
	if err := checkCredentialAccess(file, uid, false); err != nil {
		t.Fatal("systemd-style root/service-only ACL was refused:", err)
	}
	set("system.posix_acl_access", encodedCredentialACL(credentialACLEntry{1, 6, absent}, credentialACLEntry{2, 4, uid + 1}, credentialACLEntry{4, 0, absent}, credentialACLEntry{16, 4, absent}, credentialACLEntry{32, 0, absent}))
	if err := checkCredentialAccess(file, uid, false); err == nil {
		t.Fatal("ACL granting another user was accepted")
	}
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := unix.Fsetxattr(int(directory.Fd()), "system.posix_acl_default", encodedCredentialACL(credentialACLEntry{1, 7, absent}, credentialACLEntry{4, 0, absent}, credentialACLEntry{32, 0, absent}), 0); err != nil {
		t.Fatal(err)
	}
	if err := checkCredentialAccess(directory, uid, true); err == nil {
		t.Fatal("credential directory with a default ACL was accepted")
	}
}
