package app

import "golang.org/x/sys/windows"

// Test-only bridges let external release-verification tests use the existing
// Windows privacy implementation without adding a production API or changing it.
var SecureReleaseEvidenceFixture = secureOutputDirectory

func ValidateReleaseEvidenceDirectory(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	return validateOutputDACL(path, user.User.Sid)
}
