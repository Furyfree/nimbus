package cli

import (
	"errors"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

// approvedGreeterSource is used only after the system plan's approval and sudo
// acquisition. It permits one fixed read-only query for the approved account;
// preview, status and unrelated commands keep the ordinary source.
type approvedGreeterSource struct {
	native.Source
	user string
}

func (s approvedGreeterSource) Run(name string, args ...string) ([]byte, error) {
	if name == inspect.GreeterBinary && len(args) == 3 && args[0] == "passwordless-sync" && args[1] == "status" && args[2] == s.user {
		out, err := s.Source.Run("sudo", "--", inspect.GreeterBinary, "passwordless-sync", "status", s.user)
		if err != nil {
			err = errors.Join(inspect.ErrGreeterAdminStatus, err)
		}
		return out, err
	}
	return s.Source.Run(name, args...)
}
