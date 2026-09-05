package sqlitecopy

import (
	"context"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateTempDirectoryDACL(t *testing.T) {
	dir, err := makePrivateTempDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateDACL(dir, user.User.Sid); err != nil {
		t.Fatal(err)
	}
}
