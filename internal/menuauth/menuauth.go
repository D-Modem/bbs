// Package menuauth asks modemmenu's auth socket ([authd] there) whether a
// password is a user's: modemmenu's backends (its users file, LDAP, SQL,
// the system's accounts by PAM) answer, so that callers who log in by
// modemmenu use the same password for the BBS's telnet, SSH and web.
package menuauth

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// Client asks the socket at Socket.
type Client struct {
	Socket  string
	Timeout time.Duration // for an answer (modemmenu waits fail_delay before a no)
}

type request struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type reply struct {
	OK    bool   `json:"ok"`
	Auth  string `json:"auth"`
	Error string `json:"error"`
}

// Check asks whether pw is user's password: true and the backend that
// said so, false if it is not; an error if modemmenu could not be asked
// or would not answer (refused: not a user it answers).
func (c *Client) Check(user, pw string) (bool, string, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	conn, err := net.DialTimeout("unix", c.Socket, timeout)
	if err != nil {
		return false, "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))
	b, _ := json.Marshal(request{User: user, Password: pw})
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return false, "", err
	}
	line, err := bufio.NewReader(conn).ReadSlice('\n')
	if err != nil {
		return false, "", fmt.Errorf("no answer: %w", err)
	}
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		return false, "", fmt.Errorf("an answer not understood: %q", line)
	}
	switch {
	case r.OK:
		return true, r.Auth, nil
	case r.Error == "denied":
		return false, "", nil
	case r.Error != "":
		return false, "", errors.New(r.Error)
	}
	return false, "", fmt.Errorf("an answer not understood: %q", line)
}
