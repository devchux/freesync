package managed

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"sort"

	"github.com/devchux/freesync/internal/database"
)

type driveAccess struct {
	ID       string `json:"id"`
	Owner    bool   `json:"owner"`
	Pending  bool   `json:"pending"`
	Writable bool   `json:"writable"`
}

type driveMember struct {
	Account
	Pending  bool `json:"pending"`
	Writable bool `json:"writable"`
}

// HandleDrives lists the signed-in account’s drives and invitations.
func (s *Server) HandleDrives(w http.ResponseWriter, r *http.Request) {
	account, err := s.account(r)
	if err != nil {
		s.accountError(w, err)
		return
	}
	ctx := r.Context()
	queries := s.queries()
	owned, err := queries.ManagedOwnersByAccount(ctx, account.ID)
	if err != nil {
		s.respondError(w, err)
		return
	}
	grants, err := queries.ManagedGrantsByAccount(ctx, account.ID)
	if err != nil {
		s.respondError(w, err)
		return
	}
	list := make([]driveAccess, 0, len(owned)+len(grants))
	for _, row := range owned {
		list = append(list, driveAccess{ID: row.Drive, Owner: true, Writable: true})
	}
	for _, row := range grants {
		list = append(list, driveAccess{ID: row.Drive, Pending: row.Pending != 0, Writable: row.Writable != 0})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createDrive(ctx context.Context, drive, accountID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := database.NewManagedQueries(tx, s.DatabaseType)
	_, err = queries.ManagedDriveByID(ctx, drive)
	if err == nil {
		return fs.ErrExist
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := queries.InsertManagedDrive(ctx, drive); err != nil {
		return err
	}
	if err := queries.InsertManagedOwner(ctx, drive, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) claim(ctx context.Context, drive, email string) error {
	queries := s.queries()
	if _, err := queries.ManagedDriveByID(ctx, drive); errors.Is(err, sql.ErrNoRows) {
		return fs.ErrInvalid
	} else if err != nil {
		return err
	}
	_, err := queries.ManagedDriveOwner(ctx, drive)
	if err == nil {
		return fs.ErrExist
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	accountID, err := s.accountID(ctx, email)
	if err != nil {
		return err
	}
	return queries.InsertManagedOwner(ctx, drive, accountID)
}

func (s *Server) accountID(ctx context.Context, rawEmail string) (string, error) {
	email, valid := emailAddress(rawEmail)
	if !valid {
		return "", fs.ErrInvalid
	}
	row, err := s.queries().ManagedAccountByEmail(ctx, email)
	return row.ID, err
}

func (s *Server) manageMembers(w http.ResponseWriter, r *http.Request, drive, ownerID string) error {
	ctx := r.Context()
	action := r.PathValue("action")
	queries := s.queries()
	if action == "members" && r.Method == http.MethodGet {
		rows, err := queries.ManagedDriveMembers(ctx, drive)
		if err != nil {
			return err
		}
		members := make([]driveMember, 0, len(rows))
		for _, row := range rows {
			members = append(members, driveMember{
				Account: Account{ID: row.ID, Email: row.Email, FullName: row.FullName},
				Pending: row.Pending != 0, Writable: row.Writable != 0,
			})
		}
		writeJSON(w, http.StatusOK, members)
		return nil
	}
	if r.Method != http.MethodPost || (action != "invite" && action != "grant" && action != "revoke") {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
	id, err := s.accountID(ctx, r.URL.Query().Get("email"))
	if err != nil {
		return err
	}
	if id == ownerID {
		return fs.ErrInvalid
	}
	if action == "revoke" {
		return queries.DeleteManagedGrant(ctx, drive, id)
	}
	writable := permission(r.URL.Query().Get("write") == "true")
	if action == "grant" {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		q := database.NewManagedQueries(tx, s.DatabaseType)
		if err := q.DeleteManagedGrant(ctx, drive, id); err != nil {
			return err
		}
		if err := q.InsertManagedGrant(ctx, drive, id, 0, writable); err != nil {
			return err
		}
		return tx.Commit()
	}
	_, err = queries.ManagedGrantByAccount(ctx, drive, id)
	if err == nil {
		return queries.UpdateManagedGrantAccess(ctx, writable, drive, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return queries.InsertManagedGrant(ctx, drive, id, 1, writable)
}

func (s *Server) accept(ctx context.Context, drive, accountID string) error {
	count, err := s.queries().AcceptManagedGrant(ctx, drive, accountID)
	if err != nil {
		return err
	}
	if count == 0 {
		return fs.ErrPermission
	}
	return nil
}
