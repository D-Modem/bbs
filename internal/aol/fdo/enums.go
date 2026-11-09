package fdo

import (
	"fmt"
	"strings"
)

// The named values atoms take: mat_position's center_center, a
// man_start_object's trigger.  From the FDO91 manual where it gives them,
// AtomForge's compiler (MIT License: LICENSE.atomforge) for the rest, and
// all checked against what Ada32.dll compiles them to (the corpus tests).

type namedValue struct {
	name string
	val  uint32
}

// enum is a set of named values: their width in bytes, and whether they
// are flags to be or'd together.  The first name of a value is the one the
// decompiler writes.
type enum struct {
	width int
	flags bool // or'd together
	desc  bool // the decompiler writes the biggest first
	vals  []namedValue
}

func (e *enum) value(w string) (uint32, bool) {
	for _, v := range e.vals {
		if strings.TrimPrefix(v.name, "~") == w {
			return v.val, true
		}
	}
	return 0, false
}

// name is the name the decompiler writes for v; a name written ~name is
// one the compiler takes but Ada32's disassembler doesn't write.
func (e *enum) name(v uint32) (string, bool) {
	for _, nv := range e.vals {
		if nv.val == v && nv.name[0] != '~' {
			return nv.name, true
		}
	}
	return "", false
}

func nv(pairs ...any) []namedValue {
	out := make([]namedValue, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, namedValue{pairs[i].(string), uint32(pairs[i+1].(int))})
	}
	return out
}

var (
	// object types (FDO91 manual, man_start_object)
	enumObject = &enum{width: 1, vals: nv(
		"org_group", 0, "ind_group", 1, "dms_list", 2, "sms_list", 3,
		"dss_list", 4, "sss_list", 5, "trigger", 6, "ornament", 7,
		"view", 8, "edit_view", 9, "boolean", 10, "select_boolean", 11,
		"range", 12, "select_range", 13, "variable", 14, "bad_object", 15,
		"root", 16, "tool_group", 17, "tab_group", 18, "tab_page", 19,
		"pictalk", 20, "www", 21, "split", 22, "organizer", 23, "tree", 24,
		"tab", 25, "progress", 26, "toolbar", 27, "slider", 28,
		// other names for the same
		"group", 0, "independent", 1, "dynamic_multi_list", 2,
		"static_multi_list", 3, "dynamic_list", 4, "static_list", 5,
		"editable_view", 9, "selectable_boolean", 11, "selectable_range", 13,
		"ruler", 15, "popup_menu", 16, "rich_text", 17, "multimedia", 18,
		"chart", 19)}

	// action criteria (FDO91 manual, act_set_criterion)
	enumCriterion = &enum{width: 1, vals: nv(
		"void", 0, "select", 1, "close", 2, "~open", 3, "gain_focus", 4,
		"lose_focus", 5, "~cancel", 6, "~enter_free", 7, "~enter_paid", 8,
		"~create", 9, "~set_online", 10, "~set_offline", 11, "~restore", 12,
		"~minimize", 14, "~restore_from_maximize", 15, "~restore_from_minimize", 16,
		"~timeout", 17, "~screen_name_changed", 18, "~movie_over", 19, "~drop", 20,
		"~url_drop", 21, "~user_delete", 22, "~toggle_up", 23, "~activated", 24,
		"~deactivated", 25, "~popupmenu", 26, "~destroyed", 27,
		"~selection", 1)}

	enumAlert = &enum{width: 1, vals: nv(
		"info", 1, "error", 2, "pop_info", 3, "pop_error", 4, "warning", 5,
		"pop_warning", 6, "yes_no", 7, "yes_no_cancel", 8)}

	enumPosition = &enum{width: 1, vals: nv(
		"cascade", 0, "top_left", 1, "top_center", 2, "top_right", 3,
		"center_left", 4, "center_center", 5, "center_right", 6,
		"bottom_left", 7, "bottom_center", 8, "bottom_right", 9)}

	enumLogObject = &enum{width: 1, vals: nv(
		"session_log", 0, "chat_log", 1, "im_log", 2, "no_log", 3)}

	enumSortOrder = &enum{width: 1, vals: nv("normal", 0, "reverse", 1, "alphabetical", 2)}

	enumFrameStyle = &enum{width: 2, vals: nv(
		"none", 0, "single_line_pop_out", 1, "single_line_pop_in", 2,
		"pop_in", 3, "pop_out", 4, "double_line", 5, "shadow", 6, "highlight", 7)}

	enumTriggerStyle = &enum{width: 2, vals: nv(
		"default", 0, "place", 1, "rectangle", 2, "picture", 3, "framed", 4,
		"bottom_tab", 5, "plain_picture", 6, "group_state", 7)}

	enumFont = &enum{width: 1, vals: nv(
		"arial", 0, "courier", 1, "times_roman", 2, "system", 3,
		"fixed_system", 4, "ms_serif", 5, "ms_sans_serif", 6, "small_fonts", 7,
		"courier_new", 8, "script", 9, "ms_mincho", 10, "ms_gothic", 11,
		"xi_ming_ti", 12, "biao_kai_ti", 13, "ming_lui_fixed", 15,
		"ming_lui_variable", 16, "ms_hei", 17, "ms_song", 18, "fixedsys", 4)}

	enumFontStyle = &enum{width: 1, flags: true, vals: nv(
		"normal", 0, "bold", 1, "italic", 2, "underline", 4, "strikeout", 8)}

	enumFieldScript = &enum{width: 1, vals: nv(
		"latin", 0, "japanese", 1, "chinesetr", 2, "chineses", 0x19,
		"ascii", 0x7F, "any", 0x80, "default", 0xFF, "latin1", 0)}

	enumScreenName = &enum{width: 1, vals: nv("append_with_for", 2, "prepend_with_s", 0x10)}

	// mat_title_pos: where (bits 7-6) or'd with how it's justified (bits 1-0)
	enumTitlePos = &enum{width: 1, flags: true, vals: nv(
		"left", 0, "above", 0x40, "right", 0x80, "below", 0xC0,
		"center", 0, "top_or_left", 1, "bottom_or_right", 2)}

	enumTextOnPicture = &enum{width: 1, flags: true, desc: true, vals: nv(
		"art_middle_center", 0, "title_middle_center", 0,
		"title_upper_center", 2, "title_middle_left", 4, "title_middle_right", 5,
		"title_lower_center", 7, "art_middle_left", 0x40, "art_middle_right", 0x50)}

	enumFmItem = &enum{width: 1, vals: nv(
		"file_type_id", 1, "file_group_id", 2, "file_type_ext", 3,
		"file_type_desc", 4, "file_type_flags", 5, "filename", 6, "path", 7,
		"filespec", 8, "handle", 9, "error_code", 10, "custom_data", 11,
		"text_width", 12, "text_indent", 13, "text_sub_indent", 14,
		"text_flags", 15, "dialog_flags", 16, "and_mask", 17, "xor_mask", 18,
		"file_size", 19, "free_disk_space", 20, "broadcast_return_value", 21,
		"file_type_list_size", 22, "print_from_page", 23, "print_to_page", 24,
		"print_min_page", 25, "print_max_page", 26, "print_copies", 27,
		"hdc", 28, "print_job_name", 29, "date", 30, "time", 31,
		"text_mode", 32, "ini_string_length", 33, "ini_file", 34,
		"ini_group", 35, "ini_key", 36, "ini_data_type", 37, "thumbnail", 38,
		"persistent_path", 40, "ini_section_size", 41)}

	enumErrorFlags = &enum{width: 1, flags: true, vals: nv(
		"display_msg", 1, "terminate", 2, "broadcast", 4)}

	enumValidate = &enum{width: 1, flags: true, vals: nv("display_msg", 1, "terminate", 2)}

	enumDataType = &enum{width: 1, vals: nv(
		"default", 0, "text", 1, "var", 2, "boolean", 3, "global_id", 4,
		"relative_id", 5, "index", 6, "child_count", 7, "objptr", 8,
		"value", 9, "raw", 10, "length", 11, "selected", 12, "lengh", 11)}

	enumExtraction = &enum{width: 1, vals: nv(
		"default", 0, "current_object", 1, "selected_children", 2,
		"cur_obj_selected_children", 3, "all_children", 4,
		"cur_obj_all_children", 5)}

	enumBufFlags = &enum{width: 4, flags: true, vals: nv(
		"token_header", 0x1, "stream_id_header", 0x2, "host_bound", 0x4,
		"start_stream_header", 0x8, "end_stream_trailer", 0x10,
		"data_included", 0x20, "leave_buffer_open", 0x40,
		"response_id_header", 0x80, "pointer_included", 0x100,
		"clear_buffer", 0x200)}

	enumHfsFlags = &enum{width: 4, flags: true, vals: nv(
		"insert_global_id", 0x2, "update_display", 0x40)}

	enumHfsDatabase = &enum{width: 4, vals: nv("client", 0, "host", 1)}

	enumDisplay = &enum{width: 1, vals: nv("width", 0, "height", 1, "horzres", 2, "vertres", 3)}

	enumTypedData = &enum{width: 2, vals: nv("ascii", 0, "latin1", 0x0104)}

	enumAutoComplete = &enum{width: 1, vals: nv(
		"web_list", 0, "address_list", 1, "other_list", 2, "std_sort_search", 1)}
)

// atomEnums gives, per atom, the named values of each place in its
// argument (nil where it has none); the last applies to the places after.
var atomEnums = map[string][]*enum{
	"man_start_object":                {enumObject},
	"man_start_sibling":               {enumObject},
	"man_start_first":                 {enumObject},
	"man_start_alpha":                 {enumObject},
	"man_start_last":                  {enumObject},
	"mat_object_type":                 {enumObject},
	"act_set_criterion":               {enumCriterion},
	"act_do_action":                   {enumCriterion},
	"async_alert":                     {enumAlert},
	"mat_position":                    {enumPosition},
	"mat_log_object":                  {enumLogObject},
	"mat_sort_order":                  {enumSortOrder},
	"mat_frame_style":                 {enumFrameStyle},
	"mat_trigger_style":               {enumTriggerStyle},
	"mat_font_id":                     {enumFont},
	"mat_font_style":                  {enumFontStyle},
	"mat_font_sis":                    {enumFont, nil, enumFontStyle},
	"mat_intl_font_sis":               {enumFont, nil, enumFontStyle},
	"mat_field_script":                {enumFieldScript},
	"mat_title_pos":                   {enumTitlePos},
	"mat_text_on_picture_pos":         {enumTextOnPicture},
	"mat_title_append_screen_name":    {enumScreenName},
	"mat_auto_complete":               {enumAutoComplete},
	"fm_item_type":                    {enumFmItem},
	"fm_item_get":                     {enumFmItem},
	"fm_handle_error":                 {enumErrorFlags},
	"de_validate":                     {enumValidate},
	"de_set_data_type":                {enumDataType},
	"de_set_extraction_type":          {enumExtraction},
	"de_get_data":                     {enumExtraction},
	"de_get_data_value":               {enumExtraction},
	"de_get_data_pointer":             {enumExtraction},
	"de_start_extraction":             {enumBufFlags},
	"buf_start_buffer":                {enumBufFlags},
	"buf_use_buffer":                  {enumBufFlags},
	"buf_set_flags":                   {enumBufFlags},
	"buf_get_flags":                   {enumBufFlags},
	"hfs_attr_flags":                  {enumHfsFlags},
	"hfs_attr_database_type":          {enumHfsDatabase},
	"man_get_display_characteristics": {enumDisplay},
	"uni_start_typed_data":            {enumTypedData},
	"uni_next_atom_typed":             {enumTypedData},
}

func init() {
	for _, n := range []string{"chart_title_font_sis", "chart_right_font_sis",
		"chart_left_font_sis", "chart_bottom_font_sis", "chart_xlegend_font_sis",
		"chart_ylegend_font_sis", "chart_constant_font_sis", "chart_legend_font_sis"} {
		atomEnums[n] = atomEnums["mat_font_sis"]
	}
}

// enumAt is the named values of the i'th place of an atom's argument.
func enumAt(a *Atom, i int) *enum {
	es := atomEnums[a.Name]
	if len(es) == 0 {
		return nil
	}
	if i >= len(es) {
		i = len(es) - 1
	}
	return es[i]
}

// enumValue is one of an atom's named values.
func enumValue(a *Atom, i int, w string) ([]byte, bool) {
	if a.Type == TypeOrient {
		if v, ok := orientValue(w); ok {
			return []byte{v}, true
		}
		return nil, false
	}
	e := enumAt(a, i)
	if e == nil {
		return nil, false
	}
	v, ok := e.value(w)
	if !ok {
		return nil, false
	}
	return fixed(int64(v), e.width), true
}

// numWidth is the fixed width of the i'th number in an atom's argument,
// or 0 for "as few bytes as it needs".
func numWidth(a *Atom, i int) int {
	switch a.Name {
	case "mat_size":
		if i == 2 {
			return 2 // the text field's length
		}
	case "hfs_attr_field_mapping", "hfs_attr_variable_mapping", "hfs_attr_dispatch_mapping":
		return 4
	case "hfs_attr_checkbox_mapping":
		if i == 0 {
			return 4
		}
	}
	return 0
}

// pipeWidth is the width of an or'd value for an atom, or 0 for the
// widest of its parts.
func pipeWidth(a *Atom) int {
	if e := enumAt(a, 0); e != nil && e.flags {
		return e.width
	}
	return 0
}

// orientation: h or v, how the objects are justified across (c l r f e),
// and along (c t b f e).
const (
	orientAcross = "clrfe"
	orientAlong  = "ctbfe"
)

func orientValue(w string) (byte, bool) {
	if len(w) != 3 || (w[0] != 'h' && w[0] != 'v') {
		return 0, false
	}
	x := strings.IndexByte(orientAcross, w[1])
	if x < 0 && (w[1] == 't' || w[1] == 'b') {
		x = strings.IndexByte(orientAlong, w[1]) // the manual's t/b for l/r
	}
	y := strings.IndexByte(orientAlong, w[2])
	if y < 0 && (w[2] == 'l' || w[2] == 'r') {
		y = strings.IndexByte(orientAcross, w[2])
	}
	if x < 0 || y < 0 {
		return 0, false
	}
	v := byte(x<<3 | y)
	if w[0] == 'v' {
		v |= 0x40
	}
	return v, true
}

func orientName(v byte) (string, bool) {
	x, y := int(v>>3&7), int(v&7)
	if v&^0x7F != 0 || x >= len(orientAcross) || y >= len(orientAlong) {
		return "", false
	}
	d := "h"
	if v&0x40 != 0 {
		d = "v"
	}
	return fmt.Sprintf("%s%c%c", d, orientAcross[x], orientAlong[y]), true
}
