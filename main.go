package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/deltachat/deltachat-rpc-client-go/deltachat"
	"github.com/rs/zerolog"

	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/configupgrade"

	"go.mau.fi/mautrix-deltachat/config"
	"go.mau.fi/mautrix-deltachat/database"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

//go:embed example-config.yaml
var ExampleConfig string

type DeltaChatBridge struct {
	bridge.Bridge

	Config         *config.Config
	DB             *database.Database
	RPC            *deltachat.RpcIO
	AccountManager *deltachat.AccountManager

	//provisioning *ProvisioningAPI

	usersByMXID      map[id.UserID]*User
	usersByAccountID map[database.AccountID]*User
	usersLock        sync.Mutex

	managementRooms     map[id.RoomID]*User
	managementRoomsLock sync.Mutex

	portalsByMXID map[id.RoomID]*Portal
	portalsByID   map[database.PortalID]*Portal
	portalsLock   sync.Mutex

	puppets             map[database.PuppetID]*Puppet
	puppetsByCustomMXID map[id.UserID]*Puppet
	puppetsLock         sync.Mutex

	//attachmentTransfers *util.SyncMap[attachmentKey, *util.ReturnableOnce[*database.File]]
}

func (br *DeltaChatBridge) GetExampleConfig() string {
	return ExampleConfig
}

func (br *DeltaChatBridge) GetConfigPtr() interface{} {
	br.Config = &config.Config{
		BaseConfig: &br.Bridge.Config,
	}
	br.Config.BaseConfig.Bridge = &br.Config.Bridge
	return br.Config
}

func (br *DeltaChatBridge) Init() {
	br.RPC = deltachat.NewRpcIO()
	br.RPC.Stderr = ZStderr(br.ZLog.With().Str("component", "deltachat-core").Logger())
	br.AccountManager = &deltachat.AccountManager{Rpc: br.RPC}

	br.CommandProcessor = commands.NewProcessor(&br.Bridge)
	br.RegisterCommands()

	//matrixHTMLParser.PillConverter = br.pillConverter

	br.DB = database.New(br.Bridge.DB, br.Log.Sub("Database"))
	//deltaChatLog = br.ZLog.With().Str("component", "deltachat").Logger()

	// TODO move this to mautrix-go?
	zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
		files := strings.Split(file, "/")
		file = files[len(files)-1]
		name := runtime.FuncForPC(pc).Name()
		fns := strings.Split(name, ".")
		name = fns[len(fns)-1]
		return fmt.Sprintf("%s:%d:%s()", file, line, name)
	}

	// load users, puppets and portals
	for _, dbUser := range br.DB.User.GetAll() {
		user := br.NewUser(dbUser)
		if user.AccountID != nil {
			br.usersByAccountID[*user.AccountID] = user
		}
		if user.MXID != "" {
			br.usersByMXID[user.MXID] = user
		}
	}

	for _, dbPuppet := range br.DB.Puppet.GetAll() {
		puppet := br.NewPuppet(dbPuppet)
		br.puppets[puppet.ID()] = puppet
		/*
			if puppet.MXID != "" {
				br.puppetsByCustomMXID[puppet.MXID] = puppet
			}
		*/
	}

	for _, dbPortal := range br.DB.Portal.GetAll() {
		portal := br.NewPortal(dbPortal)
		br.portalsByID[portal.ID()] = portal
		if portal.MXID != "" {
			br.portalsByMXID[portal.MXID] = portal
		}
	}
}

func (br *DeltaChatBridge) Start() {
	if err := br.RPC.Start(); err != nil {
		br.ZLog.Fatal().Err(err).Msg("Failed to communicate with Delta Chat core")
	}

	// for each user we already know, import anything we've might've missed
	accounts, err := br.AccountManager.Accounts()
	if err != nil {
		br.ZLog.Err(err).Msg("Failed to get accounts on init")
		return
	}

	for _, acct := range accounts {
		user := br.GetUserByAccountID(database.AccountID(acct.Id))
		if user == nil {
			br.ZLog.Warn().Uint64("account_id", acct.Id).Msg("Account found from Delta Chat database not mapped?")
			continue
		}

		err := user.Import()
		if err != nil {
			br.ZLog.Err(err).Msg("Failed to import user data")
		} else {
			br.ZLog.Info().Str("user_id", string(user.GetMXID())).Msg("User imported successfully!")
		}

		err = user.Connect()
		if err != nil {
			br.ZLog.Err(err).Msg("Failed to connect user")
		}
	}
	/*
		if br.Config.Bridge.Provisioning.SharedSecret != "disable" {
			br.provisioning = newProvisioningAPI(br)
		}
	*/
	//go br.startUsers()
}

func (br *DeltaChatBridge) Stop() {
	br.RPC.Stop()
	for _, user := range br.usersByMXID {
		if !user.Connected() {
			continue
		}

		br.Log.Debugln("Disconnecting", user.MXID)
		user.Disconnect()
	}
}

func (br *DeltaChatBridge) GetAllIPortals() (iportals []bridge.Portal) {
	/*
		portals := br.GetAllPortals()
		iportals = make([]bridge.Portal, len(portals))
		for i, portal := range portals {
			iportals[i] = portal
		}
		return iportals
	*/
	return []bridge.Portal{}
}

func (br *DeltaChatBridge) GetIPortal(mxid id.RoomID) bridge.Portal {
	return br.GetPortalByMXID(mxid)
}

func (br *DeltaChatBridge) GetIUser(mxid id.UserID, create bool) bridge.User {
	return br.GetUserByMXID(mxid)
}

func (br *DeltaChatBridge) IsGhost(mxid id.UserID) bool {
	_, isGhost := br.ParsePuppetMXID(mxid)
	return isGhost
}

func (br *DeltaChatBridge) GetIGhost(mxid id.UserID) bridge.Ghost {
	return br.GetPuppetByMXID(mxid)
}

func (br *DeltaChatBridge) CreatePrivatePortal(id id.RoomID, user bridge.User, ghost bridge.Ghost) {
	//TODO implement
}

func main() {
	br := &DeltaChatBridge{
		usersByMXID:      make(map[id.UserID]*User),
		usersByAccountID: make(map[database.AccountID]*User),

		managementRooms: make(map[id.RoomID]*User),

		portalsByMXID: make(map[id.RoomID]*Portal),
		portalsByID:   make(map[database.PortalID]*Portal),

		puppets:             make(map[database.PuppetID]*Puppet),
		puppetsByCustomMXID: make(map[id.UserID]*Puppet),
	}
	br.Bridge = bridge.Bridge{
		Name:         "mautrix-deltachat",
		URL:          "https://github.com/mautrix/deltachat",
		Description:  "A Matrix-DeltaChat puppeting bridge.",
		Version:      "0.0.0",
		ProtocolName: "Delta Chat",

		CryptoPickleKey: "maunium.net/go/mautrix-deltachat",

		ConfigUpgrader: &configupgrade.StructUpgrader{
			SimpleUpgrader: configupgrade.SimpleUpgrader(config.DoUpgrade),
			Blocks:         config.SpacedBlocks,
			Base:           ExampleConfig,
		},

		Child: br,
	}
	br.InitVersion(Tag, Commit, BuildTime)

	br.Main()
}
