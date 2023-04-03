package config

import (
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/deltachat/deltachat-rpc-client-go/deltachat"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
)

type BridgeConfig struct {
	UsernameTemplate          string `yaml:"username_template"`
	DisplaynameTemplate       string `yaml:"displayname_template"`
	ChannelNameTemplate       string `yaml:"channel_name_template"`
	PrivateChatPortalMeta     bool   `yaml:"private_chat_portal_meta"`
	PrivateChannelCreateLimit int    `yaml:"startup_private_channel_create_limit"`

	PortalMessageBuffer int `yaml:"portal_message_buffer"`

	DeliveryReceipts            bool `yaml:"delivery_receipts"`
	MessageStatusEvents         bool `yaml:"message_status_events"`
	MessageErrorNotices         bool `yaml:"message_error_notices"`
	RestrictedRooms             bool `yaml:"restricted_rooms"`
	AutojoinThreadOnOpen        bool `yaml:"autojoin_thread_on_open"`
	EmbedFieldsAsTables         bool `yaml:"embed_fields_as_tables"`
	MuteChannelsOnCreate        bool `yaml:"mute_channels_on_create"`
	SyncDirectChatList          bool `yaml:"sync_direct_chat_list"`
	ResendBridgeInfo            bool `yaml:"resend_bridge_info"`
	CustomEmojiReactions        bool `yaml:"custom_emoji_reactions"`
	DeletePortalOnChannelDelete bool `yaml:"delete_portal_on_channel_delete"`
	FederateRooms               bool `yaml:"federate_rooms"`
	AnimatedSticker             struct {
		Target string `yaml:"target"`
		Args   struct {
			Width  int `yaml:"width"`
			Height int `yaml:"height"`
			FPS    int `yaml:"fps"`
		} `yaml:"args"`
	} `yaml:"animated_sticker"`

	DoublePuppetServerMap      map[string]string `yaml:"double_puppet_server_map"`
	DoublePuppetAllowDiscovery bool              `yaml:"double_puppet_allow_discovery"`
	LoginSharedSecretMap       map[string]string `yaml:"login_shared_secret_map"`

	CommandPrefix      string                           `yaml:"command_prefix"`
	ManagementRoomText bridgeconfig.ManagementRoomTexts `yaml:"management_room_text"`

	Encryption bridgeconfig.EncryptionConfig `yaml:"encryption"`

	Provisioning struct {
		Prefix       string `yaml:"prefix"`
		SharedSecret string `yaml:"shared_secret"`
	} `yaml:"provisioning"`

	Permissions bridgeconfig.PermissionConfig `yaml:"permissions"`

	usernameTemplate    *template.Template `yaml:"-"`
	displaynameTemplate *template.Template `yaml:"-"`
	channelNameTemplate *template.Template `yaml:"-"`
}

func (bc *BridgeConfig) GetResendBridgeInfo() bool {
	return bc.ResendBridgeInfo
}

func (bc *BridgeConfig) EnableMessageStatusEvents() bool {
	return bc.MessageStatusEvents
}

func (bc *BridgeConfig) EnableMessageErrorNotices() bool {
	return bc.MessageErrorNotices
}

func boolToInt(val bool) int {
	if val {
		return 1
	}
	return 0
}

func (bc *BridgeConfig) Validate() error {
	_, hasWildcard := bc.Permissions["*"]
	_, hasExampleDomain := bc.Permissions["example.com"]
	_, hasExampleUser := bc.Permissions["@admin:example.com"]
	exampleLen := boolToInt(hasWildcard) + boolToInt(hasExampleUser) + boolToInt(hasExampleDomain)
	if len(bc.Permissions) <= exampleLen {
		return errors.New("bridge.permissions not configured")
	}
	return nil
}

type umBridgeConfig BridgeConfig

func (bc *BridgeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	err := unmarshal((*umBridgeConfig)(bc))
	if err != nil {
		return err
	}

	bc.usernameTemplate, err = template.New("username").Parse(bc.UsernameTemplate)
	if err != nil {
		return err
	} else if !strings.Contains(bc.FormatUsername("1234567890"), "1234567890") {
		return fmt.Errorf("username template is missing user ID placeholder")
	}
	bc.displaynameTemplate, err = template.New("displayname").Parse(bc.DisplaynameTemplate)
	if err != nil {
		return err
	}
	bc.channelNameTemplate, err = template.New("channel_name").Parse(bc.ChannelNameTemplate)
	if err != nil {
		return err
	}

	return nil
}

var _ bridgeconfig.BridgeConfig = (*BridgeConfig)(nil)

func (bc BridgeConfig) GetEncryptionConfig() bridgeconfig.EncryptionConfig {
	return bc.Encryption
}

func (bc BridgeConfig) GetCommandPrefix() string {
	return bc.CommandPrefix
}

func (bc BridgeConfig) GetManagementRoomTexts() bridgeconfig.ManagementRoomTexts {
	return bc.ManagementRoomText
}

func (bc BridgeConfig) FormatUsername(userID string) string {
	var buffer strings.Builder
	_ = bc.usernameTemplate.Execute(&buffer, userID)
	return buffer.String()
}

func (bc BridgeConfig) FormatDisplayname(user *deltachat.Contact) string {
	var buffer strings.Builder
	_ = bc.displaynameTemplate.Execute(&buffer, user)
	return buffer.String()
}

type ChannelNameParams struct {
	Name string
}

func (bc BridgeConfig) FormatChannelName(params ChannelNameParams) string {
	var buffer strings.Builder
	_ = bc.channelNameTemplate.Execute(&buffer, params)
	return buffer.String()
}
