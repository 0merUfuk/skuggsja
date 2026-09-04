package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// secureOutputDirectory creates or updates the product directory with a
// protected DACL granting access only to the current user and LocalSystem.
// Files created inside inherit that boundary; os.Chmod is not relied on.
func secureOutputDirectory(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read current Windows user: %w", err)
	}
	userSID := user.User.Sid
	descriptor, err := windows.SecurityDescriptorFromString(
		fmt.Sprintf("D:P(A;OICI;FA;;;%s)(A;OICI;FA;;;SY)", userSID.String()),
	)
	if err != nil {
		return fmt.Errorf("build private Windows DACL: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read constructed Windows DACL: %w", err)
	}

	info, statErr := os.Stat(path)
	switch {
	case statErr == nil && !info.IsDir():
		return errors.New("output directory path is not a directory")
	case statErr == nil:
		if err := windows.SetNamedSecurityInfo(
			path,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			nil,
			nil,
			dacl,
			nil,
		); err != nil {
			return fmt.Errorf("apply private Windows DACL: %w", err)
		}
	case errors.Is(statErr, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create output directory parent: %w", err)
		}
		attributes := &windows.SecurityAttributes{
			Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
			SecurityDescriptor: descriptor,
		}
		pathUTF16, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return fmt.Errorf("encode output directory path: %w", err)
		}
		if err := windows.CreateDirectory(pathUTF16, attributes); err != nil {
			if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
				return fmt.Errorf("create private Windows output directory: %w", err)
			}
			return secureOutputDirectory(path)
		}
	default:
		return fmt.Errorf("inspect output directory: %w", statErr)
	}
	return validateOutputDACL(path, userSID)
}

func validateOutputDACL(path string, userSID *windows.SID) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read output Windows DACL: %w", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return fmt.Errorf("read output Windows DACL control flags: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("output Windows DACL unexpectedly inherits permissions")
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read output Windows access list: %w", err)
	}
	if dacl == nil || defaulted || dacl.AceCount != 2 {
		return errors.New("output Windows access list is not the expected explicit two-entry DACL")
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
			return fmt.Errorf("read output Windows access entry: %w", err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
			ace.Header.AceFlags&requiredInheritance != requiredInheritance ||
			ace.Mask&requiredAccess != requiredAccess {
			return errors.New("output Windows access entry has unexpected type, inheritance, or permissions")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		matchesUser := sid.Equals(userSID)
		matchesSystem := sid.Equals(systemSID)
		if !matchesUser && !matchesSystem {
			return errors.New("output Windows access entry grants an unexpected principal")
		}
		seenUser = seenUser || matchesUser
		seenSystem = seenSystem || matchesSystem
	}
	if !seenUser || !seenSystem {
		return errors.New("output Windows access list omits the user or LocalSystem")
	}
	return nil
}
