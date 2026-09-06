package app_test

import (
	"errors"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/0merUfuk/skuggsja/internal/app"
	"golang.org/x/sys/windows"
)

func secureReleaseEvidenceFixture(path string) error {
	return app.SecureReleaseEvidenceFixture(path)
}

func releaseEvidencePrivacy(path string, directory bool) error {
	if directory {
		return app.ValidateReleaseEvidenceDirectory(path)
	}
	if err := app.ValidateReleaseEvidenceDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	// Evidence files must inherit only the current user and LocalSystem access
	// entries from the protected directory, with no additional explicit grants.
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil || defaulted || dacl.AceCount != 2 {
		return errors.New("evidence file must have exactly two inherited access entries")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	requiredAccess := windows.ACCESS_MASK(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE | windows.DELETE)
	seenUser, seenSystem := false, false
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERITED_ACE == 0 || ace.Mask&requiredAccess != requiredAccess {
			return errors.New("evidence file access entry lacks the expected inherited permissions")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		matchesUser, matchesSystem := sid.Equals(user.User.Sid), sid.Equals(system)
		if !matchesUser && !matchesSystem {
			return errors.New("evidence file grants an unexpected principal")
		}
		seenUser, seenSystem = seenUser || matchesUser, seenSystem || matchesSystem
	}
	if !seenUser || !seenSystem {
		return errors.New("evidence file omits the current user or LocalSystem")
	}
	return nil
}

func TestReleaseEvidencePrivacyRejectsBroadWindowsDACL(t *testing.T) {
	dir := t.TempDir()
	if err := secureReleaseEvidenceFixture(dir); err != nil {
		t.Fatal(err)
	}
	if err := releaseEvidencePrivacy(dir, true); err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if err := releaseEvidencePrivacy(dir, true); err == nil {
		t.Fatal("release evidence accepted a directory granting Everyone access")
	}
}
