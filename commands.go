package main

import (
	"strings"

	"maunium.net/go/mautrix/bridge/commands"
)

type WrappedCommandEvent struct {
	*commands.Event
	Bridge *DeltaChatBridge
	User   *User
	Portal *Portal
}

var HelpSectionPortalManagement = commands.HelpSection{Name: "Portal management", Order: 20}

func (br *DeltaChatBridge) RegisterCommands() {
	proc := br.CommandProcessor.(*commands.Processor)
	proc.AddHandlers(
		cmdSet,
		cmdGet,
		cmdLogin,
		cmdLogout,
		cmdConnect,
		cmdDisconnect,
		cmdPing,
	)
}

func wrapCommand(handler func(*WrappedCommandEvent)) func(*commands.Event) {
	return func(ce *commands.Event) {
		user := ce.User.(*User)
		var portal *Portal
		if ce.Portal != nil {
			portal = ce.Portal.(*Portal)
		}
		br := ce.Bridge.Child.(*DeltaChatBridge)
		handler(&WrappedCommandEvent{ce, br, user, portal})
	}
}

var cmdLogin = &commands.FullHandler{
	Func: wrapCommand(fnLogin),
	Name: "login",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Login to your email account",
		Args:        "<user/bot/oauth> <_token_>",
	},
}

func fnLogin(ce *WrappedCommandEvent) {
	if ce.User.IsLoggedIn() {
		ce.Reply("You're already logged in")
		return
	}

	err := ce.User.Login()
	if err != nil {
		ce.Reply("Error: %v", err)
	} else {
		ce.Reply("Login initiated, will post status here!")
	}
}

var cmdLogout = &commands.FullHandler{
	Func: wrapCommand(fnLogout),
	Name: "logout",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Logout and remove account.",
	},
}

func fnLogout(ce *WrappedCommandEvent) {
	ce.User.Logout(false)
	ce.Reply("Logged out successfully.")
}

var cmdConnect = &commands.FullHandler{
	Func: wrapCommand(fnConnect),
	Name: "connect",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Connect to server.",
	},
}

func fnConnect(ce *WrappedCommandEvent) {
	err := ce.User.Connect()
	if err != nil {
		ce.Reply("Error: %v", err)
	} else {
		ce.Reply("Connecting.")
	}
}

var cmdDisconnect = &commands.FullHandler{
	Func: wrapCommand(fnDisconnect),
	Name: "disconnect",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Disconnect from server.",
	},
}

func fnDisconnect(ce *WrappedCommandEvent) {
	err := ce.User.Disconnect()
	if err != nil {
		ce.Reply("Error: %v", err)
	} else {
		ce.Reply("Disconnecting.")
	}
}

var cmdGet = &commands.FullHandler{
	Func: wrapCommand(fnGet),
	Name: "get",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "get configuration value",
	},
}

func fnGet(ce *WrappedCommandEvent) {
	if len(ce.Args) != 1 {
		ce.Reply("**Usage**: `$cmdprefix get <key>`")
		return
	}

	value, err := ce.User.GetConfig(ce.Args[0])
	if err != nil {
		ce.Reply("Error: %v", err)
	} else {
		if strings.Contains(ce.Args[0], "pw") && value != "" {
			value = "***"
		}
		ce.Reply("%s = %s", ce.Args[0], value)
	}
}

var cmdSet = &commands.FullHandler{
	Func: wrapCommand(fnSet),
	Name: "set",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "set configuration value",
	},
}

func fnSet(ce *WrappedCommandEvent) {
	if len(ce.Args) != 2 {
		ce.Reply("**Usage**: `$cmdprefix set <key> <value>`")
		return
	}

	err := ce.User.SetConfig(ce.Args[0], ce.Args[1])

	if strings.Contains(ce.Args[0], "pw") {
		ce.Args[1] = "***"
		defer ce.Redact()
	}

	if err != nil {
		ce.Reply("Error: %v", err)
	} else {
		ce.Reply("%s = %s", ce.Args[0], ce.Args[1])
	}
}

var cmdPing = &commands.FullHandler{
	Func: wrapCommand(fnPing),
	Name: "ping",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Check your connection to IMAP and SMTP",
	},
}

func fnPing(ce *WrappedCommandEvent) {
	if ce.User.AccountID == nil {
		ce.Reply("You're not logged in")
	} else {
		ce.Reply("You're logged in")
	}
}
