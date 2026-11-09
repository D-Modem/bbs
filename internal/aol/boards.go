package aol

import (
	"errors"
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/aol/fdo"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// Message boards: the BBS's message areas as AOL's boards, each a list of
// its topics, newest first, and each topic its posts in order.  These are
// the host's own windows, whose buttons send keywords: "boards",
// "board N", "topic N", "post N" (a new topic in area N), "reply N" (to
// message N).  The post window's Send sends its two fields (subject, text)
// as de_data with postToken.

// postToken is what the host's post window sends its fields with.
const postToken = "Zp"

// The board windows, by global id.
const (
	boardsWindow = "50-4-1"
	boardWindow  = "50-4-2"
	topicWindow  = "50-4-3"
	postWindow   = "50-4-4"
)

const (
	boardMax      = 25    // the most topics a board lists
	topicMax      = 30    // the most posts a topic shows
	topicMaxBytes = 16000 // and the most text
)

// postTo is where the post window sends: a new topic in an area, or a
// reply to a message.
type postTo struct {
	area    *store.Area
	replyTo *store.Message
}

// boards lists the areas the user may read, with their new messages.
func (ss *session) boards() {
	areas, err := ss.s.B.AreasFor(ss.u)
	if err != nil {
		ss.logf("areas: %v", err)
		ss.notice("The message boards can't be opened just now.")
		return
	}
	var buttons []button
	for _, a := range areas {
		label := a.Name
		if a.New > 0 {
			label += fmt.Sprintf(" (%d new)", a.New)
		}
		buttons = append(buttons, button{label, fmt.Sprintf("board %d", a.ID)})
	}
	text := "There are no message boards here."
	if len(areas) > 0 {
		text = fmt.Sprintf("%s.  Pick one to see its topics.", count(len(areas), "board", "boards"))
	}
	ss.send(closeForm(boardsWindow))
	ss.send(windowForm(boardsWindow, "Message Boards", text, 3, buttons))
}

// area is an area the user may read, or nil (and told why).
func (ss *session) area(id int64) *store.Area {
	a, err := ss.s.B.Store.Area(id)
	if err != nil || !ss.s.B.CanRead(ss.u, a) {
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			ss.logf("area %d: %v", id, err)
		}
		ss.notice("There is no such board.")
		return nil
	}
	return a
}

// board lists an area's topics, the one written in last first.
func (ss *session) board(id int64) {
	a := ss.area(id)
	if a == nil {
		return
	}
	st := ss.s.B.Store
	topics, err := st.Topics(a.ID, 0, boardMax)
	if err != nil {
		ss.logf("topics %d: %v", a.ID, err)
		ss.notice("This board can't be opened just now.")
		return
	}
	roots := make([]int64, len(topics))
	for i, t := range topics {
		roots[i] = t.Root.ID
	}
	read, _ := st.TopicsRead(ss.u.ID, roots)
	total, _ := st.TopicCount(a.ID)
	var buttons []button
	if ss.s.B.CanPost(ss.u, a) {
		buttons = append(buttons, button{"Start a topic", fmt.Sprintf("post %d", a.ID)})
	}
	for _, t := range topics {
		mark := " "
		if read[t.Root.ID] < t.Last.ID {
			mark = "*"
		}
		buttons = append(buttons, button{
			label:   fmt.Sprintf("%s %s  %s (%d)", mark, t.Last.Created.Format("Jan 2"), clip(t.Root.Subject, 44), t.Replies+1),
			keyword: fmt.Sprintf("topic %d", t.Root.ID)})
	}
	text := a.Description
	if text == "" {
		text = a.Name
	}
	switch {
	case total == 0:
		text += "\r\n\r\nThere are no topics yet."
	case total > len(topics):
		text += fmt.Sprintf("\r\n\r\nThe %d of its %d topics written in last; * has posts you haven't read.", len(topics), total)
	default:
		text += fmt.Sprintf("\r\n\r\n%s; * has posts you haven't read.", count(total, "topic", "topics"))
	}
	buttons = append(buttons, button{"All boards", "boards"})
	ss.send(closeForm(boardWindow))
	ss.send(windowForm(boardWindow, a.Name, text, 4, buttons))
}

// topic shows a topic's posts in order, and marks them read.
func (ss *session) topic(id int64) {
	st := ss.s.B.Store
	m, err := st.Message(id)
	if err != nil || m.AreaID == 0 {
		ss.notice("There is no such topic.")
		return
	}
	a := ss.area(m.AreaID)
	if a == nil {
		return
	}
	root := st.TopicOf(m)
	posts, err := st.TopicMessages(root, 0, topicMax)
	if err != nil || len(posts) == 0 {
		ss.logf("topic %d: %v", root, err)
		ss.notice("This topic can't be opened just now.")
		return
	}
	n, _ := st.TopicLen(root)
	var sb strings.Builder
	shown := 0
	for _, p := range posts {
		part := fmt.Sprintf("%s, %s:\r\n%s\r\n\r\n", p.From, p.Created.Format("Jan 2, 2006 15:04"),
			strings.ReplaceAll(p.Body, "\n", "\r\n"))
		if shown > 0 && sb.Len()+len(part) > topicMaxBytes {
			break
		}
		sb.WriteString(part)
		shown++
	}
	if shown < n {
		fmt.Fprintf(&sb, "(%d of %d posts shown; read the rest by telnet or the web.)", shown, n)
	}
	last := posts[shown-1]
	st.SetTopicRead(ss.u.ID, root, last.ID)
	if lr, _ := st.LastRead(ss.u.ID, a.ID); lr < last.ID {
		st.SetLastRead(ss.u.ID, a.ID, last.ID)
	}
	buttons := []button{}
	if ss.s.B.CanPost(ss.u, a) {
		buttons = append(buttons, button{"Reply", fmt.Sprintf("reply %d", posts[len(posts)-1].ID)})
	}
	buttons = append(buttons, button{"Back to " + clip(a.Name, 30), fmt.Sprintf("board %d", a.ID)})
	ss.send(closeForm(topicWindow))
	ss.send(windowForm(topicWindow, posts[0].Subject, sb.String(), 16, buttons))
}

// postForm opens the post window: a new topic in an area, or a reply to
// a message.
func (ss *session) postForm(area, replyTo int64) {
	t := &postTo{}
	title, subject := "", ""
	if replyTo != 0 {
		m, err := ss.s.B.Store.Message(replyTo)
		if err != nil || m.AreaID == 0 {
			ss.notice("There is no such post.")
			return
		}
		area = m.AreaID
		t.replyTo = m
		subject = m.Subject
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
		title = "Reply: " + m.Subject
	}
	if t.area = ss.area(area); t.area == nil {
		return
	}
	if !ss.s.B.CanPost(ss.u, t.area) {
		ss.notice("Your access does not allow posting on this board.")
		return
	}
	if title == "" {
		title = "New topic in " + t.area.Name
	}
	ss.mu.Lock()
	ss.posting = t
	ss.mu.Unlock()
	ss.send(closeForm(postWindow))
	ss.send(postFormSrc(title, subject))
}

// postFormSrc is the post window: a subject, the text, and Send.
func postFormSrc(title, subject string) string {
	return fmt.Sprintf(`uni_start_stream
  man_start_object <ind_group, %s>
    mat_object_id <%s>
    mat_orientation <vcf>
    mat_position <center_center>
    man_start_object <ornament, "Subject:">
    man_end_object
    man_start_object <edit_view, "">
      mat_size <50, 1, %d>
      mat_relative_tag <1>
      mat_bool_vertical_scroll <no>
      man_append_data <%s>
      man_end_data
    man_end_object
    man_start_object <edit_view, "">
      mat_size <50, 12, 16000>
      mat_relative_tag <2>
    man_end_object
    man_start_object <trigger, "Send">
      mat_bool_default <yes>
      act_replace_select_action
        <
        uni_start_stream_wait_on <00x>
        de_ez_send_form <"%s">
        >
    man_end_object
  man_end_object
  man_update_display
  uni_wait_off
uni_end_stream
`, fdo.Quote(title), postWindow, bbs.MaxSubjectLen, fdo.Quote(subject), postToken)
}

// post is the post window's Send: its subject and text, posted where the
// window was opened for.
func (ss *session) post(atoms []byte) {
	ss.mu.Lock()
	t := ss.posting
	ss.mu.Unlock()
	if t == nil {
		ss.notice("Open a board's Start a topic, or a topic's Reply, to post.")
		return
	}
	f := texts(atoms)
	if len(f) < 2 {
		ss.logf("a post without a subject and a text: % x", atoms)
		ss.notice("Please give your post a subject and some text.")
		return
	}
	to, replyTo := "All", int64(0)
	if t.replyTo != nil {
		to, replyTo = t.replyTo.From, t.replyTo.ID
	}
	id, err := ss.n.Post(t.area, to, strings.TrimSpace(f[0]), bbs.WrapBody(mailText(f[len(f)-1])), replyTo)
	if err != nil {
		ss.notice(fmt.Sprintf("Your post was not sent: %v.", err))
		return
	}
	ss.mu.Lock()
	ss.posting = nil
	ss.mu.Unlock()
	ss.send(closeForm(postWindow))
	ss.topic(id)
}
