senpai(5)

# NAME

senpai - Configuration file format and settings

# DESCRIPTION

A senpai configuration file is a scfg file.
See https://git.sr.ht/~emersion/scfg.

Some settings are required, the others are optional.

# SETTINGS

*address* (required)
	The address (_host[:port]_) of the IRC server. senpai uses TLS connections
	by default unless you specify *tls* option to be *false*. TLS connections
	default to port 6697, plain-text use port 6667.

*nickname* (required)
	Your nickname, sent with a _NICK_ IRC message. It mustn't contain spaces or
	colons (*:*).

*realname*
	Your real name, or actually just a field that will be available to others
	and may contain spaces and colons.  Sent with the _USER_ IRC message.  By
	default, the value of *nick* is used.

*username*
	Your username, sent with the _USER_ IRC message and also used for SASL
	authentication.  By default, the value of *nick* is used.

*password*
	Your password, used for SASL authentication. See also *password-cmd*.

*password-cmd* command [arguments...]
	Alternatively to providing your SASL authentication password directly in
	plaintext, you can specify a command to be run to fetch the password at
	runtime. This is useful if you store your passwords in a separate (probably
	encrypted) file using `gpg` or a command line password manager such as
	_pass_ or _gopass_. If a *password-cmd* is provided, the value of *password*
	will be ignored and the first line of the output of *password-cmd* will be
	used for login.

*channel*
	A spaced separated list of channel names that senpai will automatically join
	at startup and server reconnect. This directive can be specified multiple
	times.

*highlight*
	A space separated list of keywords that will trigger a notification and a
	display indicator when said by others. This directive can be specified
	multiple times.

	By default, senpai will use your current nickname.

*on-highlight-path*
	Alternative path to a shell script to be executed when you are highlighted.
	By default, senpai looks for a highlight shell script at
	$XDG_CONFIG_HOME/senpai/highlight. If no file is found at that path, and an
	alternate path is not provided, highlight command execution is disabled.

	If unset, $XDG_CONFIG_HOME defaults to *~/.config/*.

	Before the highlight script is executed, the following environment
	variables are populated:

	Shell scripts MUST ENSURE VARIABLES appear QUOTED in the script file,
	OR YOU WILL BE OPEN TO SHELL INJECTION ATTACKS. Shell scripts must also
	ensure characters like '\*' and '?' are not expanded.

[[ *Environment variable*
:< *Description*
|  BUFFER
:  buffer where the message appeared
|  HERE
:  equals 1 if _BUFFER_ is the current buffer, 0 otherwise
|  MESSAGE
:  content of the message
|  SENDER
:  nickname of the sender

	Note: when passing those to *notify-send*(1), some notification daemons use
	*\\* for escape sequences in the body, which causes *\\* to disappear from the
	message or triggers unintended side-effects (like newlines).

	To get around this, you can double the backslash with the following snippet:

```
#!/bin/sh
escape() {
	printf "%s" "$1" | sed 's#\\#\\\\#g'
}

notify-send "[$BUFFER] $SENDER" "$(escape "$MESSAGE")"
```

*pane-widths* { ... }
	Configure the width of various UI panes.

	Pane widths are set as sub-directives of the main *pane-widths* directive:

```
pane-widths {
    nicknames 16
}
```

	This directive supports the following sub-directives:

	*nicknames*
		The number of cells that the column for nicknames occupies in the
		timeline. By default, 16.

	*channels*
		Make the channel list vertical, with a width equals to the given amount
		of cells.  By default, the channel list is horizontal.

	*members*
		Show the list of channel members on the right of the screen, with a
		width equals to the given amount of cells.

*tls*
	Enable TLS encryption.  Defaults to true.

*typings*
	Send typing notifications which let others know when you are typing a
	message. Defaults to true.

*mouse*
	Enable or disable mouse support.  Defaults to true.

*colors* { ... }
	Settings for colors of different UI elements.

	Colors are represented as numbers from 0 to 255 for 256 default terminal
	colors respectively. -1 has special meaning of default terminal color. To
	use true colors, *#*_rrggbb_ notation is supported.

	Colors are set as sub-directives of the main *colors* directive:

```
colors {
    prompt 2 # green
}
```

[[ *Sub-directive*
:< *Description*
|  prompt
:  color for ">"-prompt that appears in command mode
|  unread
:  foreground color for unread buffer names in buffer lists

*debug*
	Dump all sent and received data to the home buffer, useful for debugging.
	Defaults to false.

# EXAMPLES

A minimal configuration file to connect to Libera.Chat as "Guest123456":

```
address irc.libera.chat
nickname Guest123456
```

A more advanced configuration file that enables SASL authentication, fetches the
password from an external program instead of storing in plaintext, sends
notifications on highlight and decreases the width of the nick column to 12
(note: _swaymsg_ is specific to sway, a wayland compositor. Use whatever you
need to know if the terminal emulator that runs senpai has focus):

```
address irc.libera.chat
nickname Guest123456
username senpai
realname "Guest von Lenon"
password-cmd gopass show irc/guest # use your favorite CLI password solution here
channel "#rahxephon"
highlight guest senpai
highlight lenon # don't know why you'd split it into multiple lines, but you can if you want
pane-widths {
	nicknames 12
}
```

And the highlight file (*~/.config/senpai/highlight*):
```
#!/bin/sh

escape() {
	printf "%s" "$1" | sed 's#\\#\\\\#g'
}
FOCUS=$(swaymsg -t get_tree | jq '..|objects|select(.focused==true)|.name' | grep senpai | wc -l)
if [ "$HERE" -eq 0 ] || [ $FOCUS -eq 0 ]; then
	notify-send "[$BUFFER] $SENDER" "$(escape "$MESSAGE")"
fi
```

# SEE ALSO

*senpai*(1)
