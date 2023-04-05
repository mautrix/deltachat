package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/deltachat/deltachat-rpc-client-go/deltachat"
	"github.com/rs/zerolog"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-deltachat/database"
)

var (
	ErrNotConnected = errors.New("not connected")
	ErrNotLoggedIn  = errors.New("not logged in")
)

type User struct {
	*database.User

	sync.Mutex

	bridge *DeltaChatBridge
	log    zerolog.Logger

	account       *deltachat.Account
	accountEvents <-chan *deltachat.Event

	contacts map[database.ContactID]*deltachat.Contact

	PermissionLevel bridgeconfig.PermissionLevel

	BridgeState     *bridge.BridgeStateQueue
	bridgeStateLock sync.Mutex
}

func (user *User) NewPuppet(contactID uint64) *Puppet {
	dbPuppet := user.bridge.DB.Puppet.New()
	dbPuppet.AccountID = *user.AccountID
	dbPuppet.ContactID = database.ContactID(contactID)
	return user.bridge.NewPuppet(dbPuppet)
}

func (user *User) NewPortal(chatID uint64) *Portal {
	dbPortal := user.bridge.DB.Portal.New()
	dbPortal.AccountID = *user.AccountID
	dbPortal.ChatID = database.ChatID(chatID)
	return user.bridge.NewPortal(dbPortal)
}

func (user *User) GetPuppetID(contactID uint64) database.PuppetID {
	if user.AccountID == nil {
		return database.PuppetID{}
	}

	return database.PuppetID{
		AccountID: *user.AccountID,
		ContactID: database.ContactID(contactID),
	}
}

func (user *User) GetPortalID(chatID uint64) database.PortalID {
	if user.AccountID == nil {
		return database.PortalID{}
	}

	return database.PortalID{
		AccountID: *user.AccountID,
		ChatID:    database.ChatID(chatID),
	}
}

func (user *User) GetRemoteID() string {
	if user.account == nil {
		return ""
	}

	return strconv.FormatInt(int64(user.account.Id), 10)
}

func (user *User) GetRemoteName() string {
	if user.account == nil {
		return ""
	}

	return user.account.Me().String()
}

func (user *User) GetPermissionLevel() bridgeconfig.PermissionLevel {
	return user.PermissionLevel
}

func (user *User) GetManagementRoomID() id.RoomID {
	return user.ManagementRoom
}

func (user *User) GetMXID() id.UserID {
	return user.MXID
}

func (user *User) GetCommandState() map[string]interface{} {
	return nil
}

func (user *User) GetIDoublePuppet() bridge.DoublePuppet {
	return nil
}

func (user *User) GetIGhost() bridge.Ghost {
	return nil
}

var _ bridge.User = (*User)(nil)

func (br *DeltaChatBridge) loadUser(dbUser *database.User, mxid *id.UserID) *User {
	if dbUser == nil {
		if mxid == nil {
			return nil
		}
		dbUser = br.DB.User.New()
		dbUser.MXID = *mxid
		dbUser.Insert()
	}

	user := br.NewUser(dbUser)
	br.usersByMXID[user.MXID] = user
	if user.ManagementRoom != "" {
		br.managementRoomsLock.Lock()
		br.managementRooms[user.ManagementRoom] = user
		br.managementRoomsLock.Unlock()
	}
	return user
}

func (br *DeltaChatBridge) GetUserByMXID(userID id.UserID) *User {
	if userID == br.Bot.UserID || br.IsGhost(userID) {
		return nil
	}
	br.usersLock.Lock()
	defer br.usersLock.Unlock()

	user, ok := br.usersByMXID[userID]
	if !ok {
		return br.loadUser(br.DB.User.GetByMXID(userID), &userID)
	}
	return user
}

func (br *DeltaChatBridge) GetUserByAccountID(accountID database.AccountID) *User {
	br.usersLock.Lock()
	defer br.usersLock.Unlock()

	user, ok := br.usersByAccountID[accountID]
	if !ok {
		return br.loadUser(br.DB.User.GetByAccountID(accountID), nil)
	}
	return user
}

func (br *DeltaChatBridge) NewUser(dbUser *database.User) *User {
	user := &User{
		User:     dbUser,
		bridge:   br,
		log:      br.ZLog.With().Str("user_id", string(dbUser.MXID)).Logger(),
		contacts: map[database.ContactID]*deltachat.Contact{},

		PermissionLevel: br.Config.Bridge.Permissions.Get(dbUser.MXID),
	}

	user.BridgeState = br.NewBridgeStateQueue(user)

	return user
}

func (user *User) Import() error {
	user.Lock()
	defer user.Unlock()

	if user.AccountID == nil {
		// FIXME: error
		return nil
	}

	// check that all contacts are mapped to puppets
	acct, err := user.getAccount()
	if err != nil {
		return err
	}

	chats, err := acct.ChatListEntries()
	if err != nil {
		return err
	}

	for _, chat := range chats {
		// fetching each portal will implicitly create them and invite the user
		user.bridge.GetPortalByID(database.PortalID{AccountID: *user.AccountID, ChatID: database.ChatID(chat.Id)})
	}

	return nil
}

func (user *User) SetManagementRoom(roomID id.RoomID) {
	user.bridge.managementRoomsLock.Lock()
	defer user.bridge.managementRoomsLock.Unlock()

	existing, ok := user.bridge.managementRooms[roomID]
	if ok {
		existing.ManagementRoom = ""
		existing.Update()
	}

	user.ManagementRoom = roomID
	user.bridge.managementRooms[user.ManagementRoom] = user
	user.Update()
}

func (user *User) GetSpaceRoom() id.RoomID {
	return id.RoomID("")
}

func (user *User) GetDMSpaceRoom() id.RoomID {
	return id.RoomID("")
}

func (user *User) ViewingChannel(portal *Portal) bool {
	return false
}

func (user *User) Account() (*deltachat.Account, error) {
	return user.getAccount()
}

func (user *User) getAccount() (*deltachat.Account, error) {
	if user.account == nil {
		if user.AccountID == nil {
			acc, err := user.bridge.AccountManager.AddAccount()
			if err != nil {
				return nil, err
			}

			user.account = acc
			accountID := database.AccountID(acc.Id)
			user.AccountID = &accountID
			user.Update()
		}

		accounts, err := user.bridge.AccountManager.Accounts()
		if err != nil {
			return nil, err
		}

		for _, acc := range accounts {
			if database.AccountID(acc.Id) == *user.AccountID {
				user.account = acc
			}
		}

		if user.account == nil {
			panic("account not found")
		}
	}

	return user.account, nil
}

func (user *User) SetConfig(key, value string) error {
	acct, err := user.getAccount()
	if err != nil {
		return err
	}

	return acct.SetConfig(key, value)
}

func (user *User) GetConfig(key string) (string, error) {
	acct, err := user.getAccount()
	if err != nil {
		return "", err
	}

	return acct.GetConfig(key)
}

func (user *User) Login() error {
	user.Lock()
	defer user.Unlock()

	acct, err := user.getAccount()
	if err != nil {
		return err
	}

	err = acct.Configure()
	if err != nil {
		return err
	}

	return nil

}
func (user *User) IsLoggedIn() bool {
	user.Lock()
	defer user.Unlock()

	acct, err := user.getAccount()
	if err != nil {
		user.log.Err(err).Msg("Failed to get account")
		return false
	}

	ok, err := acct.IsConfigured()
	if err != nil {
		user.log.Err(err).Msg("Failed to check if configured")
		return false
	}

	return ok
}

func (user *User) Logout(isOverwriting bool) {
	err := user.Disconnect()
	if err != nil && err != ErrNotConnected {
		user.log.Err(err).Msg("Failed to disconnect on logout")
		return
	}

	user.Lock()
	defer user.Unlock()

	_, err = user.getAccount()
	if err != nil {
		user.log.Err(err).Msg("Failed to get account")
		return
	}

	// FIXME: delete account data?
}

func (user *User) Connected() bool {
	acct, err := user.getAccount()
	if err != nil {
		user.log.Err(err).Msg("Failed to get account")
		return false
	}

	conn, err := acct.Connectivity()
	if err != nil {
		user.log.Err(err).Msg("Failed to get connectivity")
		return false
	}

	// anything not disconnected is probably connected
	return conn >= DC_CONNECTIVITY_CONNECTING
}

func (user *User) Connect() error {
	user.Lock()
	defer user.Unlock()

	acct, err := user.getAccount()
	if err != nil {
		return err
	}

	if ok, err := acct.IsConfigured(); err != nil {
		return err
	} else if !ok {
		return ErrNotLoggedIn
	}

	if err = acct.StartIO(); err != nil {
		return err
	}

	go user.processAccountEvents(user.account.GetEventChannel())
	return nil
}

const DC_CONNECTIVITY_NOT_CONNECTED = 1000
const DC_CONNECTIVITY_CONNECTING = 2000
const DC_CONNECTIVITY_WORKING = 3000
const DC_CONNECTIVITY_CONNECTED = 4000

func (user *User) processAccountEvents(eventsChan <-chan *deltachat.Event) {
	log := user.log.With().Str("component", "account_events").Logger()
	acct, err := user.getAccount()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get account in event loop")
	}

	for {
		evt, ok := <-eventsChan
		if !ok {
			break
		}

		var message string

		switch evt.Type {
		case deltachat.EVENT_INFO:
			log.Trace().Msg(evt.Msg)
		case deltachat.EVENT_ERROR:
			log.Error().Msg(evt.Msg)
			message = fmt.Sprintf("%s: %s", evt.Type, evt.Msg)
		case deltachat.EVENT_WARNING:
			log.Warn().Msg(evt.Msg)
			message = fmt.Sprintf("%s: %s", evt.Type, evt.Msg)
		case deltachat.EVENT_CONFIGURE_PROGRESS:
			message = fmt.Sprintf("%s: %d", evt.Type, evt.Progress)
		case deltachat.EVENT_CONNECTIVITY_CHANGED:
			conn, err := user.account.Connectivity()
			if err != nil {
				log.Err(err).Msg("Connectivity check failed")
			}

			status := "Disconnected."
			if conn >= DC_CONNECTIVITY_CONNECTED {
				status = "Connected!"
			} else if conn >= DC_CONNECTIVITY_WORKING {
				status = "Working..."
			} else if conn >= DC_CONNECTIVITY_CONNECTING {
				status = "Connecting..."
			} else if conn >= DC_CONNECTIVITY_NOT_CONNECTED {
				status = "Not connected..."
			}
			message = status
		case deltachat.EVENT_INCOMING_MSG:
			msg := deltachat.Message{Account: user.account, Id: evt.MsgId}
			snap, err := msg.Snapshot()
			if err != nil {
				user.log.Err(err).Msg("Failed to get incoming message snapshot")
				break
			}

			portal := user.bridge.GetPortalByID(database.PortalID{AccountID: database.AccountID(acct.Id), ChatID: database.ChatID(snap.ChatId)})
			portal.ReceiveDeltaChatMessage(snap)
		case deltachat.EVENT_INCOMING_MSG_BUNCH:
			// not used
		case deltachat.EVENT_CONTACTS_CHANGED:
			puppet := user.bridge.GetPuppetByID(database.PuppetID{AccountID: database.AccountID(acct.Id), ContactID: database.ContactID(evt.ContactId)})
			err := puppet.Update()
			if err != nil {
				user.log.Err(err).Msg("Failed to update puppet")
				break
			}
		case deltachat.EVENT_CHAT_MODIFIED:
			portal := user.bridge.GetPortalByID(database.PortalID{AccountID: database.AccountID(acct.Id), ChatID: database.ChatID(evt.ChatId)})
			portal.Update()
			if err != nil {
				user.log.Err(err).Msg("Failed to update portal")
				break
			}
		default:
			log.Warn().Str("type", evt.Type).Msg("Account event ignored")
			// FIXME hide these later
			message = fmt.Sprintf("%s?", evt.Type)
		}

		if message != "" {
			user.bridge.AS.BotIntent().SendMessageEvent(user.GetManagementRoomID(), event.EventMessage, event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    message,
			})
		}
	}

	user.log.Debug().Msg("Account event loop exit.")
}

func (user *User) Disconnect() error {
	user.Lock()
	defer user.Unlock()

	if !user.Connected() {
		return ErrNotConnected
	}

	acct, err := user.getAccount()
	if err != nil {
		return err
	}

	err = acct.StopIO()
	if err != nil {
		return err
	}

	return nil
}

func (user *User) ensureInvited(intent *appservice.IntentAPI, roomID id.RoomID, isDirect bool) bool {
	if intent == nil {
		intent = user.bridge.Bot
	}
	ret := false

	inviteContent := event.Content{
		Parsed: &event.MemberEventContent{
			Membership: event.MembershipInvite,
			IsDirect:   isDirect,
		},
		Raw: map[string]interface{}{},
	}

	/*
		customPuppet := user.bridge.GetPuppetByCustomMXID(user.MXID)
		if customPuppet != nil && customPuppet.CustomIntent() != nil {
			inviteContent.Raw["fi.mau.will_auto_accept"] = true
		}
	*/

	_, err := intent.SendStateEvent(roomID, event.StateMember, user.MXID.String(), &inviteContent)

	var httpErr mautrix.HTTPError
	if err != nil && errors.As(err, &httpErr) && httpErr.RespError != nil && strings.Contains(httpErr.RespError.Err, "is already in the room") {
		user.bridge.StateStore.SetMembership(roomID, user.MXID, event.MembershipJoin)
		ret = true
	} else if err != nil {
		user.log.Error().Err(err).Str("room_id", roomID.String()).Msg("Failed to invite user to room")
	} else {
		ret = true
	}

	/*
		if customPuppet != nil && customPuppet.CustomIntent() != nil {
			err = customPuppet.CustomIntent().EnsureJoined(roomID, appservice.EnsureJoinedParams{IgnoreCache: true})
			if err != nil {
				user.log.Warn().Err(err).Str("room_id", roomID.String()).Msg("Failed to auto-join room")
				ret = false
			} else {
				ret = true
			}
		}
	*/

	return ret
}
