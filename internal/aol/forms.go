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
