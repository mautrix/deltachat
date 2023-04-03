package main

import (
	"sync"

	"github.com/deltachat/deltachat-rpc-client-go/deltachat"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-deltachat/database"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type portalMatrixMessage struct {
	evt  *event.Event
	user *User
}

type Portal struct {
	*database.Portal
	sync.Mutex

	bridge *DeltaChatBridge
	log    zerolog.Logger
	chat   *deltachat.Chat

	matrixMessages chan portalMatrixMessage

	Encrypted bool
}

func (portal *Portal) Chat() (*deltachat.Chat, error) {
	portal.Lock()
	defer portal.Unlock()

	if portal.chat == nil {
		if portal.AccountID == 0 || portal.ChatID == 0 {
			return nil, ErrNotLoggedIn // FIXME
		}

		user := portal.bridge.GetUserByAccountID(portal.AccountID)
		if user == nil {
			return nil, ErrNotLoggedIn // FIXME
		}

		acct, err := user.Account()
		if err != nil {
			return nil, err
		}

		portal.chat = &deltachat.Chat{
			Account: acct,
			Id:      uint64(portal.ChatID),
		}
	}

	return portal.chat, nil
}

func (portal *Portal) IsEncrypted() bool {
	return portal.Encrypted
}

func (portal *Portal) MarkEncrypted() {
	portal.Encrypted = true
	portal.Upsert()
}

func (portal *Portal) IsPrivateChat() bool {
	// FIXME stub
	return true
}

func (portal *Portal) ReceiveMatrixEvent(user bridge.User, evt *event.Event) {
	if user.GetPermissionLevel() >= bridgeconfig.PermissionLevelUser {
		portal.matrixMessages <- portalMatrixMessage{user: user.(*User), evt: evt}
	}
}

func (portal *Portal) MainIntent() *appservice.IntentAPI {
	/*
		if portal.IsPrivateChat()  {
			return portal.bridge.GetPuppetByID(portal.OtherUserID).DefaultIntent()
		}
	*/

	return portal.bridge.Bot
}

func (br *DeltaChatBridge) GetPortalByMXID(mxid id.RoomID) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()

	portal, ok := br.portalsByMXID[mxid]
	if !ok {
		return &Portal{
			bridge: br,
		}
	}

	return portal
}

func (br *DeltaChatBridge) GetPortalByID(portalID database.PortalID) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()

	portal, ok := br.portalsByID[portalID]
	if !ok {
		dbPortal := br.DB.Portal.Get(portalID)
		portal := br.NewPortal(dbPortal)

		if dbPortal == nil {
			dbPortal = br.DB.Portal.New()
			dbPortal.AccountID = portalID.AccountID
			dbPortal.ChatID = portalID.ChatID
			portal = br.NewPortal(dbPortal)

			err := portal.Update()
			if err != nil {
				br.ZLog.Err(err).Msg("Failed to update portal in getter")
				return portal
			}
		}

		err := portal.Update()
		if err != nil {
			br.ZLog.Err(err).Msg("Failed to update portal in getter")
		}
		br.portalsByMXID[portal.MXID] = portal
		br.portalsByID[portalID] = portal
	}

	return portal
}

func (br *DeltaChatBridge) NewPortal(dbPortal *database.Portal) *Portal {
	if dbPortal == nil {
		return nil
	}

	log := br.ZLog.With().Str("portal", dbPortal.ID().String()).Logger()
	log.Trace().Msg("Creating new portal")

	portal := &Portal{
		Portal: dbPortal,
		bridge: br,
		log:    log,

		matrixMessages: make(chan portalMatrixMessage, br.Config.Bridge.PortalMessageBuffer),
	}

	go portal.messageLoop()

	return portal
}

func (portal *Portal) messageLoop() {
	for {
		select {
		case msg := <-portal.matrixMessages:
			portal.handleMatrixMessages(msg)
		}
	}
}

func (portal *Portal) handleMatrixMessages(msg portalMatrixMessage) {
	switch msg.evt.Type {
	case event.EventMessage, event.EventSticker:
		portal.handleMatrixMessage(msg.user, msg.evt)
	default:
		portal.log.Debug().Str("type", msg.evt.Type.String()).Msg("unknown event type")
	}
}

func (portal *Portal) handleMatrixMessage(sender *User, evt *event.Event) {
	portal.log.Debug().Msg("Handling matrix event")

	if portal.IsPrivateChat() && (sender.AccountID == nil || *sender.AccountID != portal.AccountID) {
		portal.log.Debug().Msg("Ignoring message in DM from non-user")
		return
	}

	user := portal.bridge.GetUserByAccountID(portal.AccountID)
	if user == nil {
		portal.log.Error().Msg("Failed to find target account")
		return
	}

	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		portal.log.Error().Msg("Failed to get content")
		return
	}

	switch content.MsgType {
	case event.MsgText, event.MsgEmote, event.MsgNotice:
		chat, err := portal.Chat()
		if err != nil {
			portal.log.Err(err).Msg("Failed to get chat from portal")
			return
		}

		_, err = chat.SendText(content.Body)
		if err != nil {
			portal.log.Err(err).Msg("Failed to send message")
			return
		}
		portal.log.Debug().Str("body", content.Body).Msg("Sent text event!")
	//case event.MsgAudio, event.MsgFile, event.MsgImage, event.MsgVideo:
	default:
		portal.log.Warn().Str("type", string(content.MsgType)).Msg("Ignored message type from Matrix")
	}
}

func (portal *Portal) UpdateBridgeInfo() {
	portal.log.Debug().Msg("UpdateBridgeInfo() stub")
}

func (portal *Portal) ensureUserInvited(user *User) bool {
	return user.ensureInvited(portal.MainIntent(), portal.MXID, portal.IsPrivateChat())
}

func (portal *Portal) GetEncryptionEventContent() (evt *event.EncryptionEventContent) {
	evt = &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}
	if rot := portal.bridge.Config.Bridge.Encryption.Rotation; rot.EnableCustom {
		evt.RotationPeriodMillis = rot.Milliseconds
		evt.RotationPeriodMessages = rot.Messages
	}
	return
}

func (portal *Portal) Update() error {
	chat, err := portal.Chat()
	if err != nil {
		return err
	}

	user := portal.bridge.GetUserByAccountID(portal.AccountID)
	if user == nil {
		return ErrNotLoggedIn // FIXME
	}

	snap, err := chat.FullSnapshot()
	if err != nil {
		return err
	}

	// FIXME we should use Matrix invites to handle this
	if snap.IsContactRequest {
		err := chat.Accept()
		if err != nil {
			return err
		}
	}

	portal.Name = snap.Name
	portal.NameSet = len(snap.Name) > 0
	portal.AvatarSet = !portal.AvatarURL.IsEmpty()

	createMatrixRoom := portal.MXID == ""
	if createMatrixRoom {
		err = portal.createMatrixRoom(user)
		if err != nil {
			return err
		}
	}

	// FIXME configurable
	for _, contactID := range snap.ContactIds {
		puppet := portal.bridge.GetPuppetByID(database.PuppetID{AccountID: *user.AccountID, ContactID: database.ContactID(contactID)})
		puppet.DefaultIntent().EnsureJoined(portal.MXID)
	}

	portal.ensureUserInvited(user)

	if createMatrixRoom {
		return nil
	}

	_, _ = user.bridge.Bot.SetRoomName(portal.MXID, portal.Name)

	return portal.Upsert()
}

func (portal *Portal) createMatrixRoom(user *User) error {
	portal.log.Info().Msg("Creating Matrix room for chat")

	intent := portal.MainIntent()
	if err := intent.EnsureRegistered(); err != nil {
		return err
	}

	initialState := []*event.Event{}
	/*
		bridgeInfoStateKey, bridgeInfo := portal.getBridgeInfo()
		initialState := []*event.Event{{
			Type:     event.StateBridge,
			Content:  event.Content{Parsed: bridgeInfo},
			StateKey: &bridgeInfoStateKey,
		}, {
			// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
			Type:     event.StateHalfShotBridge,
			Content:  event.Content{Parsed: bridgeInfo},
			StateKey: &bridgeInfoStateKey,
		}}
	*/

	if !portal.AvatarURL.IsEmpty() {
		initialState = append(initialState, &event.Event{
			Type: event.StateRoomAvatar,
			Content: event.Content{Parsed: &event.RoomAvatarEventContent{
				URL: portal.AvatarURL,
			}},
		})
	}

	creationContent := make(map[string]interface{})
	if !portal.bridge.Config.Bridge.FederateRooms {
		creationContent["m.federate"] = false
	}

	var invite []id.UserID

	if portal.bridge.Config.Bridge.Encryption.Default {
		initialState = append(initialState, &event.Event{
			Type: event.StateEncryption,
			Content: event.Content{
				Parsed: portal.GetEncryptionEventContent(),
			},
		})
		portal.Encrypted = true

		if portal.IsPrivateChat() {
			invite = append(invite, portal.bridge.Bot.UserID)
		}
	}

	resp, err := intent.CreateRoom(&mautrix.ReqCreateRoom{
		Visibility:      "private",
		Name:            portal.Name,
		Topic:           portal.Topic,
		Invite:          invite,
		Preset:          "private_chat",
		IsDirect:        portal.IsPrivateChat(),
		InitialState:    initialState,
		CreationContent: creationContent,
	})
	if err != nil {
		portal.log.Err(err).Msg("Failed to create room")
		return err
	}

	portal.MXID = resp.RoomID
	err = portal.Upsert()
	if err != nil {
		return err
	}

	portal.log.Info().Str("mxid", portal.MXID.String()).Msg("Matrix room created")

	if portal.Encrypted && portal.IsPrivateChat() {
		err = portal.bridge.Bot.EnsureJoined(portal.MXID, appservice.EnsureJoinedParams{BotOverride: portal.MainIntent().Client})
		if err != nil {
			portal.log.Err(err).Msg("Failed to ensure bridge bot is joined to private chat portal")
		}
	}

	//user.syncChatDoublePuppetDetails(portal, true)

	/*
		if portal.IsPrivateChat() {
			puppet := user.bridge.GetPuppetByID(portal.Key.Receiver)

			chats := map[id.UserID][]id.RoomID{puppet.MXID: {portal.MXID}}
			user.updateDirectChats(chats)
		}

		firstEventResp, err := portal.MainIntent().SendMessageEvent(portal.MXID, portalCreationDummyEvent, struct{}{})
		if err != nil {
			portal.log.Errorln("Failed to send dummy event to mark portal creation:", err)
		} else {
			portal.FirstEventID = firstEventResp.EventID
			portal.Update()
		}
	*/

	return nil
}