package database

import (
	"database/sql"

	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"
)

type UserQuery struct {
	db  *Database
	log log.Logger
}

func (uq *UserQuery) New() *User {
	return &User{
		db:  uq.db,
		log: uq.log,
	}
}

const userSelect = `SELECT mxid, account_id, management_room FROM "user" WHERE`

func (uq *UserQuery) GetByMXID(userID id.UserID) *User {
	query := `SELECT mxid, account_id, management_room FROM "user" WHERE mxid=$1`
	return uq.New().Scan(uq.db.QueryRow(query, userID))
}

func (uq *UserQuery) GetByAccountID(id AccountID) *User {
	query := `SELECT mxid, account_id, management_room FROM "user" WHERE account_id=$1`
	return uq.New().Scan(uq.db.QueryRow(query, id))
}

func (uq *UserQuery) GetAll() []*User {
	return uq.getAll(userSelect)
}

type AccountID uint64

type User struct {
	db  *Database
	log log.Logger

	MXID           id.UserID
	AccountID      *AccountID
	ManagementRoom id.RoomID
}

func (uq *UserQuery) getAll(query string, args ...interface{}) []*User {
	rows, err := uq.db.Query(query, args...)
	if err != nil || rows == nil {
		return nil
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		users = append(users, uq.New().Scan(rows))
	}

	return users
}

func (u *User) Scan(row dbutil.Scannable) *User {
	err := row.Scan(&u.MXID, &u.AccountID, &u.ManagementRoom)
	if err != nil {
		if err != sql.ErrNoRows {
			u.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}
	return u
}

func (u *User) Insert() {
	query := `INSERT INTO "user" (mxid, account_id, management_room) VALUES ($1, $2, $3)`
	_, err := u.db.Exec(query, u.MXID, u.AccountID, u.ManagementRoom)
	if err != nil {
		u.log.Warnfln("Failed to insert %s: %v", u.MXID, err)
		panic(err)
	}
}

func (u *User) Update() {
	query := `UPDATE "user" SET account_id=$1, management_room=$2 WHERE mxid=$3`
	_, err := u.db.Exec(query, u.AccountID, u.ManagementRoom, u.MXID)
	if err != nil {
		u.log.Warnfln("Failed to update %q: %v", u.MXID, err)
		panic(err)
	}
}
