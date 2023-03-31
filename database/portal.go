package database

import (
	"database/sql"
	"fmt"

	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"
)

// language=postgresql
const (
	portalSelect = `
		SELECT account_id, chat_id, mxid,
		       plain_name, name, name_set, topic, topic_set, avatar, avatar_url, avatar_set,
		       encrypted
		FROM portal
	`
)

type ChatID uint64

type PortalID struct {
	AccountID AccountID
	ChatID    ChatID
}

func (pid PortalID) String() string {
	return fmt.Sprintf("%d-%d", pid.AccountID, pid.ChatID)
}

type PortalQuery struct {
	db  *Database
	log log.Logger
}

func (pq *PortalQuery) New() *Portal {
	return &Portal{
		db:  pq.db,
		log: pq.log,
	}
}

func (pq *PortalQuery) GetAll() []*Portal {
	return pq.getAll(portalSelect)
}

func (pq *PortalQuery) Get(portalID PortalID) *Portal {
	return pq.get(portalSelect+" WHERE account_id=$1 AND chat_id=$2", portalID.AccountID, portalID.ChatID)
}

func (pq *PortalQuery) GetByMXID(mxid id.RoomID) *Portal {
	return pq.get(portalSelect+" WHERE mxid=$1", mxid)
}

func (pq *PortalQuery) getAll(query string, args ...interface{}) []*Portal {
	rows, err := pq.db.Query(query, args...)
	if err != nil || rows == nil {
		return nil
	}
	defer rows.Close()

	var portals []*Portal
	for rows.Next() {
		portals = append(portals, pq.New().Scan(rows))
	}

	return portals
}

func (pq *PortalQuery) get(query string, args ...interface{}) *Portal {
	return pq.New().Scan(pq.db.QueryRow(query, args...))
}

type Portal struct {
	db  *Database
	log log.Logger

	AccountID AccountID
	ChatID    ChatID
	MXID      id.RoomID

	PlainName string
	Name      string
	NameSet   bool
	Topic     string
	TopicSet  bool
	Avatar    string
	AvatarURL id.ContentURI
	AvatarSet bool
	Encrypted bool
}

func (p *Portal) ID() PortalID {
	return PortalID{
		AccountID: p.AccountID,
		ChatID:    p.ChatID,
	}
}

func (p *Portal) Scan(row dbutil.Scannable) *Portal {
	var avatarURL string

	err := row.Scan(&p.AccountID, &p.ChatID, &p.MXID, &p.PlainName, &p.Name, &p.NameSet, &p.Topic, &p.TopicSet, &p.Avatar, &avatarURL, &p.AvatarSet,
		&p.Encrypted)

	if err != nil {
		if err != sql.ErrNoRows {
			p.log.Errorln("Database scan failed:", err)
			panic(err)
		}

		return nil
	}

	p.AvatarURL, _ = id.ParseContentURI(avatarURL)

	return p
}

func (p *Portal) Upsert() error {
	query := `
		INSERT INTO portal (account_id, chat_id, mxid,
		                    plain_name, name, name_set, topic, topic_set, avatar, avatar_url, avatar_set,
		                    encrypted)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (account_id, chat_id) DO UPDATE
		SET mxid=EXCLUDED.mxid,
			plain_name=EXCLUDED.plain_name, name=EXCLUDED.name, name_set=EXCLUDED.name_set, topic=EXCLUDED.topic, topic_set=EXCLUDED.topic_set, avatar=EXCLUDED.avatar, avatar_url=EXCLUDED.avatar_url, avatar_set=EXCLUDED.avatar_set,
			encrypted=EXCLUDED.encrypted
		ON CONFLICT (mxid) DO UPDATE
		SET
			plain_name=EXCLUDED.plain_name, name=EXCLUDED.name, name_set=EXCLUDED.name_set, topic=EXCLUDED.topic, topic_set=EXCLUDED.topic_set, avatar=EXCLUDED.avatar, avatar_url=EXCLUDED.avatar_url, avatar_set=EXCLUDED.avatar_set,
			encrypted=EXCLUDED.encrypted
	`
	_, err := p.db.Exec(query,
		p.AccountID,
		p.ChatID,
		p.MXID,
		p.PlainName, p.Name, p.NameSet, p.Topic, p.TopicSet, p.Avatar, p.AvatarURL.String(), p.AvatarSet,
		p.Encrypted)
	return err
}

func (p *Portal) Delete() {
	query := "DELETE FROM portal WHERE account_id=$1 AND chat_id=$2"
	_, err := p.db.Exec(query, p.AccountID, p.ChatID)
	if err != nil {
		p.log.Warnfln("Failed to delete %s: %v", p.ID(), err)
		panic(err)
	}
}
