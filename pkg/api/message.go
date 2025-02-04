package api

import "fmt"

// ButtonStyle is a style of Button element.
type ButtonStyle string

// Represents a general button styles.
const (
	ButtonStyleDefault ButtonStyle = ""
	ButtonStylePrimary ButtonStyle = "primary"
	ButtonStyleDanger  ButtonStyle = "danger"
)

// SelectType is a type of Button element.
type SelectType string

// Represents a select dropdown types.
const (
	StaticSelect   SelectType = "static"
	ExternalSelect SelectType = "external"
)

// MessageType defines the message type.
type MessageType string

const (
	// DefaultMessage defines a message that should be displayed in default mode supported by communicator.
	DefaultMessage MessageType = ""
	// BaseBodyWithFilterMessage defines a message that should be displayed in plaintext mode supported by communicator.
	// In this form the built-in filter is supported.
	// NOTE: only BaseBody is preserved. All other properties are ignored even if set.
	BaseBodyWithFilterMessage MessageType = "baseBodyWithFilter"
	// PopupMessage defines a message that should be displayed to the user as popup (if possible).
	PopupMessage MessageType = "form"
)

// Message represents a generic message with interactive buttons.
type Message struct {
	Type              MessageType
	BaseBody          Body
	Sections          []Section
	PlaintextInputs   LabelInputs
	OnlyVisibleForYou bool
	ReplaceOriginal   bool
}

func (msg *Message) IsEmpty() bool {
	var emptyBase Body
	if msg.BaseBody != emptyBase {
		return false
	}
	if msg.HasInputs() {
		return false
	}
	if msg.HasSections() {
		return false
	}

	return true
}

// HasSections returns true if message has interactive sections.
func (msg *Message) HasSections() bool {
	return len(msg.Sections) != 0
}

// HasInputs returns true if message has interactive inputs.
func (msg *Message) HasInputs() bool {
	return len(msg.PlaintextInputs) != 0
}

// Select holds data related to the select drop-down.
type Select struct {
	Type    SelectType
	Name    string
	Command string
	// OptionGroups provides a way to group options in a select menu.
	OptionGroups []OptionGroup
	// InitialOption holds already pre-selected options. MUST be a sub-set of OptionGroups.
	InitialOption *OptionItem
}

// Base holds generic message fields.
type Base struct {
	Header      string
	Description string
	Body        Body
}

// Body holds message body fields.
type Body struct {
	CodeBlock string
	Plaintext string
}

// Section holds section related fields.
type Section struct {
	Base
	Buttons         Buttons
	MultiSelect     MultiSelect
	Selects         Selects
	PlaintextInputs LabelInputs
	TextFields      TextFields
	Context         ContextItems
}

// LabelInputs holds the plain text input items.
type LabelInputs []LabelInput

// ContextItems holds context items.
type ContextItems []ContextItem

// TextFields holds text field items.
type TextFields []TextField

// TextField holds a text field data.
type TextField struct {
	Text string
}

// IsDefined returns true if there are any context items defined.
func (c ContextItems) IsDefined() bool {
	return len(c) > 0
}

// ContextItem holds context item.
type ContextItem struct {
	Text string
}

// Selects holds multiple Select objects.
type Selects struct {
	// ID allows to identify a given block when we do the updated.
	ID    string
	Items []Select
}

// DispatchedInputAction defines when the action should be sent to our backend.
type DispatchedInputAction string

// Defines the possible options to dispatch the input action.
const (
	NoDispatchInputAction          DispatchedInputAction = ""
	DispatchInputActionOnEnter     DispatchedInputAction = "on_enter_pressed"
	DispatchInputActionOnCharacter DispatchedInputAction = "on_character_entered"
)

// LabelInput is used to create input elements to use in slack messages.
type LabelInput struct {
	Command          string
	Text             string
	Placeholder      string
	DispatchedAction DispatchedInputAction
}

// AreOptionsDefined returns true if some options are available.
func (s *Selects) AreOptionsDefined() bool {
	if s == nil {
		return false
	}
	return len(s.Items) > 0
}

// OptionItem defines an option model.
type OptionItem struct {
	Name  string
	Value string
}

// MultiSelect holds multi select related fields.
type MultiSelect struct {
	Name        string
	Description Body
	Command     string

	// Options holds all available options
	Options []OptionItem

	// InitialOptions hold already pre-selected options. MUST be a sub-set of Options.
	InitialOptions []OptionItem
}

// OptionGroup holds information about options in the same group.
type OptionGroup struct {
	Name    string
	Options []OptionItem
}

// AreOptionsDefined returns true if some options are available.
func (m *MultiSelect) AreOptionsDefined() bool {
	if m == nil {
		return false
	}
	if len(m.Options) == 0 {
		return false
	}
	return true
}

// Buttons holds definition of interactive buttons.
type Buttons []Button

// AtLeastOneButtonHasDescription returns true if there is at least one button with description associated with it.
func (s *Buttons) AtLeastOneButtonHasDescription() bool {
	if s == nil {
		return false
	}
	for _, item := range *s {
		if item.Description != "" {
			return true
		}
	}

	return false
}

// Button holds definition of action button.
type Button struct {
	Description string
	Name        string
	Command     string
	URL         string
	Style       ButtonStyle
}

// ButtonBuilder provides a simplified way to construct a Button model.
type ButtonBuilder struct{}

func NewMessageButtonBuilder() *ButtonBuilder {
	return &ButtonBuilder{}
}

// ForCommandWithDescCmd returns button command where description and command are the same.
func (b *ButtonBuilder) ForCommandWithDescCmd(name, cmd string, style ...ButtonStyle) Button {
	bt := ButtonStyleDefault
	if len(style) > 0 {
		bt = style[0]
	}
	return b.commandWithDesc(name, cmd, cmd, bt)
}

// DescriptionURL returns link button with description.
func (b *ButtonBuilder) DescriptionURL(name, cmd string, url string, style ...ButtonStyle) Button {
	bt := ButtonStyleDefault
	if len(style) > 0 {
		bt = style[0]
	}

	return Button{
		Name:        name,
		Description: fmt.Sprintf("%s %s", MessageBotNamePlaceholder, cmd),
		URL:         url,
		Style:       bt,
	}
}

// ForCommandWithoutDesc returns button command without description.
func (b *ButtonBuilder) ForCommandWithoutDesc(name, cmd string, style ...ButtonStyle) Button {
	bt := ButtonStyleDefault
	if len(style) > 0 {
		bt = style[0]
	}
	cmd = fmt.Sprintf("%s %s", MessageBotNamePlaceholder, cmd)
	return Button{
		Name:    name,
		Command: cmd,
		Style:   bt,
	}
}

// ForCommand returns button command.
func (b *ButtonBuilder) ForCommand(name, cmd, desc string, style ...ButtonStyle) Button {
	bt := ButtonStyleDefault
	if len(style) > 0 {
		bt = style[0]
	}
	cmd = fmt.Sprintf("%s %s", MessageBotNamePlaceholder, cmd)
	desc = fmt.Sprintf("%s %s", MessageBotNamePlaceholder, desc)
	return Button{
		Name:        name,
		Command:     cmd,
		Description: desc,
		Style:       bt,
	}
}

// ForURL returns link button.
func (b *ButtonBuilder) ForURL(name, url string, style ...ButtonStyle) Button {
	bt := ButtonStyleDefault
	if len(style) > 0 {
		bt = style[0]
	}

	return Button{
		Name:  name,
		URL:   url,
		Style: bt,
	}
}

func (b *ButtonBuilder) commandWithDesc(name, cmd, desc string, style ButtonStyle) Button {
	cmd = fmt.Sprintf("%s %s", MessageBotNamePlaceholder, cmd)
	desc = fmt.Sprintf("%s %s", MessageBotNamePlaceholder, desc)
	return Button{
		Name:        name,
		Command:     cmd,
		Description: desc,
		Style:       style,
	}
}
