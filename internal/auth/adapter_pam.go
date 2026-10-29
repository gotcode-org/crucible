// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"bytes"
	"context"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
)

// DefaultUnixChkpwdPath specifies the canonical Linux PAM validation helper.
const DefaultUnixChkpwdPath = "/sbin/unix_chkpwd" // #nosec G101

var validUsernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// DefaultAdminGroups defines standard POSIX groups mapped to the Admin role.
var DefaultAdminGroups = []string{
	"root",
	"wheel",
	"sudo",
	"adm",
	"crucible-admin",
}

// DefaultOperatorGroups defines standard POSIX groups mapped to Operator role.
var DefaultOperatorGroups = []string{
	"operator",
	"crucible-operator",
}

// PAMConfig defines configuration options for PAM authentication and RBAC
// group resolution.
type PAMConfig struct {
	HelperPath     string
	AdminGroups    []string
	OperatorGroups []string
}

// PAMAuthenticator implements the Authenticator port using the standard Linux
// PAM helper (/sbin/unix_chkpwd) with strictly zero Cgo (CGO_ENABLED=0).
type PAMAuthenticator struct {
	chkpwdPath     string
	adminGroups    []string
	operatorGroups []string
}

// NewPAMAuthenticator constructs a PAMAuthenticator using the given
// configuration. Empty paths or group slices are populated with defaults.
func NewPAMAuthenticator(cfg PAMConfig) *PAMAuthenticator {
	helper := cfg.HelperPath
	if helper == "" {
		helper = DefaultUnixChkpwdPath
	}
	admins := cfg.AdminGroups
	if len(admins) == 0 {
		admins = DefaultAdminGroups
	}
	ops := cfg.OperatorGroups
	if len(ops) == 0 {
		ops = DefaultOperatorGroups
	}
	return &PAMAuthenticator{
		chkpwdPath:     helper,
		adminGroups:    admins,
		operatorGroups: ops,
	}
}

// Provider returns "pam".
func (p *PAMAuthenticator) Provider() string {
	return "pam"
}

// Authenticate verifies Linux credentials against PAM via /sbin/unix_chkpwd.
func (p *PAMAuthenticator) Authenticate(
	ctx context.Context,
	username, password string,
) (*UserIdentity, error) {
	username = strings.TrimSpace(username)
	if username == "" || !validUsernameRe.MatchString(username) {
		return nil, ErrInvalidCredentials
	}

	// unix_chkpwd expects username as argument 1 and option flag ("nonull")
	// as argument 2, followed by the password on stdin terminated by \x00.
	// #nosec G204 -- helper path and sanitized username executed directly
	cmd := exec.CommandContext(ctx, p.chkpwdPath, username, "nonull")
	stdinPayload := append([]byte(password), 0x00)
	cmd.Stdin = bytes.NewReader(stdinPayload)

	if err := cmd.Run(); err != nil {
		return nil, ErrInvalidCredentials
	}

	role, groups := p.resolveLinuxIdentity(username)
	return &UserIdentity{
		Username: username,
		Role:     role,
		Provider: p.Provider(),
		Groups:   groups,
	}, nil
}

// ResolveRole resolves the POSIX group-mapped role for the given username.
func (p *PAMAuthenticator) ResolveRole(
	_ context.Context,
	username string,
) (Role, bool) {
	role, _ := p.resolveLinuxIdentity(username)
	if !role.IsValid() {
		return "", false
	}
	return role, true
}

func (p *PAMAuthenticator) resolveLinuxIdentity(
	username string,
) (Role, []string) {
	if username == "root" {
		return RoleAdmin, []string{"root"}
	}

	u, err := user.Lookup(username)
	if err != nil {
		return RoleViewer, nil
	}

	if u.Uid == "0" {
		return RoleAdmin, []string{"root"}
	}

	groupIDs, err := u.GroupIds()
	if err != nil {
		return RoleViewer, nil
	}

	var groupNames []string
	isAdmin := false
	isOperator := false

	adminSet := make(map[string]struct{}, len(p.adminGroups))
	for _, g := range p.adminGroups {
		adminSet[g] = struct{}{}
	}
	operatorSet := make(map[string]struct{}, len(p.operatorGroups))
	for _, g := range p.operatorGroups {
		operatorSet[g] = struct{}{}
	}

	for _, gid := range groupIDs {
		grp, gErr := user.LookupGroupId(gid)
		if gErr != nil {
			continue
		}
		groupNames = append(groupNames, grp.Name)
		if _, ok := adminSet[grp.Name]; ok {
			isAdmin = true
		} else if _, ok := operatorSet[grp.Name]; ok {
			isOperator = true
		}
	}

	if isAdmin {
		return RoleAdmin, groupNames
	}
	if isOperator {
		return RoleOperator, groupNames
	}
	return RoleViewer, groupNames
}
