package irc

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
)

type SASLClient interface {
	Handshake() (mech string)
	Respond(challenge string) (res string, err error)
}

type SASLPlain struct {
	Username string
	Password string
}

func (auth *SASLPlain) Handshake() (mech string) {
	mech = "PLAIN"
	return
}

func (auth *SASLPlain) Respond(challenge string) (res string, err error) {
	if challenge != "+" {
		err = errors.New("Unexpected challenge")
		return
	}

	user := []byte(auth.Username)
	pass := []byte(auth.Password)
	payload := bytes.Join([][]byte{user, user, pass}, []byte{0})
	res = base64.StdEncoding.EncodeToString(payload)

	return
}

var SupportedCapabilities = map[string]struct{}{
	"account-notify":    {},
	"account-tag":       {},
	"away-notify":       {},
	"batch":             {},
	"cap-notify":        {},
	"echo-message":      {},
	"extended-join":     {},
	"invite-notify":     {},
	"labeled-response":  {},
	"message-tags":      {},
	"multi-prefix":      {},
	"server-time":       {},
	"sasl":              {},
	"setname":           {},
	"userhost-in-names": {},
}

type ConnectionState int

const (
	ConnStart ConnectionState = iota
	ConnRegistered
	ConnQuit
)

type User struct {
	Nick    string
	AwayMsg string
}

type Channel struct {
	Name      string
	Members   map[string]string
	Topic     string
	TopicWho  string
	TopicTime time.Time
	Secret    bool
}

type action interface{}

type (
	actionJoin struct {
		Channel string
	}
	actionPart struct {
		Channel string
	}

	actionPrivMsg struct {
		Channel string
		Content string
	}

	actionTyping struct {
		Channel string
	}
	actionTypingStop struct {
		Channel string
	}
)

type SessionParams struct {
	Nickname string
	Username string
	RealName string

	Auth SASLClient
}

type Session struct {
	conn io.ReadWriteCloser
	msgs chan Message
	acts chan action
	evts chan Event

	running      atomic.Value // bool
	state        ConnectionState
	typingStamps map[string]time.Time

	nick  string
	lNick string
	user  string
	real  string
	acct  string
	host  string
	auth  SASLClient

	mode string
	motd string

	availableCaps map[string]string
	enabledCaps   map[string]struct{}
	features      map[string]string

	users    map[string]User
	channels map[string]Channel
}

func NewSession(conn io.ReadWriteCloser, params SessionParams) (s Session, err error) {
	s = Session{
		conn:          conn,
		msgs:          make(chan Message, 128),
		acts:          make(chan action, 128),
		evts:          make(chan Event, 128),
		typingStamps:  map[string]time.Time{},
		nick:          params.Nickname,
		lNick:         strings.ToLower(params.Nickname),
		user:          params.Username,
		real:          params.RealName,
		auth:          params.Auth,
		availableCaps: map[string]string{},
		enabledCaps:   map[string]struct{}{},
		features:      map[string]string{},
		users:         map[string]User{},
		channels:      map[string]Channel{},
	}

	s.running.Store(true)

	err = s.send("CAP LS 302\r\nNICK %s\r\nUSER %s 0 * :%s\r\n", s.nick, s.user, s.real)
	if err != nil {
		return
	}

	go func() {
		r := bufio.NewScanner(conn)

		for r.Scan() {
			line := r.Text()
			//fmt.Println(" > ", line)

			msg, err := Tokenize(line)
			if err != nil {
				continue
			}

			err = msg.Validate()
			if err != nil {
				continue
			}

			s.msgs <- msg
		}

		s.Stop()
	}()

	go s.run()

	return
}

func (s *Session) Running() bool {
	return s.running.Load().(bool)
}

func (s *Session) Stop() {
	s.running.Store(false)
	s.conn.Close()
}

func (s *Session) Poll() (events <-chan Event) {
	return s.evts
}

func (s *Session) IsChannel(name string) bool {
	return strings.IndexAny(name, "#&") == 0 // TODO compute CHANTYPES
}

func (s *Session) Join(channel string) {
	s.acts <- actionJoin{channel}
}

func (s *Session) join(act actionJoin) (err error) {
	err = s.send("JOIN %s\r\n", act.Channel)
	return
}

func (s *Session) Part(channel string) {
	s.acts <- actionPart{channel}
}

func (s *Session) part(act actionPart) (err error) {
	err = s.send("PART %s\r\n", act.Channel)
	return
}

func (s *Session) PrivMsg(channel, content string) {
	s.acts <- actionPrivMsg{channel, content}
}

func (s *Session) privMsg(act actionPrivMsg) (err error) {
	err = s.send("PRIVMSG %s :%s\r\n", act.Channel, act.Content)
	return
}

func (s *Session) Typing(channel string) {
	s.acts <- actionTyping{channel}
}

func (s *Session) typing(act actionTyping) (err error) {
	if _, ok := s.enabledCaps["message-tags"]; !ok {
		return
	}

	to := strings.ToLower(act.Channel)
	now := time.Now()

	if t, ok := s.typingStamps[to]; ok && now.Sub(t).Seconds() < 3.0 {
		return
	}

	s.typingStamps[to] = now

	err = s.send("@+typing=active TAGMSG %s\r\n", act.Channel)
	return
}

func (s *Session) TypingStop(to string) {
	s.acts <- actionTypingStop{to}
}

func (s *Session) typingStop(act actionTypingStop) (err error) {
	if _, ok := s.enabledCaps["message-tags"]; !ok {
		return
	}

	err = s.send("@+typing=done TAGMSG %s\r\n", act.Channel)
	return
}

func (s *Session) run() {
	for s.Running() {
		var (
			ev  Event
			err error
		)

		select {
		case act := <-s.acts:
			switch act := act.(type) {
			case actionJoin:
				err = s.join(act)
			case actionPart:
				err = s.part(act)
			case actionPrivMsg:
				err = s.privMsg(act)
			case actionTyping:
				err = s.typing(act)
			case actionTypingStop:
				err = s.typingStop(act)
			}
		case msg := <-s.msgs:
			if s.state == ConnStart {
				ev, err = s.handleStart(msg)
			} else if s.state == ConnRegistered {
				ev, err = s.handle(msg)
			}
		}

		if ev != nil {
			s.evts <- ev
		}
		if err != nil {
			s.evts <- err
		}
	}
}

func (s *Session) handleStart(msg Message) (ev Event, err error) {
	switch msg.Command {
	case "AUTHENTICATE":
		if s.auth != nil {
			var res string

			res, err = s.auth.Respond(msg.Params[0])
			if err != nil {
				err = s.send("AUTHENTICATE *\r\n")
				return
			}

			err = s.send("AUTHENTICATE %s\r\n", res)
			if err != nil {
				return
			}
		}
	case "900":
		err = s.send("CAP END\r\n")
		if err != nil {
			return
		}

		s.acct = msg.Params[2]
		_, _, s.host = FullMask(msg.Params[1])
	case "902", "904", "905", "906", "907", "908":
		err = s.send("CAP END\r\n")
		if err != nil {
			return
		}
	case "CAP":
		switch msg.Params[1] {
		case "LS":
			var willContinue bool
			var ls string

			if msg.Params[2] == "*" {
				willContinue = true
				ls = msg.Params[3]
			} else {
				willContinue = false
				ls = msg.Params[2]
			}

			for _, c := range TokenizeCaps(ls) {
				if c.Enable {
					s.availableCaps[c.Name] = c.Value
				} else {
					delete(s.availableCaps, c.Name)
				}
			}

			if !willContinue {
				var req strings.Builder

				for c := range s.availableCaps {
					if _, ok := SupportedCapabilities[c]; !ok {
						continue
					}

					_, _ = fmt.Fprintf(&req, "CAP REQ %s\r\n", c)
				}

				_, ok := s.availableCaps["sasl"]
				if s.auth == nil || !ok {
					_, _ = fmt.Fprintf(&req, "CAP END\r\n")
				}

				err = s.send(req.String())
				if err != nil {
					return
				}
			}
		case "ACK":
			for _, c := range strings.Split(msg.Params[2], " ") {
				s.enabledCaps[c] = struct{}{}

				if s.auth != nil && c == "sasl" {
					h := s.auth.Handshake()
					err = s.send("AUTHENTICATE %s\r\n", h)
					if err != nil {
						return
					}
				}
			}
		}
	case "372": // RPL_MOTD
		s.motd += "\n" + strings.TrimPrefix(msg.Params[1], "- ")
	case "433": // ERR_NICKNAMEINUSE
		s.nick = s.nick + "_"

		err = s.send("NICK %s\r\n", s.nick)
		if err != nil {
			return
		}
	default:
		ev, err = s.handle(msg)
	}

	return
}

func (s *Session) handle(msg Message) (ev Event, err error) {
	switch msg.Command {
	case "001": // RPL_WELCOME
		s.nick = msg.Params[0]
		s.lNick = strings.ToLower(s.nick)
		s.state = ConnRegistered
		ev = RegisteredEvent{}

		if s.host == "" {
			err = s.send("WHO %s\r\n", s.nick)
			if err != nil {
				return
			}
		}
	case "005": // RPL_ISUPPORT
		s.updateFeatures(msg.Params[1 : len(msg.Params)-1])
	case "352": // RPL_WHOREPLY
		if s.lNick == strings.ToLower(msg.Params[5]) {
			s.host = msg.Params[3]
		}
	case "CAP":
		switch msg.Params[1] {
		case "ACK":
			for _, c := range strings.Split(msg.Params[2], " ") {
				s.enabledCaps[c] = struct{}{}
			}
		case "NAK":
			for _, c := range strings.Split(msg.Params[2], " ") {
				delete(s.enabledCaps, c)
			}
		case "NEW":
			diff := TokenizeCaps(msg.Params[2])

			for _, c := range diff {
				if c.Enable {
					s.availableCaps[c.Name] = c.Value
				} else {
					delete(s.availableCaps, c.Name)
				}
			}

			var req strings.Builder

			for _, c := range diff {
				_, ok := SupportedCapabilities[c.Name]
				if !c.Enable || !ok {
					continue
				}

				_, _ = fmt.Fprintf(&req, "CAP REQ %s\r\n", c.Name)
			}

			_, ok := s.availableCaps["sasl"]
			if s.acct == "" && ok {
				// TODO authenticate
			}

			err = s.send(req.String())
			if err != nil {
				return
			}
		case "DEL":
			diff := TokenizeCaps(msg.Params[2])

			for i := range diff {
				diff[i].Enable = !diff[i].Enable
			}

			for _, c := range diff {
				if c.Enable {
					s.availableCaps[c.Name] = c.Value
				} else {
					delete(s.availableCaps, c.Name)
				}
			}

			var req strings.Builder

			for _, c := range diff {
				_, ok := SupportedCapabilities[c.Name]
				if !c.Enable || !ok {
					continue
				}

				_, _ = fmt.Fprintf(&req, "CAP REQ %s\r\n", c.Name)
			}

			_, ok := s.availableCaps["sasl"]
			if s.acct == "" && ok {
				// TODO authenticate
			}

			err = s.send(req.String())
			if err != nil {
				return
			}
		}
	case "JOIN":
		nick, _, _ := FullMask(msg.Prefix)
		lNick := strings.ToLower(nick)
		channel := strings.ToLower(msg.Params[0])
		channelEv := ChannelEvent{Channel: msg.Params[0]}

		if lNick == s.lNick {
			s.channels[channel] = Channel{
				Name:    msg.Params[0],
				Members: map[string]string{},
			}
		} else if c, ok := s.channels[channel]; ok {
			if _, ok := s.users[lNick]; !ok {
				s.users[lNick] = User{Nick: nick}
			}
			c.Members[lNick] = ""
			ev = UserJoinEvent{ChannelEvent: channelEv, UserEvent: UserEvent{Nick: nick}}
		}
	case "PART":
		nick, _, _ := FullMask(msg.Prefix)
		lNick := strings.ToLower(nick)
		channel := strings.ToLower(msg.Params[0])
		channelEv := ChannelEvent{Channel: msg.Params[0]}

		if lNick == s.lNick {
			delete(s.channels, channel)
			ev = SelfPartEvent{ChannelEvent: channelEv}
		} else if c, ok := s.channels[channel]; ok {
			delete(c.Members, lNick)
			ev = UserPartEvent{ChannelEvent: channelEv, UserEvent: UserEvent{Nick: nick}}
		}
	case "353": // RPL_NAMREPLY
		channel := strings.ToLower(msg.Params[2])

		if c, ok := s.channels[channel]; ok {
			c.Secret = msg.Params[1] == "@"
			names := TokenizeNames(msg.Params[3], "~&@%+") // TODO compute prefixes

			for _, name := range names {
				nick := name.Nick
				lNick := strings.ToLower(nick)

				if _, ok := s.users[lNick]; !ok {
					s.users[lNick] = User{Nick: nick}
				}
				c.Members[lNick] = name.PowerLevel
			}
		}
	case "366": // RPL_ENDOFNAMES
		ev = SelfJoinEvent{ChannelEvent{Channel: msg.Params[1]}}
	case "332": // RPL_TOPIC
		channel := strings.ToLower(msg.Params[1])

		if c, ok := s.channels[channel]; ok {
			c.Topic = msg.Params[2]
		}
	case "PRIVMSG":
		nick, _, _ := FullMask(msg.Prefix)
		target := strings.ToLower(msg.Params[0])

		if target == s.lNick {
			// PRIVMSG to self
			t, ok := msg.Time()
			if !ok {
				t = time.Now()
			}
			ev = QueryMessageEvent{
				UserEvent: UserEvent{Nick: nick},
				Content:   msg.Params[1],
				Time:      t,
			}
		} else if _, ok := s.channels[target]; ok {
			// PRIVMSG to channel
			t, ok := msg.Time()
			if !ok {
				t = time.Now()
			}
			ev = ChannelMessageEvent{
				UserEvent:    UserEvent{Nick: nick},
				ChannelEvent: ChannelEvent{Channel: msg.Params[0]},
				Content:      msg.Params[1],
				Time:         t,
			}
		}
	case "PING":
		err = s.send("PONG :%s\r\n", msg.Params[0])
		if err != nil {
			return
		}
	case "ERROR":
		err = errors.New("connection terminated")
		if len(msg.Params) > 0 {
			err = fmt.Errorf("connection terminated: %s", msg.Params[0])
		}
		s.state = ConnQuit
	default:
	}
	return
}

func (s *Session) updateFeatures(features []string) {
	for _, f := range features {
		if f == "" || f == "-" || f == "=" || f == "-=" {
			continue
		}

		var (
			add   bool
			key   string
			value string
		)

		if strings.HasPrefix(f, "-") {
			add = false
			f = f[1:]
		} else {
			add = true
		}

		kv := strings.SplitN(f, "=", 2)
		key = strings.ToUpper(kv[0])
		if len(kv) > 1 {
			value = kv[1]
		}

		if add {
			s.features[key] = value
		} else {
			delete(s.features, key)
		}
	}
}

/*
func (cli *Session) send(format string, args ...interface{}) (err error) {
	msg := fmt.Sprintf(format, args...)

	for _, line := range strings.Split(msg, "\r\n") {
		if line != "" {
			fmt.Println("<  ", line)
		}
	}

	_, err = cli.conn.Write([]byte(msg))

	return
}

// */

//*
func (s *Session) send(format string, args ...interface{}) (err error) {
	_, err = fmt.Fprintf(s.conn, format, args...)
	return
}

// */

/*
func (s *Session) send(format string, args ...interface{}) (err error) {
	go fmt.Fprintf(s.conn, format, args...)
	return
}
// */