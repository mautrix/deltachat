package main

import (
	"regexp"
	"strconv"

	"github.com/deltachat/deltachat-rpc-client-go/deltachat"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-deltachat/database"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/id"
)

type Puppet struct {
	*database.Puppet

	bridge *DeltaChatBridge
	log    zerolog.Logger

	MXID id.UserID
}

func (puppet *Puppet) GetMXID() id.UserID {
	return puppet.MXID
}

func (br *DeltaChatBridge) NewPuppet(dbPuppet *database.Puppet) *Puppet {
	return &Puppet{
		Puppet: dbPuppet,
		bridge: br,
		log:    br.ZLog.With().Str("puppet_id", dbPuppet.ID().String()).Logger(),

		MXID: br.FormatPuppetMXID(dbPuppet.ID()),
	}
}

var userIDRegex *regexp.Regexp

func (br *DeltaChatBridge) ParsePuppetMXID(mxid id.UserID) (_ database.PuppetID, _ bool) {
	if userIDRegex == nil {
		userIDRegex = regexp.MustCompile(br.Config.Bridge.FormatUsername("([0-9]+)_([0-9]+)(_(.+))?"))
	}

	localpart, homeserver, err := mxid.ParseAndDecode()
	if err != nil || homeserver != br.Config.Homeserver.Domain {
		return
	}

	match := userIDRegex.FindStringSubmatch(localpart)
	if len(match) < 3 {
		return
	}

	accountID, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return
	}

	contactID, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil {
		return
	}

	var nameOverride string
	if len(match) > 4 {
		nameOverride = match[4]
	}

	return database.PuppetID{
		AccountID:    deltachat.AccountId(accountID),
		ContactID:    deltachat.ContactId(contactID),
		NameOverride: nameOverride,
	}, true
}

func (br *DeltaChatBridge) GetPuppetByMXID(mxid id.UserID) *Puppet {
	puppetID, ok := br.ParsePuppetMXID(mxid)
	if !ok {
		return nil
	}

	return br.GetPuppetByID(puppetID)
}

func (br *DeltaChatBridge) GetPuppetByID(puppetID database.PuppetID) *Puppet {
	br.puppetsLock.Lock()
	defer br.puppetsLock.Unlock()

	puppet, ok := br.puppets[puppetID]
	if !ok {
		dbPuppet := br.DB.Puppet.Get(puppetID)
		if dbPuppet == nil {
			dbPuppet = br.DB.Puppet.New()
			dbPuppet.AccountID = puppetID.AccountID
			dbPuppet.ContactID = puppetID.ContactID
			dbPuppet.NameOverride = puppetID.NameOverride
		}

		puppet = br.NewPuppet(dbPuppet)
		puppet.Update()
		br.puppets[puppetID] = puppet
	}

	return puppet
}

func (br *DeltaChatBridge) FormatPuppetMXID(puppetID database.PuppetID) id.UserID {
	return id.NewEncodedUserID(
		br.Config.Bridge.FormatUsername(puppetID.String()),
		br.Config.Homeserver.Domain,
	)
}

func (puppet *Puppet) DefaultIntent() *appservice.IntentAPI {
	return puppet.bridge.AS.Intent(puppet.MXID)
}

func (puppet *Puppet) CustomIntent() *appservice.IntentAPI {
	/*
		if puppet == nil {
			return nil
		}
		return puppet.customIntent
	*/
	return nil
}

func (puppet *Puppet) Update() error {
	user := puppet.bridge.GetUserByAccountID(puppet.AccountID)
	if user == nil {
		return ErrNotLoggedIn // FIXME
	}

	acct, err := user.Account()
	if err != nil {
		return err
	}

	contact := &deltachat.Contact{
		Account: acct,
		Id:      puppet.ContactID,
	}

	snap, err := contact.Snapshot()
	if err != nil {
		return err
	}

	intent := puppet.DefaultIntent()

	// quick hack to force name
	if puppet.NameOverride != "" {
		snap.NameAndAddr = "~" + puppet.NameOverride
	}

	updateName := puppet.Name != snap.NameAndAddr

	if updateName {
		puppet.Name = snap.NameAndAddr
		puppet.NameSet = true

		err = intent.SetDisplayName(puppet.Name)
		if err != nil {
			return err
		}
	}

	updateAvatar := puppet.Avatar != snap.ProfileImage
	if updateAvatar {
		puppet.Avatar = snap.ProfileImage
		puppet.AvatarSet = puppet.Avatar != ""

		if puppet.AvatarSet {
			puppet.AvatarURL, err = puppet.bridge.UploadBlob(puppet.Avatar)
			if err != nil {
				return err
			}
		}

		err = puppet.DefaultIntent().SetAvatarURL(puppet.AvatarURL)
		if err != nil {
			return err
		}
	}

	return puppet.Upsert()
}

func (puppet *Puppet) SwitchCustomMXID(accessToken string, mxid id.UserID) error {
	return nil
}
