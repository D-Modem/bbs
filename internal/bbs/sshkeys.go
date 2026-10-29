package bbs

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/D-Modem/bbs/internal/store"
)

// MaxSSHKeys is how many keys one user may have.
const MaxSSHKeys = 20

// SSH key types a user may log in with (no DSA; RSA of 2048 bits or more).
var sshKeyTypes = map[string]bool{
	ssh.KeyAlgoED25519: true, ssh.KeyAlgoRSA: true, ssh.KeyAlgoECDSA256: true,
	ssh.KeyAlgoECDSA384: true, ssh.KeyAlgoECDSA521: true,
	ssh.KeyAlgoSKED25519: true, ssh.KeyAlgoSKECDSA256: true,
}

// ParseSSHKey reads one authorized_keys line (no options).
func ParseSSHKey(line string) (*store.SSHKey, error) {
	line = strings.TrimSpace(line)
	if len(line) > 16<<10 {
		return nil, errors.New("that is too long for a key")
	}
	pk, comment, opts, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, errors.New("that is not an SSH public key (as in id_ed25519.pub)")
	}
	if len(opts) > 0 {
		return nil, errors.New("a key here takes no options (command=, from= ...)")
	}
	if len(strings.TrimSpace(string(rest))) > 0 {
		return nil, errors.New("one key at a time")
	}
	if !sshKeyTypes[pk.Type()] {
		return nil, fmt.Errorf("%s keys are not taken: use ed25519, ECDSA or RSA", pk.Type())
	}
	if ck, ok := pk.(ssh.CryptoPublicKey); ok {
		if r, ok := ck.CryptoPublicKey().(*rsa.PublicKey); ok && r.N.BitLen() < 2048 {
			return nil, errors.New("RSA keys need 2048 bits or more")
		}
	}
	comment = Clean(comment, 100)
	k := &store.SSHKey{Type: pk.Type(), Fingerprint: ssh.FingerprintSHA256(pk), Comment: comment,
		Key: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk)))}
	if comment != "" {
		k.Key += " " + comment
	}
	return k, nil
}

// AddSSHKey gives a user a key to log in by SSH with.
func (b *BBS) AddSSHKey(u *store.User, line string) (*store.SSHKey, error) {
	k, err := ParseSSHKey(line)
	if err != nil {
		return nil, err
	}
	have, err := b.Store.SSHKeys(u.ID)
	if err != nil {
		return nil, err
	}
	if len(have) >= MaxSSHKeys {
		return nil, fmt.Errorf("a user may have %d keys at most", MaxSSHKeys)
	}
	k.UserID, k.User = u.ID, u.Name
	if k.ID, err = b.Store.AddSSHKey(k); err != nil {
		return nil, err
	}
	b.Log.Printf("%s: SSH key added: %s %s", u.Name, k.Type, k.Fingerprint)
	return k, nil
}

// SSHKeyLogin finds whose key it is: the user, if name is theirs and they
// are not locked.
func (b *BBS) SSHKeyLogin(name string, pk ssh.PublicKey) (*store.User, *store.SSHKey, error) {
	k, err := b.Store.SSHKeyByFingerprint(ssh.FingerprintSHA256(pk))
	if err != nil {
		return nil, nil, ErrBadLogin
	}
	// the fingerprint found it; the key itself must be the same too
	want, _, _, _, err := ssh.ParseAuthorizedKey([]byte(k.Key))
	if err != nil || string(want.Marshal()) != string(pk.Marshal()) {
		return nil, nil, ErrBadLogin
	}
	u, err := b.Store.UserByID(k.UserID)
	if err != nil || !strings.EqualFold(u.Name, name) {
		return nil, nil, ErrBadLogin
	}
	if u.Locked {
		return nil, nil, ErrLocked
	}
	return u, k, nil
}
