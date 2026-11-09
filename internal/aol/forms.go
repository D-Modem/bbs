package aol

import (
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/aol/fdo"
)

// The host's forms, as FDO91 source.

// onlineForm takes the client online after it signs on, and opens its
// main window (32-296).
const onlineForm = `uni_start_stream
  async_online
  uni_invoke_local <32-296>
uni_end_stream
`

// alertForm is a message box.
func alertForm(text string) string {
	return fmt.Sprintf("uni_start_stream\n  async_alert <info, %s>\nuni_end_stream\n", fdo.Quote(text))
}

// welcomeForm is the BBS's Welcome window.
func welcomeForm(bbsName, user string, online int) string {
	others := "You are the only one here."
	switch {
	case online == 2:
		others = "One other caller is here."
	case online > 2:
		others = fmt.Sprintf("%d other callers are here.", online-1)
	}
	text := strings.Join([]string{
		fmt.Sprintf("Welcome to %s, %s!", bbsName, user),
		"",
		"You are signed on with your BBS account.  " + others,
	}, "\r\n") // the client drops what follows a bare CR
	return fmt.Sprintf(`uni_start_stream
  man_start_object <ind_group, %s>
    mat_orientation <vcf>
    mat_position <center_center>
    man_start_object <view, "">
      mat_size <40, 6>
      man_append_data <%s>
      man_end_data
    man_end_object
  man_update_display
  uni_wait_off
uni_end_stream
`, fdo.Quote("Welcome, "+user+"!"), fdo.Quote(text))
}

// waitOff ends the client's wait for an answer (its hourglass).
const waitOff = "uni_start_stream\n  uni_wait_off\nuni_end_stream\n"

// closeForm closes the host's window with a global id, if it is open: a
// stream of its own, since a close takes effect when its stream ends.
func closeForm(gid string) string {
	return fmt.Sprintf("uni_start_stream\n  man_set_context_globalid <%s>\n  man_close\n  man_update_display\nuni_end_stream\n", gid)
}

// roomForm opens the client's own room window (form) as a room, with
// the people in it.
func roomForm(form, room string, users []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "uni_start_stream\n  man_preset_title <%s>\n  man_preset_gid <%s>\n  uni_invoke_local <%s>\n",
		fdo.Quote(room), chatWindow, form)
	sb.WriteString("  var_number_set <A, 1>\n  var_number_save <A, 65537>\n  man_update_display\n  chat_room_open\n")
	for _, u := range users {
		fmt.Fprintf(&sb, "  chat_add_user <%s>\n", fdo.Quote(u))
	}
	sb.WriteString("  man_update_display\nuni_end_stream\n")
	return sb.String()
}
