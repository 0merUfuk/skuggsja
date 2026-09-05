package sqlitecopy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsTempNameAttempts = 100

// makePrivateTempDir atomically creates a directory whose protected DACL
// grants access only to the current user and LocalSystem. Child files inherit
// that DACL; os.Chmod is not a Windows privacy boundary.
func makePrivateTempDir(ctx context.Context) (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read current Windows user: %w", err)
	}
	userSID := user.User.Sid
	userSIDText := userSID.String()
	descriptor, err := windows.SecurityDescriptorFromString(
		fmt.Sprintf("D:P(A;OICI;FA;;;%s)(A;OICI;FA;;;SY)", userSIDText),
	)
	if err != nil {
		return "", fmt.Errorf("build private Windows DACL: %w", err)
	}
	attributes := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}

	for attempt := 0; attempt < windowsTempNameAttempts; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("generate private directory name: %w", err)
		}
		path := filepath.Join(tempParent(ctx), "skuggsja-sqlite-"+hex.EncodeToString(random[:]))
		pathUTF16, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return "", fmt.Errorf("encode private directory path: %w", err)
		}
		if err := windows.CreateDirectory(pathUTF16, attributes); err != nil {
			if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
				continue
			}
			return "", fmt.Errorf("create private Windows directory: %w", err)
		}
		if err := validatePrivateDACL(path, userSID); err != nil {
			return "", errors.Join(err, os.Remove(path))
		}
		return path, nil
	}
	return "", errors.New("could not allocate a unique private Windows directory")
}

func validatePrivateDACL(path string, userSID *windows.SID) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read private Windows DACL: %w", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return fmt.Errorf("read private Windows DACL control flags: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("private Windows DACL unexpectedly inherits permissions")
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read private Windows access list: %w", err)
	}
	if dacl == nil || defaulted || dacl.AceCount != 2 {
		return errors.New("private Windows access list is not the expected explicit two-entry DACL")
	}
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("create LocalSystem SID: %w", err)
	}
	requiredAccess := windows.ACCESS_MASK(
		windows.FILE_GENERIC_READ |
			windows.FILE_GENERIC_WRITE |
			windows.FILE_GENERIC_EXECUTE |
			windows.DELETE,
	)
	requiredInheritance := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	seenUser, seenSystem := false, false
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("read private Windows access entry: %w", err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
			ace.Header.AceFlags&requiredInheritance != requiredInheritance ||
			ace.Mask&requiredAccess != requiredAccess {
			return errors.New("private Windows access entry has unexpected type, inheritance, or permissions")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		matchesUser := sid.Equals(userSID)
		matchesSystem := sid.Equals(systemSID)
		if !matchesUser && !matchesSystem {
			return errors.New("private Windows access entry grants an unexpected principal")
		}
		seenUser = seenUser || matchesUser
		seenSystem = seenSystem || matchesSystem
	}
	if !seenUser || !seenSystem {
		return errors.New("private Windows access list omits the user or LocalSystem")
	}
	return nil
}
