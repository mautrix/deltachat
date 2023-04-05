package database

import (
	"database/sql"
	"fmt"

	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"
)

const (
	puppetSelect = "SELECT account_id, contact_id, name_override, name, name_set, avatar, avatar_url, avatar_set," +
		" custom_mxid, access_token, next_batch" +
		" FROM puppet "
)

type ContactID uint64

type PuppetID struct {
	AccountID    AccountID
	ContactID    ContactID
	NameOverride string
}

func (p PuppetID) String() string {
	if p.NameOverride != "" {
		return fmt.Sprintf("%d_%d_%s", p.AccountID, p.ContactID, p.NameOverride)
	}

	return fmt.Sprintf("%d_%d", p.AccountID, p.ContactID)
}

type PuppetQuery struct {
	db  *Database
	log log.Logger
}

func (pq *PuppetQuery) New() *Puppet {
	return &Puppet{
		db:  pq.db,
		log: pq.log,
	}
}

func (pq *PuppetQuery) Get(puppetID PuppetID) *Puppet {
	return pq.get(puppetSelect+" WHERE account_id=$1 AND contact_id=$2 AND name_override=$3", puppetID.AccountID, puppetID.ContactID, puppetID.NameOverride)
}

func (pq *PuppetQuery) GetByCustomMXID(mxid id.UserID) *Puppet {
	return pq.get(puppetSelect+" WHERE custom_mxid=$1", mxid)
}

func (pq *PuppetQuery) get(query string, args ...interface{}) *Puppet {
	return pq.New().Scan(pq.db.QueryRow(query, args...))
}

func (pq *PuppetQuery) GetAll() []*Puppet {
	return pq.getAll(puppetSelect)
}

func (pq *PuppetQuery) GetAllWithCustomMXID() []*Puppet {
	return pq.getAll(puppetSelect + " WHERE custom_mxid<>''")
}

func (pq *PuppetQuery) getAll(query string, args ...interface{}) []*Puppet {
	rows, err := pq.db.Query(query, args...)
	if err != nil || rows == nil {
		return nil
	}
	defer rows.Close()

	var puppets []*Puppet
	for rows.Next() {
		puppets = append(puppets, pq.New().Scan(rows))
	}

	return puppets
}

type Puppet struct {
	db  *Database
	log log.Logger

	AccountID    AccountID
	ContactID    ContactID
	NameOverride string

	Name      string
	NameSet   bool
	Avatar    string
	AvatarURL id.ContentURI
	AvatarSet bool

	CustomMXID  id.UserID
	AccessToken string
	NextBatch   string
}

func (p *Puppet) ID() PuppetID {
	return PuppetID{
		AccountID:    p.AccountID,
		ContactID:    p.ContactID,
		NameOverride: p.NameOverride,
	}
}

func (p *Puppet) Scan(row dbutil.Scannable) *Puppet {
	var avatarURL string
	var customMXID, accessToken, nextBatch sql.NullString

	err := row.Scan(&p.AccountID, &p.ContactID, &p.NameOverride, &p.Name, &p.NameSet, &p.Avatar, &avatarURL, &p.AvatarSet,
		&customMXID, &accessToken, &nextBatch)

	if err != nil {
		if err != sql.ErrNoRows {
			p.log.Errorln("Database scan failed:", err)
			panic(err)
		}

		return nil
	}

	p.AvatarURL, _ = id.ParseContentURI(avatarURL)
	p.CustomMXID = id.UserID(customMXID.String)
	p.AccessToken = accessToken.String
	p.NextBatch = nextBatch.String

	return p
}

func (p *Puppet) Upsert() error {
	query := `
		INSERT INTO puppet (account_id, contact_id, name_override, name, name_set, avatar, avatar_url, avatar_set, custom_mxid, access_token, next_batch)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT(account_id, contact_id, name_override) DO UPDATE SET
			name = EXCLUDED.name,
			name_set = EXCLUDED.name_set,
			avatar = EXCLUDED.avatar,
			avatar_url = EXCLUDED.avatar_url,
			avatar_set = EXCLUDED.avatar_set,
			custom_mxid = EXCLUDED.custom_mxid,
			access_token = EXCLUDED.access_token,
			next_batch = EXCLUDED.next_batch
	`
	_, err := p.db.Exec(query, p.AccountID, p.ContactID, p.NameOverride, p.Name, p.NameSet, p.Avatar, p.AvatarURL.String(), p.AvatarSet,
		strPtr(string(p.CustomMXID)), strPtr(p.AccessToken), strPtr(p.NextBatch))
	return err
}
