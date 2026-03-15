package db

import (
	"Remainwith/config"
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
)

type Group struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	IsPrivate  bool   `json:"is_private"`
	InviteCode string `json:"invite_code,omitempty"`
	CreatorID  int    `json:"creator_id"`
}

type GroupMember struct {
	GroupID  int    `json:"group_id"`
	UserID   int    `json:"user_id"`
	Role     string `json:"role"` // 'admin', 'member'
	UserName string `json:"user_name,omitempty"`
}

// InitGroupTables creates the necessary tables for groups and members.
func InitGroupTables(ctx context.Context) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS chat_groups (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			is_private BOOLEAN DEFAULT FALSE,
			invite_code TEXT UNIQUE,
			creator_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS group_members (
			group_id INT NOT NULL REFERENCES chat_groups(id) ON DELETE CASCADE,
			user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role TEXT DEFAULT 'member',
			joined_at TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (group_id, user_id)
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create group tables: %w", err)
	}
	return nil
}

// CreateGroup creates a new group and adds the creator as an admin.
func CreateGroup(ctx context.Context, name string, isPrivate bool, creatorID int) (*Group, error) {
	var inviteCode string
	if isPrivate {
		var err error
		inviteCode, err = GenerateInviteCode()
		if err != nil {
			return nil, err
		}
	}

	tx, err := config.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var g Group
	err = tx.QueryRow(ctx, `
		INSERT INTO chat_groups (name, is_private, invite_code, creator_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, is_private, invite_code, creator_id
	`, name, isPrivate, inviteCode, creatorID).Scan(&g.ID, &g.Name, &g.IsPrivate, &g.InviteCode, &g.CreatorID)
	if err != nil {
		return nil, err
	}

	// Add creator as admin
	_, err = tx.Exec(ctx, `
		INSERT INTO group_members (group_id, user_id, role)
		VALUES ($1, $2, 'admin')
	`, g.ID, creatorID)
	if err != nil {
		return nil, err
	}

	return &g, tx.Commit(ctx)
}

// GetUserGroups retrieves all groups a user belongs to.
func GetUserGroups(ctx context.Context, userID int) ([]Group, error) {
	rows, err := config.DB.Query(ctx, `
		SELECT g.id, g.name, g.is_private, g.invite_code, g.creator_id
		FROM chat_groups g
		JOIN group_members gm ON g.id = gm.group_id
		WHERE gm.user_id = $1
		ORDER BY g.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.IsPrivate, &g.InviteCode, &g.CreatorID); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// GetPublicGroups retrieves all public groups.
func GetPublicGroups(ctx context.Context) ([]Group, error) {
	rows, err := config.DB.Query(ctx, `
		SELECT id, name, is_private, invite_code, creator_id
		FROM chat_groups
		WHERE is_private = FALSE
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.IsPrivate, &g.InviteCode, &g.CreatorID); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// JoinGroup adds a user to a group.
func JoinGroup(ctx context.Context, groupID, userID int) error {
	_, err := config.DB.Exec(ctx, `
		INSERT INTO group_members (group_id, user_id, role)
		VALUES ($1, $2, 'member')
		ON CONFLICT (group_id, user_id) DO NOTHING
	`, groupID, userID)
	return err
}

// JoinGroupByCode allows a user to join a private group using an invite code.
func JoinGroupByCode(ctx context.Context, code string, userID int) (*Group, error) {
	var g Group
	err := config.DB.QueryRow(ctx, `
		SELECT id, name, is_private, invite_code, creator_id
		FROM chat_groups
		WHERE invite_code = $1
	`, code).Scan(&g.ID, &g.Name, &g.IsPrivate, &g.InviteCode, &g.CreatorID)
	if err != nil {
		return nil, err
	}

	err = JoinGroup(ctx, g.ID, userID)
	if err != nil {
		return nil, err
	}

	return &g, nil
}

// RemoveMember allows an admin to remove a member from a group.
func RemoveMember(ctx context.Context, groupID, adminUserID, targetUserID int) error {
	isAdmin, err := IsGroupAdmin(ctx, groupID, adminUserID)
	if err != nil {
		return err
	}
	if !isAdmin {
		return fmt.Errorf("only admins can remove members")
	}

	_, err = config.DB.Exec(ctx, `
		DELETE FROM group_members
		WHERE group_id = $1 AND user_id = $2
	`, groupID, targetUserID)
	return err
}

// GetGroupMembers retrieves all members of a group.
func GetGroupMembers(ctx context.Context, groupID int) ([]GroupMember, error) {
	rows, err := config.DB.Query(ctx, `
		SELECT gm.group_id, gm.user_id, gm.role, u.name
		FROM group_members gm
		JOIN users u ON gm.user_id = u.id
		WHERE gm.group_id = $1
		ORDER BY gm.role DESC, gm.joined_at ASC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.GroupID, &m.UserID, &m.Role, &m.UserName); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, nil
}

// RegenerateInviteCode creates a new invite code for a group.
func RegenerateInviteCode(ctx context.Context, groupID, adminUserID int) (string, error) {
	isAdmin, err := IsGroupAdmin(ctx, groupID, adminUserID)
	if err != nil {
		return "", err
	}
	if !isAdmin {
		return "", fmt.Errorf("only admins can regenerate invite codes")
	}

	newCode, err := GenerateInviteCode()
	if err != nil {
		return "", err
	}

	_, err = config.DB.Exec(ctx, `
		UPDATE chat_groups
		SET invite_code = $1
		WHERE id = $2
	`, newCode, groupID)
	if err != nil {
		return "", err
	}

	return newCode, nil
}

// IsGroupAdmin checks if a user is an admin of a specific group.
func IsGroupAdmin(ctx context.Context, groupID, userID int) (bool, error) {
	var role string
	err := config.DB.QueryRow(ctx, `
		SELECT role FROM group_members
		WHERE group_id = $1 AND user_id = $2
	`, groupID, userID).Scan(&role)
	if err != nil {
		return false, nil
	}
	return role == "admin", nil
}

// DeleteGroup removes a group and all its members.
func DeleteGroup(ctx context.Context, groupID, adminUserID int) error {
	isAdmin, err := IsGroupAdmin(ctx, groupID, adminUserID)
	if err != nil {
		return err
	}
	if !isAdmin {
		return fmt.Errorf("only admins can delete groups")
	}

	_, err = config.DB.Exec(ctx, `DELETE FROM chat_groups WHERE id = $1`, groupID)
	return err
}

// LeaveGroup allows a user to leave a group.
func LeaveGroup(ctx context.Context, groupID, userID int) error {
	_, err := config.DB.Exec(ctx, `
		DELETE FROM group_members
		WHERE group_id = $1 AND user_id = $2
	`, groupID, userID)
	return err
}

// GenerateInviteCode creates a random 6-digit alphanumeric code.
func GenerateInviteCode() (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Excluded O, 0, I, 1 for clarity
	code := make([]byte, 6)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		code[i] = charset[n.Int64()]
	}
	return string(code), nil
}
