package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// offline: QWK offline mail, the caller's new messages and mail in a
// packet to take away, their answers brought back in a REP.
func (s *Session) offline() error {
	id := s.B.BBSID()
	for {
		s.N.Doing("Offline mail")
		u := s.refresh()
		unread, err := s.B.Store.UnreadMail(u.ID)
		if err != nil {
			return err
		}
		areas, err := s.B.AreasFor(u)
		if err != nil {
			return err
		}
		off, err := s.B.Store.ScanOff(u.ID)
		if err != nil {
			return err
		}
		scanned, waiting := 0, 0
		for _, a := range areas {
			if !off[a.ID] {
				scanned++
				waiting += a.New
			}
		}
		t := &view.Table{Title: "Offline mail (QWK)",
			Note: []view.Line{view.T(view.Dim, "Read and answer offline with a QWK reader: your packet holds your new "+
				"messages in the areas you scan, and your unread mail.")},
			Columns: []view.Column{{Title: "", Width: 22}, {Title: ""}},
			Rows: []view.Row{
				{Cells: []string{"Your packet", id + ".QWK"}},
				{Cells: []string{"Your replies", id + ".REP"}},
				{Cells: []string{"New messages", fmt.Sprintf("%d in %d areas", waiting, scanned)}},
				{Cells: []string{"Unread mail", strconv.Itoa(unread)}},
			}}
		if s.UI.Transfers() {
			t.Actions = []view.Action{{ID: "download", Label: "Download your packet", Key: 'D', PF: 5, Short: "Download"},
				{ID: "upload", Label: "Upload your replies", Key: 'U', PF: 6, Short: "Upload"}}
		} else {
			t.Command = "File transfer (IND$FILE): GET " + id + ".QWK takes your packet, PUT " + id + ".REP brings your replies."
		}
		if s.B.FTPAddress != "" {
			t.Note = append(t.Note, view.T(view.Dim, "By FTP: get /qwk/"+id+".QWK, put "+id+".REP into /qwk/."))
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "download":
			err = s.qwkDownload()
		case "upload":
			var data []byte
			if data, err = s.UI.ReceiveData(qwk.MaxREP); err == nil && data != nil {
				err = s.takeREP(data)
			}
		case view.Cmd:
			var msg string
			if msg, err = s.UI.DataCommand(c.Text, s.qwkGet, s.qwkPut); msg != "" {
				s.UI.Say(view.Plain, msg)
			}
		}
		if err != nil {
			return err
		}
	}
}

// qwkDownload sends the caller's QWK packet by a transfer protocol; its
// messages count as read only once it has gone. An empty packet is said, not
// sent.
func (s *Session) qwkDownload() error {
	out, err := s.N.QWKPacket()
	if err != nil {
		return err
	}
	if out.Messages == 0 {
		s.UI.Say(view.Plain, "There is nothing new for your packet.")
		return nil
	}
	sent, err := s.UI.SendData(out.Name, out.Data)
	if err != nil || !sent {
		return err
	}
	if err := out.Done(); err != nil {
		return err
	}
	s.UI.Say(view.Good, fmt.Sprintf("%d messages packed (%d of them mail); they now count as read.",
		out.Messages, out.Mail))
	return nil
}

// qwkGet makes the packet an IND$FILE GET asks for.
func (s *Session) qwkGet(name string) ([]byte, func(), error) {
	want := s.B.BBSID() + ".QWK"
	if !strings.EqualFold(name, want) {
		return nil, nil, fmt.Errorf("ask for %s", want)
	}
	out, err := s.N.QWKPacket()
	if err != nil {
		return nil, nil, err
	}
	return out.Data, func() { out.Done() }, nil
}

// qwkPut takes the REP an IND$FILE PUT brought.
func (s *Session) qwkPut(name string, data []byte) string {
	res, err := s.N.ImportREP(data)
	if err != nil {
		return "Your replies were not taken: " + err.Error() + "."
	}
	msg := "Your replies: " + res.String() + "."
	if len(res.Errors) > 0 {
		msg += " " + res.Errors[0]
	}
	return msg
}

// takeREP brings the caller's replies in, and says how it went.
func (s *Session) takeREP(data []byte) error {
	res, err := s.N.ImportREP(data)
	if err != nil {
		s.UI.Say(view.Bad, "Your replies were not taken: "+err.Error()+".")
		return nil
	}
	if len(res.Errors) == 0 {
		s.UI.Say(view.Good, "Your replies: "+res.String()+".")
		return nil
	}
	ls := []view.Line{view.T(view.Plain, "Your replies: "+res.String()+".")}
	for _, e := range res.Errors {
		ls = append(ls, view.T(view.Bad, e))
	}
	return s.UI.Show("Your replies", ls)
}
