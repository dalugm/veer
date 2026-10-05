package privilege

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/windows"
)

func currentLogReader() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func validateLogReader(reader string) error {
	_, err := windows.StringToSid(reader)
	return err
}

func grantLogRead(ctx context.Context, file *os.File, reader string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sid, err := windows.StringToSid(reader)
	if err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(file.Name()))
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	sd, err := windows.GetSecurityInfo(
		windows.Handle(parent.Fd()),
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !windows.EqualSid(owner, sid) {
		return errors.New(
			"automatic log access requires a log directory owned by the originating user",
		)
	}
	name, err := windows.UTF16PtrFromString(file.Name())
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return err
	}
	target := os.NewFile(uintptr(handle), file.Name())
	defer func() { _ = target.Close() }()
	before, err := file.Stat()
	if err != nil {
		return err
	}
	after, err := target.Stat()
	if err != nil {
		return err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return errors.New("log destination changed while granting access")
	}
	sd, err = windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	// A null DACL already allows reading; replacing it would remove other rights.
	if dacl == nil {
		return nil
	}
	var pin runtime.Pinner
	pin.Pin(sid)
	defer pin.Unpin()
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_GENERIC_READ,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}, dacl)
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
}

func validateArchiveOwner(file *os.File, reader string) error {
	sid, err := windows.StringToSid(reader)
	if err != nil {
		return err
	}
	sd, err := windows.GetSecurityInfo(
		windows.Handle(file.Fd()),
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !windows.EqualSid(owner, sid) {
		return errors.New("session archive must be owned by the originating user")
	}
	return nil
}
