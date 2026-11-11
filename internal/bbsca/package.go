package bbsca

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

// Package is a zip of what puts the CA's certificate among a client
// machine's trusted roots, for whoever downloads it (a caller, from a file
// area or the web):
//
//	NAME-CA.cer         the certificate (DER): double-click, Install
//	                    Certificate (Windows 98 and later, with IE 4 or later)
//	NAME-CA.crt         the same, PEM (Linux, other programs)
//	install-ca.inf      right-click, Install: the trusted roots of the
//	                    machine (Windows 95 and later), and Uninstall in
//	                    Add/Remove Programs' place by remove-ca.reg
//	install-ca.reg      the same by the registry: double-click
//	remove-ca.reg       takes it out again
//	install-ca.bat      certutil where there is one (Vista and later), else
//	                    the .reg
//	NAME-CA.mobileconfig  macOS and iOS: a profile to install
//	README.txt          which to use
func (ca *CA) Package(name string) ([]byte, error) {
	base := fileName(name) + "-CA"
	thumb := ca.Thumbprint()
	blob := certBlob(ca.Cert.Raw)
	key := `SOFTWARE\Microsoft\SystemCertificates\ROOT\Certificates\` + thumb
	files := []struct {
		name string
		data []byte
	}{
		{base + ".cer", ca.Cert.Raw},
		{base + ".crt", ca.PEM},
		{"install-ca.inf", crlf(infFile(name, key, blob))},
		{"install-ca.reg", crlf(regFile(key, blob))},
		{"remove-ca.reg", crlf("REGEDIT4\n\n[-HKEY_LOCAL_MACHINE\\" + key + "]\n\n")},
		{"install-ca.bat", crlf(batFile(base, name))},
		{base + ".mobileconfig", mobileconfig(name, ca.Cert.Raw, thumb)},
		{"README.txt", crlf(readme(name, base, thumb))},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	now := time.Now()
	for _, f := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// certBlob is a certificate as Windows keeps it in the registry (a
// serialized store element): its SHA-1 hash property, then the
// certificate itself (CERT_CERT_PROP_ID).
func certBlob(der []byte) []byte {
	var b bytes.Buffer
	prop := func(id uint32, data []byte) {
		binary.Write(&b, binary.LittleEndian, id)
		binary.Write(&b, binary.LittleEndian, uint32(1))
		binary.Write(&b, binary.LittleEndian, uint32(len(data)))
		b.Write(data)
	}
	sum := sha1.Sum(der)
	prop(3, sum[:]) // CERT_SHA1_HASH_PROP_ID
	prop(32, der)   // CERT_CERT_PROP_ID
	return b.Bytes()
}

// hexLines is data as comma-separated hex bytes, in lines of n bytes
// joined by sep (a continuation and the next line's indent).
func hexLines(data []byte, n int, sep string) string {
	var sb strings.Builder
	for i, c := range data {
		if i > 0 {
			sb.WriteString(",")
			if i%n == 0 {
				sb.WriteString(sep)
			}
		}
		fmt.Fprintf(&sb, "%02x", c)
	}
	return sb.String()
}

func infFile(name, key string, blob []byte) string {
	return `; Puts ` + name + `'s certificate authority among this machine's trusted
; roots.  Right-click, Install.
[Version]
Signature="$CHICAGO$"

[DefaultInstall]
AddReg=BBS.CA.AddReg

[DefaultUninstall]
DelReg=BBS.CA.DelReg

[BBS.CA.AddReg]
HKLM,"` + key + `","Blob",0x00000001,\
 ` + hexLines(blob, 24, "\\\n ") + `

[BBS.CA.DelReg]
HKLM,"` + key + `"
`
}

func regFile(key string, blob []byte) string {
	return "REGEDIT4\n\n[HKEY_LOCAL_MACHINE\\" + key + "]\n\"Blob\"=hex:" + hexLines(blob, 24, "\\\n  ") + "\n\n"
}

func batFile(base, name string) string {
	return `@echo off
rem Puts ` + name + `'s certificate authority among this machine's trusted roots.
rem Run it as an administrator (Vista and later: right-click, Run as administrator).
cd /d "%~dp0"
certutil -addstore -f Root "` + base + `.cer" >nul 2>&1
if not errorlevel 1 goto done
regedit /s install-ca.reg
:done
echo ` + name + `'s certificate authority is installed.
pause
`
}

func mobileconfig(name string, der []byte, thumb string) []byte {
	id := "bbs.ca." + strings.ToLower(thumb[:16])
	esc := html.EscapeString(name)
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadCertificateFileName</key>
			<string>ca.cer</string>
			<key>PayloadContent</key>
			<data>` + base64.StdEncoding.EncodeToString(der) + `</data>
			<key>PayloadDisplayName</key>
			<string>` + esc + ` CA</string>
			<key>PayloadIdentifier</key>
			<string>` + id + `.root</string>
			<key>PayloadType</key>
			<string>com.apple.security.root</string>
			<key>PayloadUUID</key>
			<string>` + uuidOf(thumb, "root") + `</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>` + esc + ` certificate authority</string>
	<key>PayloadIdentifier</key>
	<string>` + id + `</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>` + uuidOf(thumb, "profile") + `</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>
`)
}

// uuidOf is a UUID (version 5 in form) made from the thumbprint, so the
// same CA's profile is the same profile.
func uuidOf(thumb, what string) string {
	h := sha1.Sum([]byte(thumb + "/" + what))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func readme(name, base, thumb string) string {
	return name + `'s certificate authority
` + strings.Repeat("=", len(name)+26) + `

` + name + ` signs the certificates of the old services it stands in for
(MSN Messenger's Passport, Yahoo! Messenger's login, AIM's, its Gemini
server, a TLS proxy) itself.  Put its certificate authority among your
machine's trusted roots once, and those programs connect without a warning.

Its fingerprint (SHA-1), to check against what the BBS shows:
  ` + thumbGroups(thumb) + `

Windows 95, 98, ME, 2000, XP
  Right-click install-ca.inf, Install.  Or double-click install-ca.reg.
  (On Windows 98 and later with Internet Explorer 4 or later, double-
  clicking ` + base + `.cer, Install Certificate, "Trusted Root
  Certification Authorities" does it for your user too.)
  remove-ca.reg takes it out again.

Windows Vista and later
  Right-click install-ca.bat, Run as administrator.

macOS, iOS
  Open ` + base + `.mobileconfig and install the profile; on iOS, then turn it
  on under Settings, General, About, Certificate Trust Settings.

Linux
  Copy ` + base + `.crt to /usr/local/share/ca-certificates/ and run
  update-ca-certificates (Debian, Ubuntu), or to
  /etc/pki/ca-trust/source/anchors/ and run update-ca-trust (Fedora).

Programs with their own certificate store (Firefox, Netscape, AIM 6 and
later) import ` + base + `.crt in their own settings: Firefox, Settings,
Privacy and Security, View Certificates, Authorities, Import.
`
}

// thumbGroups is a thumbprint in pairs, as Windows shows it.
func thumbGroups(t string) string {
	var parts []string
	for i := 0; i+2 <= len(t); i += 2 {
		parts = append(parts, t[i:i+2])
	}
	return strings.Join(parts, " ")
}

func crlf(s string) []byte { return []byte(strings.ReplaceAll(s, "\n", "\r\n")) }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// fileName is name as a file name's start.
func fileName(name string) string {
	n := strings.Trim(unsafeName.ReplaceAllString(name, "-"), "-.")
	if n == "" {
		return "BBS"
	}
	return n
}
