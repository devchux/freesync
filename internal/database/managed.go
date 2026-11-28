package database

import (
	"context"

	"github.com/devchux/freesync/internal/database/mysql/mysqlsql"
	"github.com/devchux/freesync/internal/database/postgres/pgsql"
	"github.com/devchux/freesync/internal/database/sqlite/dbsql"
)

// ManagedQueries normalizes the types returned by the generated database queries.
type ManagedQueries struct {
	dialect string
	db      dbsql.DBTX
}

// NewManagedQueries accepts a database connection or an active transaction.
func NewManagedQueries(db dbsql.DBTX, dialect string) ManagedQueries {
	return ManagedQueries{db: db, dialect: dialect}
}

type ManagedAccount = dbsql.ManagedAccount
type ManagedCommit = dbsql.ManagedCommit
type ManagedDriveMembersRow = dbsql.ManagedDriveMembersRow
type ManagedFile = dbsql.ManagedFile
type ManagedGrant = dbsql.ManagedGrant
type ManagedMember = dbsql.ManagedMember
type ManagedOwner = dbsql.ManagedOwner

func convertManagedRows[From, To any](rows []From, err error, convert func(From) To) ([]To, error) {
	if err != nil {
		return nil, err
	}
	result := make([]To, len(rows))
	for i, row := range rows {
		result[i] = convert(row)
	}
	return result, nil
}

func pgsqlManagedAccount(row pgsql.ManagedAccount) ManagedAccount {
	return ManagedAccount(row)
}
func pgsqlManagedCommit(row pgsql.ManagedCommit) ManagedCommit {
	return ManagedCommit(row)
}
func pgsqlManagedDriveMembersRow(row pgsql.ManagedDriveMembersRow) ManagedDriveMembersRow {
	return ManagedDriveMembersRow{
		ID:       row.ID,
		Email:    row.Email,
		FullName: row.FullName,
		Pending:  int64(row.Pending),
		Writable: int64(row.Writable),
	}
}
func pgsqlManagedFile(row pgsql.ManagedFile) ManagedFile {
	return ManagedFile(row)
}
func pgsqlManagedGrant(row pgsql.ManagedGrant) ManagedGrant {
	return ManagedGrant{
		Drive:     row.Drive,
		AccountID: row.AccountID,
		Pending:   int64(row.Pending),
		Writable:  int64(row.Writable),
	}
}
func pgsqlManagedMember(row pgsql.ManagedMember) ManagedMember {
	return ManagedMember{
		Drive:      row.Drive,
		Name:       row.Name,
		Credential: row.Credential,
		Invited:    int64(row.Invited),
		Writable:   int64(row.Writable),
	}
}
func pgsqlManagedOwner(row pgsql.ManagedOwner) ManagedOwner {
	return ManagedOwner(row)
}
func mysqlsqlManagedAccount(row mysqlsql.ManagedAccount) ManagedAccount {
	return ManagedAccount{
		ID:           string(row.ID),
		Email:        string(row.Email),
		FullName:     string(row.FullName),
		PasswordHash: string(row.PasswordHash),
	}
}
func mysqlsqlManagedCommit(row mysqlsql.ManagedCommit) ManagedCommit {
	return ManagedCommit{
		Drive:  string(row.Drive),
		Member: string(row.Member),
		ID:     string(row.ID),
		Path:   string(row.Path),
		Base:   string(row.Base),
	}
}
func mysqlsqlManagedDriveMembersRow(row mysqlsql.ManagedDriveMembersRow) ManagedDriveMembersRow {
	return ManagedDriveMembersRow{
		ID:       string(row.ID),
		Email:    string(row.Email),
		FullName: string(row.FullName),
		Pending:  int64(row.Pending),
		Writable: int64(row.Writable),
	}
}
func mysqlsqlManagedFile(row mysqlsql.ManagedFile) ManagedFile {
	return ManagedFile{
		Drive:    string(row.Drive),
		Path:     string(row.Path),
		Revision: string(row.Revision),
		Size:     row.Size,
		Modified: row.Modified,
	}
}
func mysqlsqlManagedGrant(row mysqlsql.ManagedGrant) ManagedGrant {
	return ManagedGrant{
		Drive:     string(row.Drive),
		AccountID: string(row.AccountID),
		Pending:   int64(row.Pending),
		Writable:  int64(row.Writable),
	}
}
func mysqlsqlManagedMember(row mysqlsql.ManagedMember) ManagedMember {
	return ManagedMember{
		Drive:      string(row.Drive),
		Name:       string(row.Name),
		Credential: string(row.Credential),
		Invited:    int64(row.Invited),
		Writable:   int64(row.Writable),
	}
}
func mysqlsqlManagedOwner(row mysqlsql.ManagedOwner) ManagedOwner {
	return ManagedOwner{
		Drive:     string(row.Drive),
		AccountID: string(row.AccountID),
	}
}
func (q ManagedQueries) AcceptManagedGrant(ctx context.Context, drive string, accountID string) (int64, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).AcceptManagedGrant(ctx, pgsql.AcceptManagedGrantParams{Drive: drive, AccountID: accountID})
	case MySQL:
		return mysqlsql.New(q.db).AcceptManagedGrant(ctx, mysqlsql.AcceptManagedGrantParams{Drive: []byte(drive), AccountID: []byte(accountID)})
	default:
		return dbsql.New(q.db).AcceptManagedGrant(ctx, dbsql.AcceptManagedGrantParams{Drive: drive, AccountID: accountID})
	}
}

func (q ManagedQueries) AllManagedAccounts(ctx context.Context) ([]ManagedAccount, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedAccounts(ctx)
		return convertManagedRows(rows, err, pgsqlManagedAccount)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedAccounts(ctx)
		return convertManagedRows(rows, err, mysqlsqlManagedAccount)
	default:
		return dbsql.New(q.db).AllManagedAccounts(ctx)
	}
}

func (q ManagedQueries) AllManagedCommits(ctx context.Context) ([]ManagedCommit, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedCommits(ctx)
		return convertManagedRows(rows, err, pgsqlManagedCommit)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedCommits(ctx)
		return convertManagedRows(rows, err, mysqlsqlManagedCommit)
	default:
		return dbsql.New(q.db).AllManagedCommits(ctx)
	}
}

func (q ManagedQueries) AllManagedDrives(ctx context.Context) ([]string, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).AllManagedDrives(ctx)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedDrives(ctx)
		return convertManagedRows(rows, err, func(value []byte) string { return string(value) })
	default:
		return dbsql.New(q.db).AllManagedDrives(ctx)
	}
}

func (q ManagedQueries) AllManagedFiles(ctx context.Context) ([]ManagedFile, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedFiles(ctx)
		return convertManagedRows(rows, err, pgsqlManagedFile)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedFiles(ctx)
		return convertManagedRows(rows, err, mysqlsqlManagedFile)
	default:
		return dbsql.New(q.db).AllManagedFiles(ctx)
	}
}

func (q ManagedQueries) AllManagedGrants(ctx context.Context) ([]ManagedGrant, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedGrants(ctx)
		return convertManagedRows(rows, err, pgsqlManagedGrant)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedGrants(ctx)
		return convertManagedRows(rows, err, mysqlsqlManagedGrant)
	default:
		return dbsql.New(q.db).AllManagedGrants(ctx)
	}
}

func (q ManagedQueries) AllManagedLegacyMembers(ctx context.Context) ([]ManagedMember, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedLegacyMembers(ctx)
		return convertManagedRows(rows, err, pgsqlManagedMember)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedLegacyMembers(ctx)
		return convertManagedRows(rows, err, mysqlsqlManagedMember)
	default:
		return dbsql.New(q.db).AllManagedLegacyMembers(ctx)
	}
}

func (q ManagedQueries) AllManagedOwners(ctx context.Context) ([]ManagedOwner, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedOwners(ctx)
		return convertManagedRows(rows, err, pgsqlManagedOwner)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedOwners(ctx)
		return convertManagedRows(rows, err, mysqlsqlManagedOwner)
	default:
		return dbsql.New(q.db).AllManagedOwners(ctx)
	}
}

func (q ManagedQueries) AnyManagedAccount(ctx context.Context) (string, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).AnyManagedAccount(ctx)
	case MySQL:
		row, err := mysqlsql.New(q.db).AnyManagedAccount(ctx)
		return string(row), err
	default:
		return dbsql.New(q.db).AnyManagedAccount(ctx)
	}
}

func (q ManagedQueries) AnyManagedDrive(ctx context.Context) (string, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).AnyManagedDrive(ctx)
	case MySQL:
		row, err := mysqlsql.New(q.db).AnyManagedDrive(ctx)
		return string(row), err
	default:
		return dbsql.New(q.db).AnyManagedDrive(ctx)
	}
}

func (q ManagedQueries) DeleteExpiredManagedSessions(ctx context.Context, expires int64) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).DeleteExpiredManagedSessions(ctx, expires)
	case MySQL:
		return mysqlsql.New(q.db).DeleteExpiredManagedSessions(ctx, expires)
	default:
		return dbsql.New(q.db).DeleteExpiredManagedSessions(ctx, expires)
	}
}

func (q ManagedQueries) DeleteManagedFile(ctx context.Context, drive string, path string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).DeleteManagedFile(ctx, pgsql.DeleteManagedFileParams{Drive: drive, Path: path})
	case MySQL:
		return mysqlsql.New(q.db).DeleteManagedFile(ctx, mysqlsql.DeleteManagedFileParams{Drive: []byte(drive), Path: []byte(path)})
	default:
		return dbsql.New(q.db).DeleteManagedFile(ctx, dbsql.DeleteManagedFileParams{Drive: drive, Path: path})
	}
}

func (q ManagedQueries) DeleteManagedGrant(ctx context.Context, drive string, accountID string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).DeleteManagedGrant(ctx, pgsql.DeleteManagedGrantParams{Drive: drive, AccountID: accountID})
	case MySQL:
		return mysqlsql.New(q.db).DeleteManagedGrant(ctx, mysqlsql.DeleteManagedGrantParams{Drive: []byte(drive), AccountID: []byte(accountID)})
	default:
		return dbsql.New(q.db).DeleteManagedGrant(ctx, dbsql.DeleteManagedGrantParams{Drive: drive, AccountID: accountID})
	}
}

func (q ManagedQueries) DeleteManagedSession(ctx context.Context, tokenHash string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).DeleteManagedSession(ctx, tokenHash)
	case MySQL:
		return mysqlsql.New(q.db).DeleteManagedSession(ctx, []byte(tokenHash))
	default:
		return dbsql.New(q.db).DeleteManagedSession(ctx, tokenHash)
	}
}

func (q ManagedQueries) InsertManagedAccount(ctx context.Context, id string, email string, fullName string, passwordHash string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedAccount(ctx, pgsql.InsertManagedAccountParams{ID: id, Email: email, FullName: fullName, PasswordHash: passwordHash})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedAccount(ctx, mysqlsql.InsertManagedAccountParams{ID: []byte(id), Email: []byte(email), FullName: []byte(fullName), PasswordHash: []byte(passwordHash)})
	default:
		return dbsql.New(q.db).InsertManagedAccount(ctx, dbsql.InsertManagedAccountParams{ID: id, Email: email, FullName: fullName, PasswordHash: passwordHash})
	}
}

func (q ManagedQueries) InsertManagedCommit(ctx context.Context, drive string, member string, id string, path string, base string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedCommit(ctx, pgsql.InsertManagedCommitParams{Drive: drive, Member: member, ID: id, Path: path, Base: base})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedCommit(ctx, mysqlsql.InsertManagedCommitParams{Drive: []byte(drive), Member: []byte(member), ID: []byte(id), Path: []byte(path), Base: []byte(base)})
	default:
		return dbsql.New(q.db).InsertManagedCommit(ctx, dbsql.InsertManagedCommitParams{Drive: drive, Member: member, ID: id, Path: path, Base: base})
	}
}

func (q ManagedQueries) InsertManagedDrive(ctx context.Context, id string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedDrive(ctx, id)
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedDrive(ctx, []byte(id))
	default:
		return dbsql.New(q.db).InsertManagedDrive(ctx, id)
	}
}

func (q ManagedQueries) InsertManagedFile(ctx context.Context, drive string, path string, revision string, size int64, modified int64) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedFile(ctx, pgsql.InsertManagedFileParams{Drive: drive, Path: path, Revision: revision, Size: size, Modified: modified})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedFile(ctx, mysqlsql.InsertManagedFileParams{Drive: []byte(drive), Path: []byte(path), Revision: []byte(revision), Size: size, Modified: modified})
	default:
		return dbsql.New(q.db).InsertManagedFile(ctx, dbsql.InsertManagedFileParams{Drive: drive, Path: path, Revision: revision, Size: size, Modified: modified})
	}
}

func (q ManagedQueries) InsertManagedGrant(ctx context.Context, drive string, accountID string, pending int64, writable int64) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedGrant(ctx, pgsql.InsertManagedGrantParams{Drive: drive, AccountID: accountID, Pending: int32(pending), Writable: int32(writable)})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedGrant(ctx, mysqlsql.InsertManagedGrantParams{Drive: []byte(drive), AccountID: []byte(accountID), Pending: int32(pending), Writable: int32(writable)})
	default:
		return dbsql.New(q.db).InsertManagedGrant(ctx, dbsql.InsertManagedGrantParams{Drive: drive, AccountID: accountID, Pending: pending, Writable: writable})
	}
}

func (q ManagedQueries) InsertManagedLegacyMember(ctx context.Context, drive string, name string, credential string, invited int64, writable int64) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedLegacyMember(ctx, pgsql.InsertManagedLegacyMemberParams{Drive: drive, Name: name, Credential: credential, Invited: int32(invited), Writable: int32(writable)})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedLegacyMember(ctx, mysqlsql.InsertManagedLegacyMemberParams{Drive: []byte(drive), Name: []byte(name), Credential: []byte(credential), Invited: int32(invited), Writable: int32(writable)})
	default:
		return dbsql.New(q.db).InsertManagedLegacyMember(ctx, dbsql.InsertManagedLegacyMemberParams{Drive: drive, Name: name, Credential: credential, Invited: invited, Writable: writable})
	}
}

func (q ManagedQueries) InsertManagedOwner(ctx context.Context, drive string, accountID string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedOwner(ctx, pgsql.InsertManagedOwnerParams{Drive: drive, AccountID: accountID})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedOwner(ctx, mysqlsql.InsertManagedOwnerParams{Drive: []byte(drive), AccountID: []byte(accountID)})
	default:
		return dbsql.New(q.db).InsertManagedOwner(ctx, dbsql.InsertManagedOwnerParams{Drive: drive, AccountID: accountID})
	}
}

func (q ManagedQueries) InsertManagedSession(ctx context.Context, tokenHash string, accountID string, expires int64) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedSession(ctx, pgsql.InsertManagedSessionParams{TokenHash: tokenHash, AccountID: accountID, Expires: expires})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedSession(ctx, mysqlsql.InsertManagedSessionParams{TokenHash: []byte(tokenHash), AccountID: []byte(accountID), Expires: expires})
	default:
		return dbsql.New(q.db).InsertManagedSession(ctx, dbsql.InsertManagedSessionParams{TokenHash: tokenHash, AccountID: accountID, Expires: expires})
	}
}

func (q ManagedQueries) ManagedAccountByEmail(ctx context.Context, email string) (ManagedAccount, error) {
	switch q.dialect {
	case PostgreSQL:
		row, err := pgsql.New(q.db).ManagedAccountByEmail(ctx, email)
		return pgsqlManagedAccount(row), err
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedAccountByEmail(ctx, []byte(email))
		return mysqlsqlManagedAccount(row), err
	default:
		return dbsql.New(q.db).ManagedAccountByEmail(ctx, email)
	}
}

func (q ManagedQueries) ManagedAccountBySession(ctx context.Context, tokenHash string, expires int64) (ManagedAccount, error) {
	switch q.dialect {
	case PostgreSQL:
		row, err := pgsql.New(q.db).ManagedAccountBySession(ctx, pgsql.ManagedAccountBySessionParams{TokenHash: tokenHash, Expires: expires})
		return pgsqlManagedAccount(row), err
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedAccountBySession(ctx, mysqlsql.ManagedAccountBySessionParams{TokenHash: []byte(tokenHash), Expires: expires})
		return mysqlsqlManagedAccount(row), err
	default:
		return dbsql.New(q.db).ManagedAccountBySession(ctx, dbsql.ManagedAccountBySessionParams{TokenHash: tokenHash, Expires: expires})
	}
}

func (q ManagedQueries) ManagedCommitByID(ctx context.Context, drive string, member string, id string) (ManagedCommit, error) {
	switch q.dialect {
	case PostgreSQL:
		row, err := pgsql.New(q.db).ManagedCommitByID(ctx, pgsql.ManagedCommitByIDParams{Drive: drive, Member: member, ID: id})
		return pgsqlManagedCommit(row), err
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedCommitByID(ctx, mysqlsql.ManagedCommitByIDParams{Drive: []byte(drive), Member: []byte(member), ID: []byte(id)})
		return mysqlsqlManagedCommit(row), err
	default:
		return dbsql.New(q.db).ManagedCommitByID(ctx, dbsql.ManagedCommitByIDParams{Drive: drive, Member: member, ID: id})
	}
}

func (q ManagedQueries) ManagedDriveByID(ctx context.Context, id string) (string, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).ManagedDriveByID(ctx, id)
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedDriveByID(ctx, []byte(id))
		return string(row), err
	default:
		return dbsql.New(q.db).ManagedDriveByID(ctx, id)
	}
}

func (q ManagedQueries) ManagedDriveMembers(ctx context.Context, drive string) ([]ManagedDriveMembersRow, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).ManagedDriveMembers(ctx, drive)
		return convertManagedRows(rows, err, pgsqlManagedDriveMembersRow)
	case MySQL:
		rows, err := mysqlsql.New(q.db).ManagedDriveMembers(ctx, []byte(drive))
		return convertManagedRows(rows, err, mysqlsqlManagedDriveMembersRow)
	default:
		return dbsql.New(q.db).ManagedDriveMembers(ctx, drive)
	}
}

func (q ManagedQueries) ManagedDriveOwner(ctx context.Context, drive string) (string, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).ManagedDriveOwner(ctx, drive)
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedDriveOwner(ctx, []byte(drive))
		return string(row), err
	default:
		return dbsql.New(q.db).ManagedDriveOwner(ctx, drive)
	}
}

func (q ManagedQueries) ManagedFileByPath(ctx context.Context, drive string, path string) (ManagedFile, error) {
	switch q.dialect {
	case PostgreSQL:
		row, err := pgsql.New(q.db).ManagedFileByPath(ctx, pgsql.ManagedFileByPathParams{Drive: drive, Path: path})
		return pgsqlManagedFile(row), err
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedFileByPath(ctx, mysqlsql.ManagedFileByPathParams{Drive: []byte(drive), Path: []byte(path)})
		return mysqlsqlManagedFile(row), err
	default:
		return dbsql.New(q.db).ManagedFileByPath(ctx, dbsql.ManagedFileByPathParams{Drive: drive, Path: path})
	}
}

func (q ManagedQueries) ManagedFilesByPrefix(ctx context.Context, drive string, prefixLengthKey string, prefix string) ([]ManagedFile, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).ManagedFilesByPrefix(ctx, pgsql.ManagedFilesByPrefixParams{Drive: drive, PrefixLengthKey: prefixLengthKey, Prefix: prefix})
		return convertManagedRows(rows, err, pgsqlManagedFile)
	case MySQL:
		rows, err := mysqlsql.New(q.db).ManagedFilesByPrefix(ctx, mysqlsql.ManagedFilesByPrefixParams{Drive: []byte(drive), PrefixLengthKey: prefixLengthKey, Prefix: prefix})
		return convertManagedRows(rows, err, mysqlsqlManagedFile)
	default:
		return dbsql.New(q.db).ManagedFilesByPrefix(ctx, dbsql.ManagedFilesByPrefixParams{Drive: drive, PrefixLengthKey: prefixLengthKey, Prefix: prefix})
	}
}

func (q ManagedQueries) ManagedGrantByAccount(ctx context.Context, drive string, accountID string) (ManagedGrant, error) {
	switch q.dialect {
	case PostgreSQL:
		row, err := pgsql.New(q.db).ManagedGrantByAccount(ctx, pgsql.ManagedGrantByAccountParams{Drive: drive, AccountID: accountID})
		return pgsqlManagedGrant(row), err
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedGrantByAccount(ctx, mysqlsql.ManagedGrantByAccountParams{Drive: []byte(drive), AccountID: []byte(accountID)})
		return mysqlsqlManagedGrant(row), err
	default:
		return dbsql.New(q.db).ManagedGrantByAccount(ctx, dbsql.ManagedGrantByAccountParams{Drive: drive, AccountID: accountID})
	}
}

func (q ManagedQueries) ManagedGrantsByAccount(ctx context.Context, accountID string) ([]ManagedGrant, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).ManagedGrantsByAccount(ctx, accountID)
		return convertManagedRows(rows, err, pgsqlManagedGrant)
	case MySQL:
		rows, err := mysqlsql.New(q.db).ManagedGrantsByAccount(ctx, []byte(accountID))
		return convertManagedRows(rows, err, mysqlsqlManagedGrant)
	default:
		return dbsql.New(q.db).ManagedGrantsByAccount(ctx, accountID)
	}
}

func (q ManagedQueries) ManagedOwnersByAccount(ctx context.Context, accountID string) ([]ManagedOwner, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).ManagedOwnersByAccount(ctx, accountID)
		return convertManagedRows(rows, err, pgsqlManagedOwner)
	case MySQL:
		rows, err := mysqlsql.New(q.db).ManagedOwnersByAccount(ctx, []byte(accountID))
		return convertManagedRows(rows, err, mysqlsqlManagedOwner)
	default:
		return dbsql.New(q.db).ManagedOwnersByAccount(ctx, accountID)
	}
}

func (q ManagedQueries) ManagedPathCollision(ctx context.Context, drive string, descendantPrefix string, path string) (string, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).ManagedPathCollision(ctx, pgsql.ManagedPathCollisionParams{Drive: drive, DescendantPrefix: descendantPrefix, Path: path})
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedPathCollision(ctx, mysqlsql.ManagedPathCollisionParams{Drive: []byte(drive), DescendantPrefix: descendantPrefix, Path: path})
		return string(row), err
	default:
		return dbsql.New(q.db).ManagedPathCollision(ctx, dbsql.ManagedPathCollisionParams{Drive: drive, DescendantPrefix: descendantPrefix, Path: path})
	}
}

func (q ManagedQueries) UpdateManagedFile(ctx context.Context, revision string, size int64, modified int64, drive string, path string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).UpdateManagedFile(ctx, pgsql.UpdateManagedFileParams{Revision: revision, Size: size, Modified: modified, Drive: drive, Path: path})
	case MySQL:
		return mysqlsql.New(q.db).UpdateManagedFile(ctx, mysqlsql.UpdateManagedFileParams{Revision: []byte(revision), Size: size, Modified: modified, Drive: []byte(drive), Path: []byte(path)})
	default:
		return dbsql.New(q.db).UpdateManagedFile(ctx, dbsql.UpdateManagedFileParams{Revision: revision, Size: size, Modified: modified, Drive: drive, Path: path})
	}
}

func (q ManagedQueries) UpdateManagedGrantAccess(ctx context.Context, writable int64, drive string, accountID string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).UpdateManagedGrantAccess(ctx, pgsql.UpdateManagedGrantAccessParams{Writable: int32(writable), Drive: drive, AccountID: accountID})
	case MySQL:
		return mysqlsql.New(q.db).UpdateManagedGrantAccess(ctx, mysqlsql.UpdateManagedGrantAccessParams{Writable: int32(writable), Drive: []byte(drive), AccountID: []byte(accountID)})
	default:
		return dbsql.New(q.db).UpdateManagedGrantAccess(ctx, dbsql.UpdateManagedGrantAccessParams{Writable: writable, Drive: drive, AccountID: accountID})
	}
}
