package supportchannels

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/useopendesk/opendesk/internal/auth"
	"github.com/useopendesk/opendesk/internal/db"
	"github.com/useopendesk/opendesk/internal/httpx"
)

const (
	roleAgent = "agent"
	roleAdmin = "admin"
	// maxAllowed bounds a destination's list of chosen people.
	maxAllowed = 100
	// noAccess is what a person without access is told in the thread.
	noAccess = "You can't use opendesk from this channel. Ask an admin for access."
)

// AccessInput is who may act from a destination's threads, in its create or update body.
type AccessInput struct {
	MinRole        *string   `json:"min_role,omitempty" enum:"agent,admin" doc:"The lowest role that may act on tickets from this channel's threads: agent (default) or admin. Linked people below it are told they have no access."`
	AllowedUserIDs *[]string `json:"allowed_user_ids,omitempty" maxItems:"100" doc:"When not empty, only these members (agents or admins) may act from this channel, and only if they also meet min_role. Replaces the list; empty lets everyone with the role."`
}

// apply validates the given values, sets them on d and reports the keys it changed.
func (in AccessInput) apply(ctx context.Context, q db.Querier, wid uuid.UUID, d *destination) ([]string, error) {
	var changed []string
	if in.MinRole != nil {
		if !oneOf(*in.MinRole, roleAgent, roleAdmin) {
			return nil, httpx.Validation("min_role must be agent or admin.")
		}
		if *in.MinRole != d.MinRole {
			d.MinRole = *in.MinRole
			changed = append(changed, "min_role")
		}
	}
	if in.AllowedUserIDs != nil {
		var list []uuid.UUID
		for _, raw := range *in.AllowedUserIDs {
			id, err := uuid.Parse(strings.TrimSpace(raw))
			if err != nil {
				return nil, httpx.Validation("allowed_user_ids holds member ids.")
			}
			if !slices.Contains(list, id) {
				list = append(list, id)
			}
		}
		if len(list) > maxAllowed {
			return nil, httpx.Validation("Choose at most 100 people.")
		}
		if len(list) > 0 {
			// Each must be a member who can work tickets: a viewer has no commands to allow.
			var n int
			if err := q.QueryRow(ctx, `SELECT count(*) FROM members WHERE workspace_id = $1 AND user_id = ANY($2) AND role IN ('owner', 'admin', 'agent')`, wid, list).Scan(&n); err != nil {
				return nil, err
			}
			if n != len(list) {
				return nil, httpx.Validation("Everyone allowed must be an agent or admin of this workspace.")
			}
		}
		if list == nil {
			list = []uuid.UUID{}
		}
		if !slices.Equal(list, d.AllowedUserIDs) {
			d.AllowedUserIDs = list
			changed = append(changed, "allowed_user_ids")
		}
	}
	return changed, nil
}

// mayAct reports whether p may act on tickets from the destination's threads: the destination's
// lowest role, and its list of chosen people when it has one (contracts/support-channels.md "Access").
func (s *Service) mayAct(ctx context.Context, destinationID uuid.UUID, p *auth.Principal) (bool, error) {
	var min string
	var allowed []uuid.UUID
	if err := s.d.DB.Pool.QueryRow(ctx, `SELECT min_role, allowed_user_ids FROM support_channel_destinations WHERE id = $1`, destinationID).Scan(&min, &allowed); err != nil {
		if db.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return mayActWith(min, allowed, p), nil
}

func mayActWith(min string, allowed []uuid.UUID, p *auth.Principal) bool {
	need := auth.RoleAgent
	if min == roleAdmin {
		need = auth.RoleAdmin
	}
	return p.Role.AtLeast(need) && (len(allowed) == 0 || slices.Contains(allowed, p.UserID))
}
